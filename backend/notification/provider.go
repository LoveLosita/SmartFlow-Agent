package notification

import "context"

const (
	// ChannelFeishu 表示当前通知记录走飞书通道。
	ChannelFeishu = "feishu"
)

const (
	// FeishuErrorCodeProviderTimeout 表示 provider 超时，属于可重试错误。
	FeishuErrorCodeProviderTimeout = "provider_timeout"
	// FeishuErrorCodeProviderRateLimited 表示 provider 限流，属于可重试错误。
	FeishuErrorCodeProviderRateLimited = "provider_rate_limited"
	// FeishuErrorCodeProvider5xx 表示 provider 服务端异常，属于可重试错误。
	FeishuErrorCodeProvider5xx = "provider_5xx"
	// FeishuErrorCodeNetworkError 表示网络层异常，属于可重试错误。
	FeishuErrorCodeNetworkError = "network_error"
	// FeishuErrorCodeRecipientMissing 表示缺少接收方，属于不可恢复错误。
	FeishuErrorCodeRecipientMissing = "recipient_missing"
	// FeishuErrorCodeInvalidURL 表示目标链接非法，属于不可恢复错误。
	FeishuErrorCodeInvalidURL = "invalid_url"
	// FeishuErrorCodeProviderAuthFailed 表示 provider 认证失败，属于不可恢复错误。
	FeishuErrorCodeProviderAuthFailed = "provider_auth_failed"
	// FeishuErrorCodePayloadInvalid 表示请求体非法，属于不可恢复错误。
	FeishuErrorCodePayloadInvalid = "payload_invalid"
)

// FeishuSendOutcome 表示 provider 对一次投递尝试的分类结果。
//
// 职责边界：
// 1. 只表达 provider 层对“这次投递”是否成功、是否可重试的判断；
// 2. 不直接承载 notification_records 的状态机，状态流转由 NotificationService 决定；
// 3. future webhook/open_id provider 只要返回同一套枚举，即可复用现有重试逻辑。
type FeishuSendOutcome string

const (
	FeishuSendOutcomeSuccess       FeishuSendOutcome = "success"
	FeishuSendOutcomeTemporaryFail FeishuSendOutcome = "temporary_fail"
	FeishuSendOutcomePermanentFail FeishuSendOutcome = "permanent_fail"
	FeishuSendOutcomeSkipped       FeishuSendOutcome = "skipped"
)

// FeishuSendRequest 是通知服务传给 provider 的稳定输入。
//
// 职责边界：
// 1. 只描述 provider 真正发消息所需的信息；
// 2. 不暴露 GORM model，避免 provider 依赖数据库细节；
// 3. 同时保留审计字段，方便 mock/webhook provider 记录请求摘要。
type FeishuSendRequest struct {
	NotificationID int64  `json:"notification_id"`
	UserID         int    `json:"user_id"`
	TriggerID      string `json:"trigger_id"`
	PreviewID      string `json:"preview_id"`
	TriggerType    string `json:"trigger_type"`
	TargetType     string `json:"target_type"`
	TargetID       int    `json:"target_id"`
	TargetURL      string `json:"target_url"`
	MessageText    string `json:"message_text"`
	FallbackUsed   bool   `json:"fallback_used"`
	TraceID        string `json:"trace_id,omitempty"`
	AttemptCount   int    `json:"attempt_count"`
}

// FeishuSendResult 是 provider 对外返回的投递结果。
//
// 职责边界：
// 1. outcome 决定 NotificationService 应该进入 sent / failed / dead 中哪一条路径；
// 2. request/response payload 仅用于落库审计，不要求与任意具体 SDK 强绑定；
// 3. error_code 需要尽量稳定，便于后续按错误码做告警和排障。
type FeishuSendResult struct {
	Outcome           FeishuSendOutcome `json:"outcome"`
	ProviderMessageID string            `json:"provider_message_id,omitempty"`
	ErrorCode         string            `json:"error_code,omitempty"`
	ErrorMessage      string            `json:"error_message,omitempty"`
	RequestPayload    any               `json:"request_payload,omitempty"`
	ResponsePayload   any               `json:"response_payload,omitempty"`
}

// FeishuProvider 是飞书投递能力的抽象边界。
//
// 职责边界：
// 1. 负责把最终文案发给具体 provider；
// 2. 不负责 notification_records 的创建、去重、状态机和重试节奏；
// 3. 后续新增 WebhookFeishuProvider / OpenIDFeishuProvider 时，只需实现这个接口。
type FeishuProvider interface {
	Send(ctx context.Context, req FeishuSendRequest) (FeishuSendResult, error)
}
