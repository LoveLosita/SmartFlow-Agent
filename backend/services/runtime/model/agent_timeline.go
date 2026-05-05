package model

import (
	"strings"
	"time"
)

// AgentTimelineKind 定义会话时间线事件类型。
//
// 说明：
// 1. 这些类型面向前端渲染，要求语义稳定，不随节点内部实现细节频繁变化；
// 2. 文本消息和卡片事件共用一条时间线，前端只按 seq 顺序渲染；
// 3. token 统计仍以 chat_histories / agent_chats 为准，时间线只负责展示顺序与结构承载。
const (
	AgentTimelineKindUserText          = "user_text"
	AgentTimelineKindAssistantText     = "assistant_text"
	AgentTimelineKindToolCall          = "tool_call"
	AgentTimelineKindToolResult        = "tool_result"
	AgentTimelineKindConfirmRequest    = "confirm_request"
	AgentTimelineKindBusinessCard      = "business_card"
	AgentTimelineKindScheduleCompleted = "schedule_completed"
	AgentTimelineKindThinkingSummary   = "thinking_summary"
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
	TokensConsumed int        `gorm:"column:tokens_consumed;not null;default:0;comment:该事件关联 token，默认 0"`
	CreatedAt      *time.Time `gorm:"column:created_at;autoCreateTime;index:idx_timeline_user_chat_created,priority:3"`
}

func (AgentTimelineEvent) TableName() string { return "agent_timeline_events" }

// ChatTimelinePersistPayload 定义时间线单条事件落库输入。
//
// 职责边界：
// 1. 只表达一次“写入 agent_timeline_events”的最小字段集合；
// 2. Content 面向纯文本类事件，结构化事件更多依赖 PayloadJSON；
// 3. thinking_summary 事件要求 PayloadJSON 内只保留 detail_summary 与必要 metadata。
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

// Normalize 负责收敛时间线持久化载荷的基础口径。
//
// 职责边界：
// 1. 只做字符串 trim 和非负数兜底；
// 2. 不负责 thinking_summary 的业务裁剪；
// 3. 返回副本，避免调用方意外修改原对象。
func (p ChatTimelinePersistPayload) Normalize() ChatTimelinePersistPayload {
	p.ConversationID = strings.TrimSpace(p.ConversationID)
	p.Kind = strings.TrimSpace(p.Kind)
	p.Role = strings.TrimSpace(p.Role)
	p.Content = strings.TrimSpace(p.Content)
	p.PayloadJSON = strings.TrimSpace(p.PayloadJSON)
	if p.Seq < 0 {
		p.Seq = 0
	}
	if p.TokensConsumed < 0 {
		p.TokensConsumed = 0
	}
	return p
}

// HasValidIdentity 判断 payload 是否具备最小可持久化主键语义。
func (p ChatTimelinePersistPayload) HasValidIdentity() bool {
	normalized := p.Normalize()
	return normalized.UserID > 0 &&
		normalized.ConversationID != "" &&
		normalized.Seq > 0 &&
		normalized.Kind != ""
}

// MatchesStoredEvent 判断 payload 与库中事件是否可视为“同一条业务事件”。
//
// 说明：
// 1. 主要用于 outbox 重放时识别“唯一键冲突但其实已经成功落库”的场景；
// 2. 只比较持久化字段，不比较 created_at / id 这类存储侧派生值；
// 3. 返回 true 时，上层可以把 seq 冲突视为幂等成功。
func (p ChatTimelinePersistPayload) MatchesStoredEvent(event AgentTimelineEvent) bool {
	normalized := p.Normalize()
	return event.UserID == normalized.UserID &&
		strings.TrimSpace(event.ChatID) == normalized.ConversationID &&
		event.Seq == normalized.Seq &&
		strings.TrimSpace(event.Kind) == normalized.Kind &&
		trimTimelinePointerString(event.Role) == normalized.Role &&
		trimTimelinePointerString(event.Content) == normalized.Content &&
		trimTimelinePointerString(event.Payload) == normalized.PayloadJSON &&
		event.TokensConsumed == normalized.TokensConsumed
}

// IsTimelineSeqConflictError 判断 error 是否属于时间线 seq 唯一键冲突。
//
// 说明：
// 1. MySQL / PostgreSQL / SQLite 的重复键报错文案并不完全一致，这里用宽松文本匹配；
// 2. 该函数只用于“是否进入幂等/补 seq 分支”的判断，不承担精确错误分类职责；
// 3. 若未来统一抽数据库错误码适配层，应优先替换这里而不是继续复制判断逻辑。
func IsTimelineSeqConflictError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate entry") ||
		strings.Contains(lower, "duplicate key") ||
		strings.Contains(lower, "unique constraint") ||
		strings.Contains(lower, "unique violation") ||
		strings.Contains(lower, "error 1062") ||
		strings.Contains(lower, "uk_timeline_user_chat_seq")
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

func trimTimelinePointerString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
