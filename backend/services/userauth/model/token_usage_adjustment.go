package model

import "time"

// TokenUsageAdjustment 是 user/auth 服务内的 token 账本幂等表。
//
// 职责边界：
// 1. 只记录“某个 outbox/event_id 是否已经调整过 users.token_usage”；
// 2. 不保存 agent 会话 token_total，那个统计仍属于 agent 领域；
// 3. event_id 作为主键，配合 users.token_usage 更新放在同一个 MySQL 事务里，避免并发重放重复记账。
type TokenUsageAdjustment struct {
	EventID    string    `gorm:"column:event_id;type:varchar(64);primaryKey;comment:来源事件ID"`
	UserID     int       `gorm:"column:user_id;not null;index:idx_userauth_token_adjust_user;comment:用户ID"`
	TokenDelta int       `gorm:"column:token_delta;not null;comment:本次增加的 token 用量"`
	CreatedAt  time.Time `gorm:"column:created_at;autoCreateTime;comment:创建时间"`
}

func (TokenUsageAdjustment) TableName() string {
	return "user_token_usage_adjustments"
}
