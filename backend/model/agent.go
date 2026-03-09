package model

import "time"

type UserSendMessageRequest struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Message        string `json:"message" binding:"required"`
	Model          string `json:"model,omitempty"`
	Thinking       bool   `json:"thinking,omitempty"`
}

type SSEResponse struct {
	Event string         `json:"event"`
	ID    int            `json:"id,omitempty"`
	Retry int64          `json:"retry,omitempty"`
	Data  SSEMessageData `json:"data"`
}

type SSEMessageData struct {
	Step    int    `json:"step,omitempty"`
	Message string `json:"message,omitempty"`
}

type AgentChat struct {
	ID            int64      `gorm:"column:id;primaryKey;autoIncrement;comment:自增ID"`
	ChatID        string     `gorm:"column:chat_id;type:varchar(36);not null;uniqueIndex:uk_chat_id;comment:会话UUID"`
	UserID        int        `gorm:"column:user_id;not null;index:idx_user_last,priority:1;index:idx_user_status,priority:1;comment:所属用户ID"`
	Title         *string    `gorm:"column:title;type:varchar(255);comment:会话标题"`
	SystemPrompt  *string    `gorm:"column:system_prompt;type:text;comment:系统提示词"`
	Model         *string    `gorm:"column:model;type:varchar(100);comment:模型标识"`
	MessageCount  int        `gorm:"column:message_count;not null;default:0;comment:消息总数"`
	TokensTotal   int        `gorm:"column:tokens_total;not null;default:0;comment:累计Token"`
	LastMessageAt *time.Time `gorm:"column:last_message_at;comment:最后消息时间"`
	Status        string     `gorm:"column:status;type:varchar(32);not null;default:active;index:idx_user_status,priority:2;comment:会话状态"`
	CreatedAt     *time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt     *time.Time `gorm:"column:updated_at;autoUpdateTime"`
	DeletedAt     *time.Time `gorm:"column:deleted_at;comment:软删除时间"`
}

func (AgentChat) TableName() string { return "agent_chats" }

type ChatHistory struct {
	ID             int        `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID         string     `gorm:"column:chat_id;type:varchar(36);not null;index:idx_user_chat,priority:2;index:idx_chat_id;comment:会话UUID"`
	UserID         int        `gorm:"column:user_id;not null;index:idx_user_chat,priority:1"`
	MessageContent *string    `gorm:"column:message_content;type:text;comment:消息内容"`
	Role           *string    `gorm:"column:role;type:varchar(32);comment:消息角色"`
	TokensConsumed int        `gorm:"column:tokens_consumed;not null;default:0;comment:本轮消耗Token"`
	CreatedAt      *time.Time `gorm:"column:created_at;autoCreateTime"`

	// 只保留从聊天记录到会话的单向关联，避免迁移时出现循环依赖。
	Chat AgentChat `gorm:"foreignKey:ChatID;references:ChatID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func (ChatHistory) TableName() string { return "chat_histories" }
