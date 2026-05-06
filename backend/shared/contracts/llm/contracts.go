package llm

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

const (
	ModelAliasLite                 = "lite"
	ModelAliasPro                  = "pro"
	ModelAliasMax                  = "max"
	ModelAliasCourseImageResponses = "course_image_responses"

	DefaultTextModelAlias = ModelAliasPro
	ProviderNameArk       = "ark"
)

// PingRequest 只用于跨进程探活，不承载业务字段。
type PingRequest struct{}

// PingResponse 只用于跨进程探活，不承载业务字段。
type PingResponse struct{}

// BillingContext 是 LLM RPC 透传的计费上下文副本。
type BillingContext struct {
	UserID         uint64 `json:"user_id"`
	EventID        string `json:"event_id"`
	Scene          string `json:"scene"`
	RequestID      string `json:"request_id"`
	ConversationID string `json:"conversation_id"`
	ModelAlias     string `json:"model_alias"`
	SkipCharge     bool   `json:"skip_charge"`
}

// GenerateOptions 是文本模型跨进程调用时的最小公共参数。
type GenerateOptions struct {
	Temperature float64        `json:"temperature"`
	MaxTokens   int            `json:"max_tokens"`
	Thinking    string         `json:"thinking"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// TextRequest 描述一次非流式文本调用。
type TextRequest struct {
	ModelAlias string            `json:"model_alias"`
	Messages   []*schema.Message `json:"messages"`
	Options    GenerateOptions   `json:"options"`
	Billing    *BillingContext   `json:"billing,omitempty"`
}

// StreamTextRequest 描述一次流式文本调用。
type StreamTextRequest struct {
	ModelAlias string            `json:"model_alias"`
	Messages   []*schema.Message `json:"messages"`
	Options    GenerateOptions   `json:"options"`
	Billing    *BillingContext   `json:"billing,omitempty"`
}

// TextResult 保存文本模型最终输出。
type TextResult struct {
	Text         string             `json:"text"`
	Usage        *schema.TokenUsage `json:"usage,omitempty"`
	FinishReason string             `json:"finish_reason,omitempty"`
}

// TextResponse 统一包住文本调用结果，便于后续扩字段。
type TextResponse struct {
	Result *TextResult `json:"result,omitempty"`
}

// StreamChunk 是流式返回的最小块协议。
type StreamChunk struct {
	Message *schema.Message `json:"message,omitempty"`
}

// ResponsesMessage 描述 Responses 模型单条输入。
type ResponsesMessage struct {
	Role        string `json:"role"`
	Text        string `json:"text,omitempty"`
	ImageURL    string `json:"image_url,omitempty"`
	ImageDetail string `json:"image_detail,omitempty"`
}

// ResponsesOptions 描述 Responses 模型公共参数。
type ResponsesOptions struct {
	Model           string  `json:"model,omitempty"`
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Thinking        string  `json:"thinking"`
	TextFormat      string  `json:"text_format,omitempty"`
}

// ResponsesRequest 描述一次 Responses 文本调用。
type ResponsesRequest struct {
	ModelAlias string             `json:"model_alias"`
	Messages   []ResponsesMessage `json:"messages"`
	Options    ResponsesOptions   `json:"options"`
	Billing    *BillingContext    `json:"billing,omitempty"`
}

// ResponsesUsage 是 Responses 结果里的最小 usage 结构。
type ResponsesUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// ResponsesResult 统一承载 Responses 输出。
type ResponsesResult struct {
	Text             string          `json:"text"`
	Status           string          `json:"status,omitempty"`
	IncompleteReason string          `json:"incomplete_reason,omitempty"`
	ErrorCode        string          `json:"error_code,omitempty"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	Usage            *ResponsesUsage `json:"usage,omitempty"`
}

// ResponsesResponse 统一包住 Responses 结果。
type ResponsesResponse struct {
	Result *ResponsesResult `json:"result,omitempty"`
}

// NormalizeModelAlias 负责把空别名收敛到默认文本模型。
func NormalizeModelAlias(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return DefaultTextModelAlias
	}
	return trimmed
}
