package model

import "time"

const (
	OutboxStatusPending   = "pending"
	OutboxStatusPublished = "published"
	OutboxStatusConsumed  = "consumed"
	OutboxStatusDead      = "dead"

	OutboxBizTypeChatHistoryPersist = "chat_history_persist"
)

// AgentOutboxMessage 保存需要异步投递到 Kafka 的消息。
type AgentOutboxMessage struct {
	ID          int64      `gorm:"column:id;primaryKey;autoIncrement"`
	BizType     string     `gorm:"column:biz_type;type:varchar(64);not null;index:idx_outbox_status_next,priority:3;comment:业务类型"`
	Topic       string     `gorm:"column:topic;type:varchar(128);not null;comment:Kafka Topic"`
	MessageKey  string     `gorm:"column:message_key;type:varchar(128);not null;comment:Kafka 消息键"`
	Payload     string     `gorm:"column:payload;type:longtext;not null;comment:业务载荷(JSON)"`
	Status      string     `gorm:"column:status;type:varchar(32);not null;index:idx_outbox_status_next,priority:1;comment:pending/published/consumed/dead"`
	RetryCount  int        `gorm:"column:retry_count;not null;default:0;comment:已重试次数"`
	MaxRetry    int        `gorm:"column:max_retry;not null;default:20;comment:最大重试次数"`
	NextRetryAt *time.Time `gorm:"column:next_retry_at;index:idx_outbox_status_next,priority:2;comment:下次重试时间"`
	LastError   *string    `gorm:"column:last_error;type:text;comment:最后一次错误"`
	PublishedAt *time.Time `gorm:"column:published_at;comment:投递到 Kafka 时间"`
	ConsumedAt  *time.Time `gorm:"column:consumed_at;comment:消费完成时间"`
	CreatedAt   *time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   *time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AgentOutboxMessage) TableName() string {
	return "agent_outbox_messages"
}

// ChatHistoryPersistPayload 是“聊天记录持久化”消息体。
type ChatHistoryPersistPayload struct {
	UserID         int    `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Message        string `json:"message"`
}
