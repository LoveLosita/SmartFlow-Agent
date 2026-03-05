package model

import "time"

type UserSendMessageRequest struct {
	ConversationID string `json:"conversation_id,omitempty"` // 可选，指定对话 ID
	Message        string `json:"message" binding:"required"`
	Model          string `json:"model,omitempty"`    // 可选，指定使用的模型
	Thinking       bool   `json:"thinking,omitempty"` // 可选，是否开启思考模式
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

type AgentChat struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	ChatID        string     `gorm:"column:chat_id;type:varchar(36);not null;uniqueIndex:uk_chat_id;comment:会话ID，UUID格式"`
	UserID        int        `gorm:"column:user_id;not null;index:idx_user_last,priority:1;index:idx_user_status,priority:1;comment:所属用户"`
	Title         *string    `gorm:"column:title;type:varchar(255);comment:会话标题"`
	SystemPrompt  *string    `gorm:"column:system_prompt;type:text;comment:可选：系统提示词/会话级上下文"`
	Model         *string    `gorm:"column:model;type:varchar(100);comment:可选：使用的模型标识"`
	MessageCount  int        `gorm:"column:message_count;not null;default:0;comment:消息数（可冗余）"`
	TokensTotal   int        `gorm:"column:tokens_total;not null;default:0;comment:累计消耗（可冗余）"`
	LastMessageAt *time.Time `gorm:"column:last_message_at;comment:最后一条消息时间"`
	Status        string     `gorm:"column:status;type:varchar(32);not null;default:active;index:idx_user_status,priority:2;comment:active/archived"`
	CreatedAt     *time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;comment:软删除"`
	// 关联：一个会话有多条消息
	Chats []ChatHistory `gorm:"foreignKey:ChatID;references:ChatID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func (AgentChat) TableName() string { return "agent_chats" }

type ChatHistory struct {
	ID             int        `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID         string     `gorm:"column:chat_id;type:varchar(36);not null;index:idx_user_chat,priority:2;index:idx_chat_id;comment:对话UUID"`
	UserID         int        `gorm:"column:user_id;not null;index:idx_user_chat,priority:1"`
	MessageContent *string    `gorm:"column:message_content;type:text;comment:用户或AI的话"`
	Role           *string    `gorm:"column:role;type:varchar(32);comment:user / assistant"`
	TokensConsumed int        `gorm:"column:tokens_consumed;not null;default:0;comment:单次消耗，用于累加到 users 表"`
	CreatedAt      *time.Time `gorm:"column:created_at;autoCreateTime"`

	// 可选：回挂会话（按 chat_id -> chat_histories.chat_id）
	ChatHistory AgentChat `gorm:"foreignKey:ChatID;references:ChatID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func (ChatHistory) TableName() string { return "chat_histories" }
