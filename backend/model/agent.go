package model

import "time"

type UserSendMessageRequest struct {
	ConversationID string         `json:"conversation_id,omitempty"`
	Message        string         `json:"message" binding:"required"`
	Model          string         `json:"model,omitempty"`
	Thinking       bool           `json:"thinking,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
}

type ChatHistoryPersistPayload struct {
	UserID                      int     `json:"user_id"`
	ConversationID              string  `json:"conversation_id"`
	Role                        string  `json:"role"`
	Message                     string  `json:"message"`
	ReasoningContent            string  `json:"reasoning_content,omitempty"`
	ReasoningDurationSeconds    int     `json:"reasoning_duration_seconds,omitempty"`
	RetryGroupID                *string `json:"retry_group_id,omitempty"`
	RetryIndex                  *int    `json:"retry_index,omitempty"`
	RetryFromUserMessageID      *int    `json:"retry_from_user_message_id,omitempty"`
	RetryFromAssistantMessageID *int    `json:"retry_from_assistant_message_id,omitempty"`
	TokensConsumed              int     `json:"tokens_consumed"`
}

type ChatTokenUsageAdjustPayload struct {
	UserID         int       `json:"user_id"`
	ConversationID string    `json:"conversation_id"`
	TokensDelta    int       `json:"tokens_delta"`
	Reason         string    `json:"reason"`
	TriggeredAt    time.Time `json:"triggered_at"`
}

type GetConversationMetaResponse struct {
	ConversationID string     `json:"conversation_id"`
	Title          string     `json:"title"`
	HasTitle       bool       `json:"has_title"`
	MessageCount   int        `json:"message_count"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
	Status         string     `json:"status"`
}

type GetConversationListItem struct {
	ConversationID string     `json:"conversation_id"`
	Title          string     `json:"title"`
	HasTitle       bool       `json:"has_title"`
	MessageCount   int        `json:"message_count"`
	LastMessageAt  *time.Time `json:"last_message_at,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      *time.Time `json:"created_at,omitempty"`
}

type GetConversationListResponse struct {
	List     []GetConversationListItem `json:"list"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
	Limit    int                       `json:"limit"`
	Total    int64                     `json:"total"`
	HasMore  bool                      `json:"has_more"`
}

type GetConversationHistoryItem struct {
	ID                       int        `json:"id,omitempty"`
	Role                     string     `json:"role"`
	Content                  string     `json:"content"`
	CreatedAt                *time.Time `json:"created_at,omitempty"`
	ReasoningContent         string     `json:"reasoning_content,omitempty"`
	ReasoningDurationSeconds int        `json:"reasoning_duration_seconds,omitempty"`
	RetryGroupID             *string    `json:"retry_group_id"`
	RetryIndex               *int       `json:"retry_index"`
	RetryTotal               *int       `json:"retry_total"`
}

type SchedulePlanPreviewCache struct {
	UserID         int                   `json:"user_id"`
	ConversationID string                `json:"conversation_id"`
	TraceID        string                `json:"trace_id,omitempty"`
	Summary        string                `json:"summary"`
	CandidatePlans []UserWeekSchedule    `json:"candidate_plans"`
	TaskClassIDs   []int                 `json:"task_class_ids,omitempty"`
	HybridEntries  []HybridScheduleEntry `json:"hybrid_entries,omitempty"`
	AllocatedItems []TaskClassItem       `json:"allocated_items,omitempty"`
	GeneratedAt    time.Time             `json:"generated_at"`
}

type GetSchedulePlanPreviewResponse struct {
	ConversationID string             `json:"conversation_id"`
	TraceID        string             `json:"trace_id,omitempty"`
	Summary        string             `json:"summary"`
	CandidatePlans []UserWeekSchedule `json:"candidate_plans"`
	GeneratedAt    time.Time          `json:"generated_at"`
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
	ID                          int        `gorm:"column:id;primaryKey;autoIncrement"`
	ChatID                      string     `gorm:"column:chat_id;type:varchar(36);not null;index:idx_user_chat,priority:2;index:idx_chat_id;comment:会话UUID"`
	UserID                      int        `gorm:"column:user_id;not null;index:idx_user_chat,priority:1"`
	MessageContent              *string    `gorm:"column:message_content;type:text;comment:消息内容"`
	ReasoningContent            *string    `gorm:"column:reasoning_content;type:text;comment:deep reasoning text"`
	ReasoningDurationSeconds    int        `gorm:"column:reasoning_duration_seconds;not null;default:0;comment:deep reasoning duration seconds"`
	RetryGroupID                *string    `gorm:"column:retry_group_id;type:varchar(64);index:idx_retry_group;comment:retry group id"`
	RetryIndex                  *int       `gorm:"column:retry_index;comment:retry page index"`
	RetryFromUserMessageID      *int       `gorm:"column:retry_from_user_message_id;comment:source user message id"`
	RetryFromAssistantMessageID *int       `gorm:"column:retry_from_assistant_message_id;comment:source assistant message id"`
	Role                        *string    `gorm:"column:role;type:varchar(32);comment:消息角色"`
	TokensConsumed              int        `gorm:"column:tokens_consumed;not null;default:0;comment:本轮消耗Token"`
	CreatedAt                   *time.Time `gorm:"column:created_at;autoCreateTime"`

	Chat AgentChat `gorm:"foreignKey:ChatID;references:ChatID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE"`
}

func (ChatHistory) TableName() string { return "chat_histories" }
