package model

import "time"

const (
	// OutboxStatusPending 表示消息已落 outbox，等待投递或等待下次重试窗口到达。
	OutboxStatusPending = "pending"
	// OutboxStatusPublished 表示消息已成功写入 Kafka，但尚未完成业务消费。
	OutboxStatusPublished = "published"
	// OutboxStatusConsumed 表示消息对应的业务逻辑已成功执行（本项目中即聊天记录已落库）。
	OutboxStatusConsumed = "consumed"
	// OutboxStatusDead 表示达到最大重试次数或出现不可恢复错误，进入死信终态。
	OutboxStatusDead = "dead"

	// OutboxBizTypeChatHistoryPersist 当前唯一业务类型：聊天记录异步持久化。
	OutboxBizTypeChatHistoryPersist = "chat_history_persist"
)

// AgentOutboxMessage 是 outbox 模式的核心表结构：
// 1. 先写本地数据库（保证事务内可见）；
// 2. 再由后台扫描并投递 Kafka；
// 3. 由消费者完成最终业务落库并回写状态。
type AgentOutboxMessage struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// BizType 决定消费者侧如何解释 Payload。
	BizType string `gorm:"column:biz_type;type:varchar(64);not null;index:idx_outbox_status_next,priority:3;comment:业务类型"`
	// Topic/MessageKey 用于 Kafka 路由与分区稳定性。
	Topic      string `gorm:"column:topic;type:varchar(128);not null;comment:Kafka Topic"`
	MessageKey string `gorm:"column:message_key;type:varchar(128);not null;comment:Kafka 消息键"`
	// Payload 存储业务 JSON，消费时再反序列化为具体 payload 结构。
	Payload string `gorm:"column:payload;type:longtext;not null;comment:业务载荷(JSON)"`
	// Status + NextRetryAt + RetryCount 共同描述“是否可被调度重试”。
	Status      string     `gorm:"column:status;type:varchar(32);not null;index:idx_outbox_status_next,priority:1;comment:pending/published/consumed/dead"`
	RetryCount  int        `gorm:"column:retry_count;not null;default:0;comment:已重试次数"`
	MaxRetry    int        `gorm:"column:max_retry;not null;default:20;comment:最大重试次数"`
	NextRetryAt *time.Time `gorm:"column:next_retry_at;index:idx_outbox_status_next,priority:2;comment:下次重试时间"`
	// LastError 记录最近一次失败原因，便于排障和可观测。
	LastError *string `gorm:"column:last_error;type:text;comment:最后一次错误"`
	// PublishedAt/ConsumedAt 便于统计“投递延迟”和“消费完成耗时”。
	PublishedAt *time.Time `gorm:"column:published_at;comment:投递到 Kafka 时间"`
	ConsumedAt  *time.Time `gorm:"column:consumed_at;comment:消费完成时间"`
	CreatedAt   *time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt   *time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AgentOutboxMessage) TableName() string {
	return "agent_outbox_messages"
}

// ChatHistoryPersistPayload 是“聊天记录持久化”消息的业务载荷。
// 注意：该载荷既会被写入 outbox，也会被封装到 Kafka Envelope 中传输。
type ChatHistoryPersistPayload struct {
	UserID         int    `json:"user_id"`
	ConversationID string `json:"conversation_id"`
	Role           string `json:"role"`
	Message        string `json:"message"`
}
