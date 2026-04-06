package model

import "time"

// AgentStateSnapshotRecord 是 agent 运行态快照的 MySQL 持久化模型。
//
// 设计说明：
// 1. 通过 outbox 异步写入，Redis 快照到期后仍可从此表恢复；
// 2. 按 conversation_id 索引，支持按会话查询最近快照；
// 3. phase 字段便于按阶段过滤和清理；
// 4. 不做历史版本管理（覆盖写），同一会话只保留最新快照。
type AgentStateSnapshotRecord struct {
	ID             int64     `gorm:"column:id;primaryKey;autoIncrement"`
	ConversationID string    `gorm:"column:conversation_id;type:varchar(128);not null;uniqueIndex:idx_conversation_snapshot"`
	UserID         int       `gorm:"column:user_id;not null;index:idx_user_snapshot"`
	Phase          string    `gorm:"column:phase;type:varchar(32);not null"`
	SnapshotJSON   string    `gorm:"column:snapshot_json;type:longtext;not null"`
	CreatedAt      time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt      time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (AgentStateSnapshotRecord) TableName() string {
	return "agent_state_snapshot_records"
}
