package dao

import (
	"context"
	"errors"
	"time"

	notificationmodel "github.com/LoveLosita/smartflow/backend/services/notification/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ChannelDAO 管理用户外部通知通道配置。
//
// 职责边界：
// 1. 只负责 user_notification_channels 的基础读写；
// 2. 不负责 webhook 请求发送、notification_records 状态机或 outbox 消费；
// 3. webhook_url / bearer_token 的脱敏由 service 层处理，DAO 保持真实持久化值。
type ChannelDAO struct {
	db *gorm.DB
}

func NewChannelDAO(db *gorm.DB) *ChannelDAO {
	return &ChannelDAO{db: db}
}

func (d *ChannelDAO) WithTx(tx *gorm.DB) *ChannelDAO {
	return &ChannelDAO{db: tx}
}

func (d *ChannelDAO) ensureDB() error {
	if d == nil || d.db == nil {
		return errors.New("notification channel dao 未初始化")
	}
	return nil
}

// UpsertUserNotificationChannel 按 user_id + channel 幂等保存用户通知配置。
//
// 说明：
// 1. 只覆盖开关、webhook、鉴权配置和 updated_at；
// 2. 不清空 last_test_*，避免用户保存配置后丢掉最近一次测试结果；
// 3. channel.ID 由数据库自增，调用方不应依赖传入 ID。
func (d *ChannelDAO) UpsertUserNotificationChannel(ctx context.Context, channel *notificationmodel.UserNotificationChannel) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if channel == nil || channel.UserID <= 0 || channel.Channel == "" {
		return errors.New("notification channel 必须包含 user_id 和 channel")
	}
	now := time.Now()
	values := map[string]any{
		"user_id":      channel.UserID,
		"channel":      channel.Channel,
		"enabled":      channel.Enabled,
		"webhook_url":  channel.WebhookURL,
		"auth_type":    channel.AuthType,
		"bearer_token": channel.BearerToken,
		"created_at":   now,
		"updated_at":   now,
	}
	return d.db.WithContext(ctx).
		Model(&notificationmodel.UserNotificationChannel{}).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "user_id"}, {Name: "channel"}},
			DoUpdates: clause.Assignments(map[string]any{
				"enabled":      channel.Enabled,
				"webhook_url":  channel.WebhookURL,
				"auth_type":    channel.AuthType,
				"bearer_token": channel.BearerToken,
				"updated_at":   now,
			}),
		}).
		Create(values).Error
}

// GetUserNotificationChannel 查询用户指定通知通道配置。
func (d *ChannelDAO) GetUserNotificationChannel(ctx context.Context, userID int, channel string) (*notificationmodel.UserNotificationChannel, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if userID <= 0 || channel == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var row notificationmodel.UserNotificationChannel
	err := d.db.WithContext(ctx).
		Where("user_id = ? AND channel = ?", userID, channel).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// DeleteUserNotificationChannel 删除用户指定通知通道配置。
//
// 说明：当前表不保留软删除列；删除后再次保存会重新创建配置。
func (d *ChannelDAO) DeleteUserNotificationChannel(ctx context.Context, userID int, channel string) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if userID <= 0 || channel == "" {
		return nil
	}
	return d.db.WithContext(ctx).
		Where("user_id = ? AND channel = ?", userID, channel).
		Delete(&notificationmodel.UserNotificationChannel{}).Error
}

// UpdateUserNotificationChannelTestResult 回写用户 webhook 测试结果。
func (d *ChannelDAO) UpdateUserNotificationChannelTestResult(ctx context.Context, userID int, channel string, status string, testErr string, testedAt time.Time) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if userID <= 0 || channel == "" {
		return errors.New("user_id 和 channel 不能为空")
	}
	updates := map[string]any{
		"last_test_status": status,
		"last_test_error":  testErr,
		"last_test_at":     &testedAt,
	}
	return d.db.WithContext(ctx).
		Model(&notificationmodel.UserNotificationChannel{}).
		Where("user_id = ? AND channel = ?", userID, channel).
		Updates(updates).Error
}
