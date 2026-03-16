package model

import "time"

const (
	// OutboxStatusPending 表示消息已写入 outbox，等待投递或重试窗口到达。
	OutboxStatusPending = "pending"
	// OutboxStatusPublished 表示消息已成功写入 Kafka，但业务消费尚未完成。
	OutboxStatusPublished = "published"
	// OutboxStatusConsumed 表示消息对应业务处理已成功完成（最终态）。
	OutboxStatusConsumed = "consumed"
	// OutboxStatusDead 表示达到重试上限或出现不可恢复错误（最终态）。
	OutboxStatusDead = "dead"
)

// AgentOutboxMessage 是 outbox 状态机表模型。
//
// 关键说明：
// 1. EventType 映射到数据库 `biz_type` 列（为兼容历史表结构，不改 DDL）；
// 2. Payload 保存统一事件外壳 JSON；
// 3. Status/RetryCount/NextRetryAt 组成重试状态机。
type AgentOutboxMessage struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	EventType  string `gorm:"column:biz_type;type:varchar(64);not null;index:idx_outbox_status_next,priority:3;comment:事件类型"`
	Topic      string `gorm:"column:topic;type:varchar(128);not null;comment:Kafka Topic"`
	MessageKey string `gorm:"column:message_key;type:varchar(128);not null;comment:Kafka 消息键"`
	Payload    string `gorm:"column:payload;type:longtext;not null;comment:业务载荷(JSON)"`

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
