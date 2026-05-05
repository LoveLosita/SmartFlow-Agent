package agent

import "encoding/json"

// ChatRequest 是 Gateway 调用 agent RPC Chat 流时使用的最小跨进程契约。
//
// 职责边界：
// 1. 只承载 Gateway 已完成鉴权与会话归一化后的字段；
// 2. 不承载 HTTP header、SSE 细节或 Gin 上下文；
// 3. extra_json 只负责透传 extra 的 JSON 快照，不在契约层解释业务语义。
type ChatRequest struct {
	Message        string          `json:"message"`
	Thinking       string          `json:"thinking,omitempty"`
	Model          string          `json:"model,omitempty"`
	UserID         int             `json:"user_id"`
	ConversationID string          `json:"conversation_id"`
	ExtraJSON      json.RawMessage `json:"extra_json,omitempty"`
}

// ChatChunk 是 agent RPC Chat 流回传给 Gateway 的最小分块契约。
//
// 职责边界：
// 1. payload 保持前端现有 SSE data 负载格式，不在 Gateway 二次改写；
// 2. done 只表达“流是否结束”，不包含额外业务语义；
// 3. error_json 只表达服务端错误快照，最终由 Gateway 转换成既有 SSE 错误形态。
type ChatChunk struct {
	Payload   string          `json:"payload,omitempty"`
	Done      bool            `json:"done,omitempty"`
	ErrorJSON json.RawMessage `json:"error_json,omitempty"`
}
