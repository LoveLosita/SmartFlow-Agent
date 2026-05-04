package notification

import "time"

// SaveFeishuWebhookRequest 是 gateway 写入飞书 Webhook 通道配置的跨进程契约。
//
// 职责边界：
// 1. 只承载用户提交的配置字段，不做 URL、鉴权类型或 token 的业务校验；
// 2. user_id 由 gateway 从 JWT 上下文取得，不能信任前端传入；
// 3. bearer_token 只在服务内持久化和投递时使用，响应契约只返回 has_bearer_token。
type SaveFeishuWebhookRequest struct {
	UserID      int    `json:"user_id"`
	Enabled     bool   `json:"enabled"`
	WebhookURL  string `json:"webhook_url"`
	AuthType    string `json:"auth_type"`
	BearerToken string `json:"bearer_token"`
}

// GetFeishuWebhookRequest 是查询飞书 Webhook 通道配置的跨进程契约。
type GetFeishuWebhookRequest struct {
	UserID int `json:"user_id"`
}

// DeleteFeishuWebhookRequest 是删除飞书 Webhook 通道配置的跨进程契约。
type DeleteFeishuWebhookRequest struct {
	UserID int `json:"user_id"`
}

// TestFeishuWebhookRequest 是触发飞书 Webhook 测试消息的跨进程契约。
type TestFeishuWebhookRequest struct {
	UserID int `json:"user_id"`
}

// ChannelResponse 是通知通道配置返回给前端的脱敏视图。
//
// 职责边界：
// 1. 不返回 webhook_url 原文和 bearer_token 原文；
// 2. 只返回用户界面需要展示的开关、脱敏地址、鉴权类型和最近测试结果；
// 3. 不暴露 notification_records 的投递状态，二者属于不同读模型。
type ChannelResponse struct {
	Channel        string     `json:"channel"`
	Enabled        bool       `json:"enabled"`
	Configured     bool       `json:"configured"`
	WebhookURLMask string     `json:"webhook_url_mask,omitempty"`
	AuthType       string     `json:"auth_type"`
	HasBearerToken bool       `json:"has_bearer_token"`
	LastTestStatus string     `json:"last_test_status,omitempty"`
	LastTestError  string     `json:"last_test_error,omitempty"`
	LastTestAt     *time.Time `json:"last_test_at,omitempty"`
}

// TestResult 描述一次飞书 Webhook 测试投递结果。
type TestResult struct {
	Channel  ChannelResponse `json:"channel"`
	Status   string          `json:"status"`
	Outcome  string          `json:"outcome"`
	Message  string          `json:"message,omitempty"`
	TraceID  string          `json:"trace_id,omitempty"`
	SentAt   time.Time       `json:"sent_at"`
	Skipped  bool            `json:"skipped"`
	Provider string          `json:"provider"`
}
