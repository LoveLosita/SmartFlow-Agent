package model

import "time"

type UserSendMessageRequest struct {
	ConversationID string `json:"conversation_id,omitempty"`
	Message        string `json:"message" binding:"required"`
	Model          string `json:"model,omitempty"`
	Thinking       bool   `json:"thinking,omitempty"`
}

// ChatHistoryPersistPayload 是“聊天消息持久化请求”业务 DTO。
//
// 职责边界：
// 1. 只描述聊天业务需要落库的核心字段；
// 2. 可被同步直写路径与异步事件路径复用；
// 3. 不包含 outbox/kafka 协议字段（这些字段由 infra 层统一封装）。
type ChatHistoryPersistPayload struct {
	UserID         int    `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Message        string `json:"message"`
}

// GetConversationMetaResponse 是会话元信息查询接口的返回结构。
// 说明：
// 1) title 可能为空字符串（表示标题尚未生成）；
// 2) has_title 便于前端快速判断是否需要展示默认占位文案；
// 3) 保留 message_count/last_message_at，方便前端后续扩展会话列表排序或角标。
type GetConversationMetaResponse struct {
	ConversationID string     `json:"conversation_id"`
	Title          string     `json:"title"`
	HasTitle       bool       `json:"has_title"`
	MessageCount   int        `json:"message_count"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
	Status         string     `json:"status"`
}

// GetConversationListItem 是“会话列表”中的单项数据。
//
// 职责边界：
// 1. 仅承载列表展示所需的轻量字段，不承载具体消息正文；
// 2. title 允许为空字符串，has_title 用于前端快速判断占位文案；
// 3. message_count/last_message_at 用于排序展示与角标扩展。
type GetConversationListItem struct {
	ConversationID string     `json:"conversation_id"`
	Title          string     `json:"title"`
	HasTitle       bool       `json:"has_title"`
	MessageCount   int        `json:"message_count"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}

// GetConversationListResponse 是“获取用户会话列表”接口的统一响应体。
//
// 职责边界：
// 1. list 承载当前页数据；
// 2. page/page_size/total/has_more 承载分页语义；
// 3. 不负责返回会话正文明细（正文仍由聊天历史接口承担）。
type GetConversationListResponse struct {
	List     []GetConversationListItem `json:"list"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
	Total    int64                     `json:"total"`
	HasMore  bool                      `json:"has_more"`
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
