package dao

import (
	"context"
	"errors"
	"time"

	notificationmodel "github.com/LoveLosita/smartflow/backend/services/notification/model"
	"gorm.io/gorm"
)

// RecordDAO 管理 notification_records 投递状态机持久化。
//
// 职责边界：
// 1. 只负责通知记录的创建、去重查询、状态更新和重试扫描；
// 2. 不负责 provider 发送、幂等锁或 outbox consumed 标记；
// 3. 不读写 active_schedule_* 表，避免 notification 服务反向持有主动调度内部状态。
type RecordDAO struct {
	db *gorm.DB
}

func NewRecordDAO(db *gorm.DB) *RecordDAO {
	return &RecordDAO{db: db}
}

func (d *RecordDAO) WithTx(tx *gorm.DB) *RecordDAO {
	return &RecordDAO{db: tx}
}

func (d *RecordDAO) ensureDB() error {
	if d == nil || d.db == nil {
		return errors.New("notification record dao 未初始化")
	}
	return nil
}

func (d *RecordDAO) CreateNotificationRecord(ctx context.Context, record *notificationmodel.NotificationRecord) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if record == nil {
		return errors.New("notification record 不能为空")
	}
	return d.db.WithContext(ctx).Create(record).Error
}

func (d *RecordDAO) UpdateNotificationRecordFields(ctx context.Context, notificationID int64, updates map[string]any) error {
	if err := d.ensureDB(); err != nil {
		return err
	}
	if notificationID <= 0 {
		return errors.New("notification record id 不能为空")
	}
	if len(updates) == 0 {
		return nil
	}
	return d.db.WithContext(ctx).
		Model(&notificationmodel.NotificationRecord{}).
		Where("id = ?", notificationID).
		Updates(updates).Error
}

func (d *RecordDAO) GetNotificationRecordByID(ctx context.Context, notificationID int64) (*notificationmodel.NotificationRecord, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if notificationID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var record notificationmodel.NotificationRecord
	err := d.db.WithContext(ctx).Where("id = ?", notificationID).First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// FindNotificationRecordByDedupeKey 查询通知去重记录。
//
// 说明：
// 1. notification 第一版按 channel + dedupe_key 聚合去重；
// 2. 若返回 pending/sending/sent，上层应避免重复投递；
// 3. 若返回 failed，上层可以复用同一条记录进入 provider retry。
func (d *RecordDAO) FindNotificationRecordByDedupeKey(ctx context.Context, channel string, dedupeKey string) (*notificationmodel.NotificationRecord, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if channel == "" || dedupeKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var record notificationmodel.NotificationRecord
	err := d.db.WithContext(ctx).
		Where("channel = ? AND dedupe_key = ?", channel, dedupeKey).
		Order("created_at DESC, id DESC").
		First(&record).Error
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// ListRetryableNotificationRecords 查询到达重试时间的通知记录。
//
// 1. failed 记录按 next_retry_at 进入重试队列；
// 2. sending 记录只有超过租约才会回收，避免仍在执行的 provider 调用被重复放大；
// 3. 这让 retry scanner 同时覆盖显式失败重试和“发送中崩溃恢复”。
func (d *RecordDAO) ListRetryableNotificationRecords(ctx context.Context, now time.Time, sendingStaleBefore time.Time, limit int) ([]notificationmodel.NotificationRecord, error) {
	if err := d.ensureDB(); err != nil {
		return nil, err
	}
	if limit <= 0 || now.IsZero() {
		return []notificationmodel.NotificationRecord{}, nil
	}
	if sendingStaleBefore.IsZero() {
		sendingStaleBefore = now.Add(-10 * time.Minute)
	}
	var records []notificationmodel.NotificationRecord
	err := d.db.WithContext(ctx).
		Where(
			"(status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?) OR (status = ? AND updated_at <= ?)",
			notificationmodel.RecordStatusFailed,
			now,
			notificationmodel.RecordStatusSending,
			sendingStaleBefore,
		).
		Order("next_retry_at ASC, id ASC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

// ClaimRetryableNotificationRecord 抢占一条到期失败通知，避免多实例重复调用 provider。
//
// 职责边界：
// 1. 只做跨进程 claim，不发送通知、不推进最终投递状态；
// 2. failed 到期记录和 stale sending 记录都可以被回收为 sending；
// 3. 返回 claimed=false 表示记录已被其它实例抢走或状态已变化，调用方应跳过本次重试。
func (d *RecordDAO) ClaimRetryableNotificationRecord(ctx context.Context, notificationID int64, now time.Time, sendingStaleBefore time.Time) (bool, error) {
	if err := d.ensureDB(); err != nil {
		return false, err
	}
	if notificationID <= 0 || now.IsZero() {
		return false, nil
	}
	if sendingStaleBefore.IsZero() {
		sendingStaleBefore = now.Add(-10 * time.Minute)
	}
	result := d.db.WithContext(ctx).
		Model(&notificationmodel.NotificationRecord{}).
		Where(
			"id = ? AND ((status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?) OR (status = ? AND updated_at <= ?))",
			notificationID,
			notificationmodel.RecordStatusFailed,
			now,
			notificationmodel.RecordStatusSending,
			sendingStaleBefore,
		).
		Updates(map[string]any{
			"status":        notificationmodel.RecordStatusSending,
			"next_retry_at": nil,
			"updated_at":    now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected == 1, nil
}
