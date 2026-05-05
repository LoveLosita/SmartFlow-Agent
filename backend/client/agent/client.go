package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	agentpb "github.com/LoveLosita/smartflow/backend/services/agent/rpc/pb"
	agentcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/agent"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultEndpoint = "127.0.0.1:9089"
	defaultTimeout  = 0
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 访问 agent zrpc 的流式适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC stream 调用，不感知 Gin / SSE；
// 2. ChatChunk 的 payload 保持 agent 服务原样输出，Gateway API 再转成 SSE data；
// 3. agent.rpc.chat.enabled 关闭时，调用方仍可走本地 AgentService 回退链路。
type Client struct {
	rpc agentpb.AgentClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout < 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	return &Client{rpc: agentpb.NewAgentClient(zclient.Conn())}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	_, err := c.rpc.Ping(ctx, &agentpb.StatusResponse{})
	return responseFromRPCError(err)
}

func (c *Client) Chat(ctx context.Context, req agentcontracts.ChatRequest) (*ChatStream, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	stream, err := c.rpc.Chat(ctx, &agentpb.ChatRequest{
		Message:        req.Message,
		Thinking:       req.Thinking,
		Model:          req.Model,
		UserId:         int32(req.UserID),
		ConversationId: req.ConversationID,
		ExtraJson:      append([]byte(nil), req.ExtraJSON...),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return &ChatStream{stream: stream}, nil
}

func (c *Client) GetConversationMeta(ctx context.Context, req agentcontracts.ConversationQueryRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetConversationMeta, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetConversationList(ctx context.Context, req agentcontracts.ConversationListRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetConversationList, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetConversationTimeline(ctx context.Context, req agentcontracts.ConversationQueryRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetConversationTimeline, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetSchedulePlanPreview(ctx context.Context, req agentcontracts.ConversationQueryRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetSchedulePlanPreview, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetContextStats(ctx context.Context, req agentcontracts.ConversationQueryRequest) (string, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetContextStats, req)
	raw, err := jsonFromResponse(resp, err)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func (c *Client) SaveScheduleState(ctx context.Context, req agentcontracts.SaveScheduleStateRequest) error {
	_, err := c.callJSON(ctx, c.rpc.SaveScheduleState, req)
	return responseFromRPCError(err)
}

type ChatStream struct {
	stream agentpb.Agent_ChatClient
}

// Recv 读取 agent RPC 的下一段输出。
//
// 返回语义：
// 1. io.EOF 表示服务端正常关闭 stream；
// 2. 其它 error 已尽量反解为项目内错误；
// 3. chunk.Done 由上层决定是否写出 [DONE]。
func (s *ChatStream) Recv() (agentcontracts.ChatChunk, error) {
	if s == nil || s.stream == nil {
		return agentcontracts.ChatChunk{}, errors.New("agent zrpc stream is not initialized")
	}
	chunk, err := s.stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return agentcontracts.ChatChunk{}, io.EOF
		}
		return agentcontracts.ChatChunk{}, responseFromRPCError(err)
	}
	if chunk == nil {
		return agentcontracts.ChatChunk{}, errors.New("agent zrpc service returned empty chunk")
	}
	return agentcontracts.ChatChunk{
		Payload:   chunk.Payload,
		Done:      chunk.Done,
		ErrorJSON: append([]byte(nil), chunk.ErrorJson...),
	}, nil
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("agent zrpc client is not initialized")
	}
	return nil
}

func (c *Client) callJSON(ctx context.Context, fn func(context.Context, *agentpb.JSONRequest, ...grpc.CallOption) (*agentpb.JSONResponse, error), payload any) (*agentpb.JSONResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return fn(ctx, &agentpb.JSONRequest{PayloadJson: raw})
}

func jsonFromResponse(resp *agentpb.JSONResponse, rpcErr error) (json.RawMessage, error) {
	if rpcErr != nil {
		return nil, responseFromRPCError(rpcErr)
	}
	if resp == nil {
		return nil, errors.New("agent zrpc service returned empty JSON response")
	}
	if len(resp.DataJson) == 0 {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(resp.DataJson), nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}
