package llm

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	llmrpc "github.com/LoveLosita/smartflow/backend/services/llm/rpc"
	llmcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/llm"
	"github.com/cloudwego/eino/schema"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint    = "127.0.0.1:9096"
	defaultTimeout     = 0
	defaultPingTimeout = 2 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

type ServiceConfig struct {
	ClientConfig
	CourseVisionModel string
}

// Client 是业务进程访问独立 LLM 服务的最小 RPC 适配层。
type Client struct {
	rpc llmrpc.LLMClient
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
	}, zrpc.WithDialOption(llmrpc.JSONCodecDialOption()))
	if err != nil {
		return nil, err
	}

	client := &Client{rpc: llmrpc.NewLLMClient(zclient.Conn())}
	if err = client.ping(resolvePingTimeout(timeout)); err != nil {
		return nil, err
	}
	return client, nil
}

// NewService 一次性把远端 LLM RPC 包装回旧的 *llmservice.Service 门面。
func NewService(cfg ServiceConfig) (*llmservice.Service, error) {
	client, err := NewClient(cfg.ClientConfig)
	if err != nil {
		return nil, err
	}
	return client.BuildService(cfg.CourseVisionModel), nil
}

func (c *Client) BuildService(courseVisionModel string) *llmservice.Service {
	if c == nil {
		return nil
	}

	return llmservice.NewWithClients(llmservice.StaticClients{
		Lite: buildTextClient(c, llmcontracts.ModelAliasLite),
		Pro:  buildTextClient(c, llmcontracts.ModelAliasPro),
		Max:  buildTextClient(c, llmcontracts.ModelAliasMax),
		CourseImageResponses: llmservice.NewArkResponsesClientWithFunc(courseVisionModel, func(ctx context.Context, messages []llmservice.ArkResponsesMessage, options llmservice.ArkResponsesOptions) (*llmservice.ArkResponsesResult, error) {
			return c.GenerateResponsesText(ctx, llmcontracts.ModelAliasCourseImageResponses, messages, options)
		}),
	})
}

func (c *Client) Ping(ctx context.Context) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	_, err := c.rpc.Ping(ctx, &llmcontracts.PingRequest{})
	return responseFromRPCError(err)
}

func (c *Client) GenerateText(ctx context.Context, modelAlias string, messages []*schema.Message, options llmservice.GenerateOptions) (*llmservice.TextResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}

	resp, err := c.rpc.GenerateText(ctx, &llmcontracts.TextRequest{
		ModelAlias: modelAlias,
		Messages:   messages,
		Options:    toContractGenerateOptions(options),
		Billing:    billingFromContext(ctx, modelAlias),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil || resp.Result == nil {
		return nil, errors.New("llm zrpc service returned empty text response")
	}
	return &llmservice.TextResult{
		Text:         resp.Result.Text,
		Usage:        llmservice.CloneUsage(resp.Result.Usage),
		FinishReason: resp.Result.FinishReason,
	}, nil
}

func (c *Client) StreamText(ctx context.Context, modelAlias string, messages []*schema.Message, options llmservice.GenerateOptions) (llmservice.StreamReader, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}

	stream, err := c.rpc.StreamText(ctx, &llmcontracts.StreamTextRequest{
		ModelAlias: modelAlias,
		Messages:   messages,
		Options:    toContractGenerateOptions(options),
		Billing:    billingFromContext(ctx, modelAlias),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return &streamReader{stream: stream}, nil
}

func (c *Client) GenerateResponsesText(ctx context.Context, modelAlias string, messages []llmservice.ArkResponsesMessage, options llmservice.ArkResponsesOptions) (*llmservice.ArkResponsesResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}

	resp, err := c.rpc.GenerateResponsesText(ctx, &llmcontracts.ResponsesRequest{
		ModelAlias: modelAlias,
		Messages:   toContractResponsesMessages(messages),
		Options:    toContractResponsesOptions(options),
		Billing:    billingFromContext(ctx, modelAlias),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil || resp.Result == nil {
		return nil, errors.New("llm zrpc service returned empty responses response")
	}
	return toServiceResponsesResult(resp.Result), nil
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("llm zrpc client is not initialized")
	}
	return nil
}

func (c *Client) ping(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return c.Ping(ctx)
}

type streamReader struct {
	stream llmrpc.LLM_StreamTextClient
}

func (r *streamReader) Recv() (*schema.Message, error) {
	if r == nil || r.stream == nil {
		return nil, errors.New("llm zrpc stream is not initialized")
	}

	chunk, err := r.stream.Recv()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.EOF
		}
		return nil, responseFromRPCError(err)
	}
	if chunk == nil {
		return nil, errors.New("llm zrpc service returned empty stream chunk")
	}
	return chunk.Message, nil
}

func (r *streamReader) Close() error {
	return nil
}

func buildTextClient(remote *Client, modelAlias string) *llmservice.Client {
	return llmservice.NewClient(
		func(ctx context.Context, messages []*schema.Message, options llmservice.GenerateOptions) (*llmservice.TextResult, error) {
			return remote.GenerateText(ctx, modelAlias, messages, options)
		},
		func(ctx context.Context, messages []*schema.Message, options llmservice.GenerateOptions) (llmservice.StreamReader, error) {
			return remote.StreamText(ctx, modelAlias, messages, options)
		},
	)
}

func billingFromContext(ctx context.Context, modelAlias string) *llmcontracts.BillingContext {
	billing, ok := llmservice.BillingContextFromContext(ctx)
	if !ok {
		return nil
	}
	if strings.TrimSpace(billing.ModelAlias) == "" {
		billing.ModelAlias = strings.TrimSpace(modelAlias)
	}
	return &llmcontracts.BillingContext{
		UserID:         billing.UserID,
		EventID:        billing.EventID,
		Scene:          billing.Scene,
		RequestID:      billing.RequestID,
		ConversationID: billing.ConversationID,
		ModelAlias:     billing.ModelAlias,
		SkipCharge:     billing.SkipCharge,
	}
}

func toContractGenerateOptions(input llmservice.GenerateOptions) llmcontracts.GenerateOptions {
	return llmcontracts.GenerateOptions{
		Temperature: input.Temperature,
		MaxTokens:   input.MaxTokens,
		Thinking:    string(input.Thinking),
		Metadata:    input.Metadata,
	}
}

func toContractResponsesMessages(input []llmservice.ArkResponsesMessage) []llmcontracts.ResponsesMessage {
	if len(input) == 0 {
		return nil
	}
	output := make([]llmcontracts.ResponsesMessage, 0, len(input))
	for _, item := range input {
		output = append(output, llmcontracts.ResponsesMessage{
			Role:        item.Role,
			Text:        item.Text,
			ImageURL:    item.ImageURL,
			ImageDetail: item.ImageDetail,
		})
	}
	return output
}

func toContractResponsesOptions(input llmservice.ArkResponsesOptions) llmcontracts.ResponsesOptions {
	return llmcontracts.ResponsesOptions{
		Model:           input.Model,
		Temperature:     input.Temperature,
		MaxOutputTokens: input.MaxOutputTokens,
		Thinking:        string(input.Thinking),
		TextFormat:      input.TextFormat,
	}
}

func toServiceResponsesResult(result *llmcontracts.ResponsesResult) *llmservice.ArkResponsesResult {
	if result == nil {
		return nil
	}
	output := &llmservice.ArkResponsesResult{
		Text:             result.Text,
		Status:           result.Status,
		IncompleteReason: result.IncompleteReason,
		ErrorCode:        result.ErrorCode,
		ErrorMessage:     result.ErrorMessage,
	}
	if result.Usage != nil {
		output.Usage = &llmservice.ArkResponsesUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		}
	}
	return output
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

func resolvePingTimeout(timeout time.Duration) time.Duration {
	if timeout > 0 && timeout < defaultPingTimeout {
		return timeout
	}
	return defaultPingTimeout
}
