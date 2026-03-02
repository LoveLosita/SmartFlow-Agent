package model

type UserSendMessageRequest struct {
	ConversationID int    `json:"conversation_id,omitempty"` // 可选，指定对话 ID
	Message        string `json:"message" binding:"required"`
	Model          string `json:"model,omitempty"` // 可选，指定使用的模型
}

type SSEResponse struct {
	Event string         `json:"event"`           // 事件类型，如 "message"、"error" 等
	ID    int            `json:"id,omitempty"`    // SSE 的 id 字段
	Retry int64          `json:"retry,omitempty"` // SSE 的 retry 字段（毫秒）
	Data  SSEMessageData `json:"data"`            // 事件数据
}

type SSEMessageData struct {
	Step    int    `json:"step,omitempty"`
	Message string `json:"message,omitempty"`
}
