package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/audit"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/ratelimit"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/security"
	"github.com/LoveLosita/smartflow/infra/smartflow-mcp-server/internal/tools"
)

const (
	jsonRPCVersion = "2.0"

	errCodeParseError     = -32700
	errCodeInvalidRequest = -32600
	errCodeMethodNotFound = -32601
	errCodeInvalidParams  = -32602
	errCodeInternalError  = -32603
)

type Server struct {
	reader          *bufio.Reader
	writer          io.Writer
	writeMu         sync.Mutex
	registry        *tools.Registry
	auditLogger     *audit.Logger
	limiter         *ratelimit.Limiter
	serverName      string
	serverVersion   string
	protocolVersion string
	defaultCaller   string
	toolTimeout     time.Duration
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *respError      `json:"error,omitempty"`
}

type respError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type initializeParams struct {
	ProtocolVersion string `json:"protocolVersion"`
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Meta      map[string]any `json:"_meta,omitempty"`
}

func NewServer(
	in io.Reader,
	out io.Writer,
	registry *tools.Registry,
	auditLogger *audit.Logger,
	limiter *ratelimit.Limiter,
	serverName string,
	serverVersion string,
	protocolVersion string,
	defaultCaller string,
	toolTimeout time.Duration,
) *Server {
	return &Server{
		reader:          bufio.NewReader(in),
		writer:          out,
		registry:        registry,
		auditLogger:     auditLogger,
		limiter:         limiter,
		serverName:      serverName,
		serverVersion:   serverVersion,
		protocolVersion: protocolVersion,
		defaultCaller:   defaultCaller,
		toolTimeout:     toolTimeout,
	}
}

func (s *Server) Serve(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		body, err := readMessage(s.reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			log.Printf("read message failed: %v", err)
			continue
		}

		var req request
		if err := json.Unmarshal(body, &req); err != nil {
			_ = s.writeError(nil, errCodeParseError, "invalid json")
			continue
		}
		if req.Method == "" || req.JSONRPC != jsonRPCVersion {
			_ = s.writeError(req.ID, errCodeInvalidRequest, "invalid json-rpc request")
			continue
		}

		if err := s.handleRequest(ctx, req); err != nil {
			log.Printf("handle request failed: %v", err)
		}
	}
}

func (s *Server) handleRequest(ctx context.Context, req request) error {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		return nil
	case "tools/list":
		if len(req.ID) == 0 {
			return nil
		}
		return s.writeResult(req.ID, map[string]any{"tools": s.registry.List()})
	case "tools/call":
		if len(req.ID) == 0 {
			return nil
		}
		return s.handleToolCall(ctx, req)
	case "ping":
		if len(req.ID) == 0 {
			return nil
		}
		return s.writeResult(req.ID, map[string]any{})
	default:
		if len(req.ID) == 0 {
			return nil
		}
		return s.writeError(req.ID, errCodeMethodNotFound, "method not found")
	}
}

func (s *Server) handleInitialize(req request) error {
	if len(req.ID) == 0 {
		return nil
	}

	var params initializeParams
	if len(req.Params) > 0 {
		_ = json.Unmarshal(req.Params, &params)
	}
	_ = params

	return s.writeResult(req.ID, map[string]any{
		"protocolVersion": s.protocolVersion,
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    s.serverName,
			"version": s.serverVersion,
		},
	})
}

func (s *Server) handleToolCall(ctx context.Context, req request) error {
	var params toolCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return s.writeError(req.ID, errCodeInvalidParams, "invalid tool call params")
	}
	if params.Name == "" {
		return s.writeError(req.ID, errCodeInvalidParams, "tool name is required")
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	caller := extractCaller(params, s.defaultCaller)
	rateKey := fmt.Sprintf("%s:%s", caller, params.Name)
	if !s.limiter.Allow(rateKey) {
		result := buildToolErrorResult("rate limit exceeded")
		s.auditLogger.Log(audit.Record{
			Tool:       params.Name,
			Caller:     caller,
			Success:    false,
			DurationMs: 0,
			Meta:       sanitizeAuditInput(params.Name, params.Arguments),
			Error:      "rate limit exceeded",
		})
		return s.writeResult(req.ID, result)
	}

	tool, ok := s.registry.Find(params.Name)
	if !ok {
		result := buildToolErrorResult("tool not found")
		s.auditLogger.Log(audit.Record{
			Tool:       params.Name,
			Caller:     caller,
			Success:    false,
			DurationMs: 0,
			Meta:       sanitizeAuditInput(params.Name, params.Arguments),
			Error:      "tool not found",
		})
		return s.writeResult(req.ID, result)
	}

	start := time.Now()
	toolCtx, cancel := context.WithTimeout(ctx, s.toolTimeout)
	defer cancel()

	output, err := tool.Execute(toolCtx, params.Arguments)
	duration := time.Since(start).Milliseconds()
	if err != nil {
		errMsg := sanitizeError(err)
		s.auditLogger.Log(audit.Record{
			Tool:       params.Name,
			Caller:     caller,
			Success:    false,
			DurationMs: duration,
			Meta:       sanitizeAuditInput(params.Name, params.Arguments),
			Error:      errMsg,
		})
		return s.writeResult(req.ID, buildToolErrorResult(errMsg))
	}

	s.auditLogger.Log(audit.Record{
		Tool:       params.Name,
		Caller:     caller,
		Success:    true,
		DurationMs: duration,
		Meta:       sanitizeAuditInput(params.Name, params.Arguments),
	})
	return s.writeResult(req.ID, buildToolSuccessResult(output))
}

func buildToolSuccessResult(output map[string]any) map[string]any {
	asJSON, _ := json.Marshal(output)
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(asJSON)},
		},
		"structuredContent": output,
	}
}

func buildToolErrorResult(message string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": message},
		},
		"structuredContent": map[string]any{"error": message},
		"isError":           true,
	}
}

func extractCaller(params toolCallParams, fallback string) string {
	if params.Meta != nil {
		if v, ok := params.Meta["caller"]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	if v, ok := params.Arguments["caller"]; ok {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return fallback
}

func sanitizeAuditInput(toolName string, args map[string]any) map[string]any {
	meta := map[string]any{}
	switch toolName {
	case "mysql_query_readonly":
		sql, _ := args["sql"].(string)
		meta["sql"] = security.RedactSQL(sql)
		if params, ok := args["params"].([]any); ok {
			meta["paramCount"] = len(params)
		}
	case "redis_get":
		if key, ok := args["key"].(string); ok {
			meta["key"] = security.RedactKey(key)
		}
	case "redis_scan":
		if pattern, ok := args["pattern"].(string); ok {
			if len(pattern) > 40 {
				meta["pattern"] = pattern[:40]
			} else {
				meta["pattern"] = pattern
			}
		}
		if count, ok := args["count"]; ok {
			meta["count"] = count
		}
	default:
		meta["args"] = "masked"
	}
	return meta
}

func sanitizeError(err error) string {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	if len(msg) > 300 {
		return msg[:300]
	}
	return msg
}

func (s *Server) writeResult(id json.RawMessage, result any) error {
	resp := response{JSONRPC: jsonRPCVersion, ID: id, Result: result}
	return s.writeMessage(resp)
}

func (s *Server) writeError(id json.RawMessage, code int, message string) error {
	resp := response{
		JSONRPC: jsonRPCVersion,
		ID:      id,
		Error:   &respError{Code: code, Message: message},
	}
	return s.writeMessage(resp)
}

func (s *Server) writeMessage(payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(body))
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.writer.Write([]byte(header)); err != nil {
		return err
	}
	_, err = s.writer.Write(body)
	return err
}

func readMessage(reader *bufio.Reader) ([]byte, error) {
	contentLength := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		headerName := strings.TrimSpace(parts[0])
		headerValue := strings.TrimSpace(parts[1])
		if strings.EqualFold(headerName, "Content-Length") {
			n, err := strconv.Atoi(headerValue)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length")
			}
			contentLength = n
		}
	}
	if contentLength < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(reader, body); err != nil {
		return nil, err
	}
	return body, nil
}
