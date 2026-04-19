package model

import "time"

// AgentTimelineKind 定义会话时间线事件类型。
//
// 说明：
// 1. 这些类型面向前端渲染，要求语义稳定，不随节点内部实现细节频繁变化；
// 2. 文本消息和卡片事件共用一条时间线，前端只按 seq 顺序渲染；
// 3. token 统计仍以 chat_histories / agent_chats 为准，时间线只做展示顺序与结构承载。
const (
	AgentTimelineKindUserText          = "user_text"
	AgentTimelineKindAssistantText     = "assistant_text"
	AgentTimelineKindToolCall          = "tool_call"
	AgentTimelineKindToolResult        = "tool_result"
	AgentTimelineKindConfirmRequest    = "confirm_request"
	AgentTimelineKindScheduleCompleted = "schedule_completed"
)

// AgentTimelineEvent 表示会话里“可展示事件”的统一持久化记录。
//
// 职责边界：
// 1. 只承载“顺序 + 展示信息”，不替代 chat_histories 的消息账本职责；
// 2. seq 是同一会话内的单调递增顺序号，用于刷新后重建展示顺序；
// 3. payload 只保存前端渲染需要的结构化信息，不存整个运行时快照。
type AgentTimelineEvent struct {
	ID             int64      `gorm:"column:id;primaryKey;autoIncrement"`
	UserID         int        `gorm:"column:user_id;not null;uniqueIndex:uk_timeline_user_chat_seq,priority:1;index:idx_timeline_user_chat_created,priority:1;comment:所属用户ID"`
	ChatID         string     `gorm:"column:chat_id;type:varchar(36);not null;uniqueIndex:uk_timeline_user_chat_seq,priority:2;index:idx_timeline_user_chat_created,priority:2;comment:会话UUID"`
	Seq            int64      `gorm:"column:seq;not null;uniqueIndex:uk_timeline_user_chat_seq,priority:3;comment:会话内顺序号"`
	Kind           string     `gorm:"column:kind;type:varchar(64);not null;comment:事件类型"`
	Role           *string    `gorm:"column:role;type:varchar(32);comment:消息角色"`
	Content        *string    `gorm:"column:content;type:text;comment:正文内容"`
	Payload        *string    `gorm:"column:payload;type:json;comment:结构化负载"`
	TokensConsumed int        `gorm:"column:tokens_consumed;not null;default:0;comment:该事件关联token，默认0"`
	CreatedAt      *time.Time `gorm:"column:created_at;autoCreateTime;index:idx_timeline_user_chat_created,priority:3"`
}

func (AgentTimelineEvent) TableName() string { return "agent_timeline_events" }

// ChatTimelinePersistPayload 定义时间线单条事件落库输入。
type ChatTimelinePersistPayload struct {
	UserID         int    `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Seq            int64  `json:"seq"`
	Kind           string `json:"kind"`
	Role           string `json:"role,omitempty"`
	Content        string `json:"content,omitempty"`
	PayloadJSON    string `json:"payload_json,omitempty"`
	TokensConsumed int    `json:"tokens_consumed"`
}

// GetConversationTimelineItem 定义前端读取时间线接口的单条返回项。
type GetConversationTimelineItem struct {
	ID             int64          `json:"id,omitempty"`
	Seq            int64          `json:"seq"`
	Kind           string         `json:"kind"`
	Role           string         `json:"role,omitempty"`
	Content        string         `json:"content,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
	TokensConsumed int            `json:"tokens_consumed,omitempty"`
	CreatedAt      *time.Time     `json:"created_at,omitempty"`
}
