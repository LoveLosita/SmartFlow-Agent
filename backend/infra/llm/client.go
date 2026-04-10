package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// ThinkingMode 描述本次模型调用对 thinking 的期望。
//
// 职责边界：
// 1. 这里只表达“调用方希望怎样配置推理模式”；
// 2. 不直接绑定某个具体模型厂商的参数枚举；
// 3. 真正如何把它翻译成 ark / OpenAI / 其他 provider 的 option，由后续适配层负责。
type ThinkingMode string

const (
	ThinkingModeDefault  ThinkingMode = "default"
	ThinkingModeEnabled  ThinkingMode = "enabled"
	ThinkingModeDisabled ThinkingMode = "disabled"
)

// GenerateOptions 是统一模型调用选项。
//
// 设计目的：
// 1. 先把“每个 skill / worker 都会反复传的参数”收敛成一份结构；
// 2. 让上层以后只表达“我要什么”，不再自己重复组织 option；
// 3. 暂时不追求覆盖所有 provider 参数，先把最常用的几个公共位抽出来。
type GenerateOptions struct {
	Temperature float64
	MaxTokens   int
	Thinking    ThinkingMode
	Metadata    map[string]any
}

// TextResult 是统一文本生成结果。
//
// 职责边界：
// 1. Text 保存模型最终返回的纯文本；
// 2. Usage 保存本次调用的 token 使用量，供后续统一统计；
// 3. 不负责 JSON 解析，不负责业务字段映射。
type TextResult struct {
	Text  string
	Usage *schema.TokenUsage
}

// StreamReader 抽象了“可逐块 Recv 的流式返回器”。
//
// 之所以不直接依赖某个具体 SDK 的 reader 类型，是因为现在还处在骨架收敛阶段，
// 后续接 ark、OpenAI 兼容层还是别的 provider，都可以往这个最小接口上适配。
type StreamReader interface {
	Recv() (*schema.Message, error)
	Close() error
}

// TextGenerateFunc 是文本生成的统一适配函数签名。
type TextGenerateFunc func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (*TextResult, error)

// StreamGenerateFunc 是流式生成的统一适配函数签名。
type StreamGenerateFunc func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (StreamReader, error)

// Client 是统一模型客户端门面。
//
// 职责边界：
// 1. 负责把调用方的“模型调用意图”收敛到统一入口；
// 2. 负责统一参数校验、空响应防御、GenerateJSON 复用；
// 3. 不负责写 prompt，不负责业务 fallback，也不直接持有具体厂商 SDK 细节。
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
//
// 职责边界：
// 1. 负责做最小必要的入参校验；
// 2. 负责统一拦截“模型空响应”这类公共问题；
// 3. 不负责业务 prompt 拼接，也不负责把文本再映射成业务结构。
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
//
// 设计说明：
// 1. 把“Generate -> 提取 JSON -> 反序列化”这段公共链路收敛起来；
// 2. 上层只关心业务结构，不需要重复实现解析样板；
// 3. 返回 parsed + rawResult，方便打点与回退时保留原文。
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
//
// 职责边界：
// 1. 只负责把“流式生成能力”暴露给上层；
// 2. 不负责 chunk 到 OpenAI 协议的转换，那部分应放在 stream/；
// 3. 不负责累计全文，也不负责 token 统计落库。
func (c *Client) Stream(ctx context.Context, messages []*schema.Message, options GenerateOptions) (StreamReader, error) {
	if c == nil || c.streamText == nil {
		return nil, errors.New("llm stream client is not ready")
	}
	if len(messages) == 0 {
		return nil, errors.New("llm messages is empty")
	}
	return c.streamText(ctx, messages, options)
}

// BuildSystemUserMessages 构造最常见的“system + history + user”消息列表。
//
// 设计说明：
// 1. 先把最稳定的消息编排方式沉淀下来，减少各业务域样板代码；
// 2. 只做消息切片装配，不做 prompt 生成；
// 3. 供 agent / memory 等多个能力域复用。
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

// CloneUsage 深拷贝 token usage，避免后续多处累加时共享同一指针。
func CloneUsage(usage *schema.TokenUsage) *schema.TokenUsage {
	if usage == nil {
		return nil
	}
	copied := *usage
	return &copied
}

// MergeUsage 合并两段 usage。
//
// 合并策略：
// 1. 对“同一次调用不同流分片”的场景，取更大值作为最终值；
// 2. 对“多次独立调用累计”的场景，应由上层显式做加法，而不是用这个函数；
// 3. 该函数只适用于“同一次调用的分块 usage 收敛”。
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

// FormatEmptyResponseError 统一生成“模型返回空结果”的错误文案。
func FormatEmptyResponseError(scene string) error {
	scene = strings.TrimSpace(scene)
	if scene == "" {
		scene = "unknown"
	}
	return fmt.Errorf("模型在 %s 场景返回空结果", scene)
}
