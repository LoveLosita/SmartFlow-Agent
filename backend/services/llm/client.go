package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ThinkingMode 描述这次模型调用对 thinking 的期望。
type ThinkingMode string

const (
	ThinkingModeDefault  ThinkingMode = "default"
	ThinkingModeEnabled  ThinkingMode = "enabled"
	ThinkingModeDisabled ThinkingMode = "disabled"
)

// GenerateOptions 统一收敛文本调用时最常见的公共参数。
type GenerateOptions struct {
	Temperature float64
	MaxTokens   int
	Thinking    ThinkingMode
	Metadata    map[string]any
}

// TextResult 保存一次文本生成的最终结果和 usage。
// 1. Text 存放模型返回的纯文本。
// 2. Usage 方便上层做统一统计。
// 3. 这里不负责 JSON 解析，也不负责业务字段映射。
type TextResult struct {
	Text         string
	Usage        *schema.TokenUsage
	FinishReason string
}

// StreamReader 抽象可以逐块读取消息的流式返回器。
type StreamReader interface {
	Recv() (*schema.Message, error)
	Close() error
}

// TextGenerateFunc 定义统一文本生成函数签名。
type TextGenerateFunc func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (*TextResult, error)

// StreamGenerateFunc 定义统一流式生成函数签名。
type StreamGenerateFunc func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (StreamReader, error)

// Client 是统一模型客户端门面。
// 1. 只做最小输入校验和空响应防御。
// 2. 不负责 prompt 拼装，也不负责业务 fallback。
// 3. 具体 provider 的细节由上层适配器收敛进来。
type Client struct {
	generateText TextGenerateFunc
	streamText   StreamGenerateFunc
}

// NewClient 创建统一模型客户端。
func NewClient(generateText TextGenerateFunc, streamText StreamGenerateFunc) *Client {
	return &Client{
		generateText: generateText,
		streamText:   streamText,
	}
}

// GenerateText 执行一次统一文本生成。
func (c *Client) GenerateText(ctx context.Context, messages []*schema.Message, options GenerateOptions) (*TextResult, error) {
	if c == nil || c.generateText == nil {
		return nil, errors.New("llm client is not ready")
	}
	if len(messages) == 0 {
		return nil, errors.New("llm messages is empty")
	}

	result, err := c.generateText(ctx, messages, options)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.New("llm result is nil")
	}
	if strings.TrimSpace(result.Text) == "" {
		return nil, errors.New("llm returned empty text")
	}
	return result, nil
}

// GenerateJSON 先走统一文本生成，再走统一 JSON 解析。
func GenerateJSON[T any](ctx context.Context, client *Client, messages []*schema.Message, options GenerateOptions) (*T, *TextResult, error) {
	result, err := client.GenerateText(ctx, messages, options)
	if err != nil {
		return nil, nil, err
	}

	parsed, err := ParseJSONObject[T](result.Text)
	if err != nil {
		return nil, result, err
	}
	return parsed, result, nil
}

// Stream 打开统一流式调用入口。
func (c *Client) Stream(ctx context.Context, messages []*schema.Message, options GenerateOptions) (StreamReader, error) {
	if c == nil || c.streamText == nil {
		return nil, errors.New("llm stream client is not ready")
	}
	if len(messages) == 0 {
		return nil, errors.New("llm messages is empty")
	}
	return c.streamText(ctx, messages, options)
}

// BuildSystemUserMessages 构造最常见的 system + history + user 消息列表。
func BuildSystemUserMessages(systemPrompt string, history []*schema.Message, userPrompt string) []*schema.Message {
	messages := make([]*schema.Message, 0, len(history)+2)
	if strings.TrimSpace(systemPrompt) != "" {
		messages = append(messages, schema.SystemMessage(systemPrompt))
	}
	if len(history) > 0 {
		messages = append(messages, history...)
	}
	if strings.TrimSpace(userPrompt) != "" {
		messages = append(messages, schema.UserMessage(userPrompt))
	}
	return messages
}

// CloneUsage 深拷贝 token usage，避免后续累加时共享同一个指针。
func CloneUsage(usage *schema.TokenUsage) *schema.TokenUsage {
	if usage == nil {
		return nil
	}
	copied := *usage
	return &copied
}

// MergeUsage 合并两段 usage，取各字段更大的值作为累计结果。
func MergeUsage(base *schema.TokenUsage, incoming *schema.TokenUsage) *schema.TokenUsage {
	if incoming == nil {
		return CloneUsage(base)
	}
	if base == nil {
		return CloneUsage(incoming)
	}

	merged := *base
	if incoming.PromptTokens > merged.PromptTokens {
		merged.PromptTokens = incoming.PromptTokens
	}
	if incoming.CompletionTokens > merged.CompletionTokens {
		merged.CompletionTokens = incoming.CompletionTokens
	}
	if incoming.TotalTokens > merged.TotalTokens {
		merged.TotalTokens = incoming.TotalTokens
	}
	if incoming.PromptTokenDetails.CachedTokens > merged.PromptTokenDetails.CachedTokens {
		merged.PromptTokenDetails.CachedTokens = incoming.PromptTokenDetails.CachedTokens
	}
	if incoming.CompletionTokensDetails.ReasoningTokens > merged.CompletionTokensDetails.ReasoningTokens {
		merged.CompletionTokensDetails.ReasoningTokens = incoming.CompletionTokensDetails.ReasoningTokens
	}
	return &merged
}

// FormatEmptyResponseError 统一模型空结果的错误文案。
func FormatEmptyResponseError(scene string) error {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		scene = "unknown"
	}
	return fmt.Errorf("模型在 %s 场景返回空结果", scene)
}
