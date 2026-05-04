package model

import "time"

const (
	// RecordStatusPending 表示通知记录已落库，等待投递。
	RecordStatusPending = "pending"
	// RecordStatusSending 表示当前 worker 正在调用 provider。
	RecordStatusSending = "sending"
	// RecordStatusSent 表示 provider 明确返回成功。
	RecordStatusSent = "sent"
	// RecordStatusFailed 表示本次投递失败，但仍可重试。
	RecordStatusFailed = "failed"
	// RecordStatusDead 表示达到重试上限或不可恢复错误。
	RecordStatusDead = "dead"
	// RecordStatusSkipped 表示命中去重或配置关闭，本次不投递。
	RecordStatusSkipped = "skipped"
)

// NotificationRecord 是通知投递记录表模型。
//
// 职责边界：
// 1. 记录一次通知请求的幂等键、投递状态、provider 请求和响应审计；
// 2. 不保存用户 webhook 配置，配置由 UserNotificationChannel 维护；
// 3. 不承担主动调度 preview 或正式日程状态，二者只通过 trigger_id/preview_id 关联排障。
type NotificationRecord struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`

	Channel      string `gorm:"column:channel;type:varchar(32);not null;uniqueIndex:uk_notification_dedupe,priority:1;comment:通知渠道"`
	UserID       int    `gorm:"column:user_id;not null;index:idx_notification_user_created,priority:1"`
	TriggerID    string `gorm:"column:trigger_id;type:varchar(64);not null;index:idx_notification_trigger"`
	PreviewID    string `gorm:"column:preview_id;type:varchar(64);not null;index:idx_notification_preview"`
	TriggerType  string `gorm:"column:trigger_type;type:varchar(64);not null"`
	TargetType   string `gorm:"column:target_type;type:varchar(64);not null"`
	TargetID     int    `gorm:"column:target_id;not null"`
	DedupeKey    string `gorm:"column:dedupe_key;type:varchar(191);not null;uniqueIndex:uk_notification_dedupe,priority:2"`
	TargetURL    string `gorm:"column:target_url;type:varchar(255);not null;comment:站内预览链接"`
	SummaryText  string `gorm:"column:summary_text;type:text"`
	FallbackText string `gorm:"column:fallback_text;type:text"`
	FallbackUsed bool   `gorm:"column:fallback_used;not null;default:false"`

	Status        string     `gorm:"column:status;type:varchar(32);not null;default:'pending';index:idx_notification_status_retry,priority:1;comment:pending/sending/sent/failed/dead/skipped"`
	AttemptCount  int        `gorm:"column:attempt_count;not null;default:0"`
	MaxAttempts   int        `gorm:"column:max_attempts;not null;default:5"`
	NextRetryAt   *time.Time `gorm:"column:next_retry_at;index:idx_notification_status_retry,priority:2"`
	LastErrorCode *string    `gorm:"column:last_error_code;type:varchar(64)"`
	LastError     *string    `gorm:"column:last_error;type:text"`

	ProviderMessageID    *string    `gorm:"column:provider_message_id;type:varchar(128)"`
	ProviderRequestJSON  *string    `gorm:"column:provider_request_json;type:json"`
	ProviderResponseJSON *string    `gorm:"column:provider_response_json;type:json"`
	SentAt               *time.Time `gorm:"column:sent_at"`
	TraceID              string     `gorm:"column:trace_id;type:varchar(128)"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime;index:idx_notification_user_created,priority:2"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (NotificationRecord) TableName() string { return "notification_records" }
