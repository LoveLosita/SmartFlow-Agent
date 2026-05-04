package model

import "time"

const (
	// ChannelFeishuWebhook 表示用户配置的是飞书 Webhook 触发器。
	ChannelFeishuWebhook = "feishu_webhook"
)

const (
	// AuthTypeNone 表示 webhook 不需要额外鉴权头。
	AuthTypeNone = "none"
	// AuthTypeBearer 表示 webhook 需要 Authorization: Bearer token。
	AuthTypeBearer = "bearer"
)

// UserNotificationChannel 保存单个用户的外部通知通道配置。
//
// 职责边界：
// 1. 只记录 user_id 到具体通知 provider 配置的映射；
// 2. 不记录 notification_records 投递状态，投递状态属于 NotificationRecord；
// 3. 当前 webhook_url / bearer_token 暂以明文字段承载，接口和日志必须脱敏。
type UserNotificationChannel struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement" json:"id"`

	UserID      int    `gorm:"column:user_id;not null;uniqueIndex:uk_user_notification_channel,priority:1;index:idx_user_notification_channel_user"`
	Channel     string `gorm:"column:channel;type:varchar(32);not null;uniqueIndex:uk_user_notification_channel,priority:2"`
	Enabled     bool   `gorm:"column:enabled;not null;default:true"`
	WebhookURL  string `gorm:"column:webhook_url;type:text;not null"`
	AuthType    string `gorm:"column:auth_type;type:varchar(32);not null;default:'none'"`
	BearerToken string `gorm:"column:bearer_token;type:text;not null"`

	LastTestStatus string     `gorm:"column:last_test_status;type:varchar(32)"`
	LastTestError  string     `gorm:"column:last_test_error;type:text"`
	LastTestAt     *time.Time `gorm:"column:last_test_at"`

	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime"`
	UpdatedAt time.Time `gorm:"column:updated_at;autoUpdateTime"`
}

func (UserNotificationChannel) TableName() string { return "user_notification_channels" }
