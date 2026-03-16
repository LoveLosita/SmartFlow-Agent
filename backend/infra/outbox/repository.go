package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 是 outbox 状态机仓储。
//
// 职责边界：
// 1. 只负责 outbox 状态流转与通用事务编排；
// 2. 不负责任何业务语义（例如聊天/任务/标题等具体落库）；
// 3. 消费成功时通过回调把业务动作注入同一事务，保证原子一致。
type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// WithTx 用外部事务句柄构造同事务仓储实例。
func (d *Repository) WithTx(tx *gorm.DB) *Repository {
	return &Repository{db: tx}
}

// CreateMessage 把事件写入 outbox（入队）。
//
// 步骤：
// 1. 序列化 payload；
// 2. 初始化 pending 状态；
// 3. 写入 outbox 并返回 outbox_id。
func (d *Repository) CreateMessage(ctx context.Context, eventType, topic, messageKey string, payload any, maxRetry int) (int64, error) {
	if maxRetry <= 0 {
		maxRetry = 20
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	msg := model.AgentOutboxMessage{
		EventType:   eventType,
		Topic:       topic,
		MessageKey:  messageKey,
		Payload:     string(raw),
		Status:      model.OutboxStatusPending,
		RetryCount:  0,
		MaxRetry:    maxRetry,
		NextRetryAt: &now,
	}

	if err = d.db.WithContext(ctx).Create(&msg).Error; err != nil {
		return 0, err
	}
	return msg.ID, nil
}

func (d *Repository) GetByID(ctx context.Context, id int64) (*model.AgentOutboxMessage, error) {
	var msg model.AgentOutboxMessage
	if err := d.db.WithContext(ctx).Where("id = ?", id).First(&msg).Error; err != nil {
		return nil, err
	}
	return &msg, nil
}

// ListDueMessages 拉取到期可投递消息。
func (d *Repository) ListDueMessages(ctx context.Context, limit int) ([]model.AgentOutboxMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now()
	var messages []model.AgentOutboxMessage
	err := d.db.WithContext(ctx).
		Where("status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?", model.OutboxStatusPending, now).
		Order("next_retry_at ASC, id ASC").
		Limit(limit).
		Find(&messages).Error
	if err != nil {
		return nil, err
	}
	return messages, nil
}

// MarkPublished 标记为已投递 Kafka。
func (d *Repository) MarkPublished(ctx context.Context, id int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":        model.OutboxStatusPublished,
		"published_at":  &now,
		"last_error":    nil,
		"next_retry_at": nil,
	}
	result := d.db.WithContext(ctx).
		Model(&model.AgentOutboxMessage{}).
		Where("id = ? AND status NOT IN (?, ?)", id, model.OutboxStatusConsumed, model.OutboxStatusDead).
		Updates(updates)
	return result.Error
}

// MarkDead 标记为死信。
func (d *Repository) MarkDead(ctx context.Context, id int64, reason string) error {
	now := time.Now()
	lastErr := truncateError(reason)
	updates := map[string]interface{}{
		"status":        model.OutboxStatusDead,
		"last_error":    &lastErr,
		"next_retry_at": nil,
		"updated_at":    now,
	}
	return d.db.WithContext(ctx).Model(&model.AgentOutboxMessage{}).Where("id = ?", id).Updates(updates).Error
}

// MarkFailedForRetry 记录一次可重试失败并推进重试窗口。
//
// 步骤：
// 1. 行级锁读取当前状态；
// 2. 最终态幂等短路；
// 3. retry_count+1；
// 4. 计算 next_retry_at 或 dead；
// 5. 写回状态快照。
func (d *Repository) MarkFailedForRetry(ctx context.Context, id int64, reason string) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var msg model.AgentOutboxMessage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&msg).Error
		if err != nil {
			return err
		}

		if msg.Status == model.OutboxStatusConsumed || msg.Status == model.OutboxStatusDead {
			return nil
		}

		nextRetryCount := msg.RetryCount + 1
		now := time.Now()
		status := model.OutboxStatusPending
		var nextRetryAt *time.Time
		if nextRetryCount >= msg.MaxRetry {
			status = model.OutboxStatusDead
			nextRetryAt = nil
		} else {
			t := now.Add(calcRetryBackoff(nextRetryCount))
			nextRetryAt = &t
		}

		lastErr := truncateError(reason)
		updates := map[string]interface{}{
			"status":        status,
			"retry_count":   nextRetryCount,
			"last_error":    &lastErr,
			"next_retry_at": nextRetryAt,
			"updated_at":    now,
		}
		return tx.Model(&model.AgentOutboxMessage{}).Where("id = ?", id).Updates(updates).Error
	})
}

// ConsumeAndMarkConsumed 是通用“消费成功事务入口”。
//
// 步骤：
// 1. 事务内锁定 outbox 记录；
// 2. 已 consumed/dead 时幂等返回；
// 3. 执行业务回调 fn(tx)；
// 4. 业务成功后统一标记 consumed。
func (d *Repository) ConsumeAndMarkConsumed(ctx context.Context, outboxID int64, fn func(tx *gorm.DB) error) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var outboxMsg model.AgentOutboxMessage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", outboxID).First(&outboxMsg).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if outboxMsg.Status == model.OutboxStatusConsumed || outboxMsg.Status == model.OutboxStatusDead {
			return nil
		}

		if fn != nil {
			if err = fn(tx); err != nil {
				return err
			}
		}

		now := time.Now()
		updates := map[string]interface{}{
			"status":        model.OutboxStatusConsumed,
			"consumed_at":   &now,
			"last_error":    nil,
			"next_retry_at": nil,
			"updated_at":    now,
		}
		return tx.Model(&model.AgentOutboxMessage{}).Where("id = ?", outboxID).Updates(updates).Error
	})
}

func calcRetryBackoff(retryCount int) time.Duration {
	if retryCount <= 0 {
		return time.Second
	}
	if retryCount > 6 {
		retryCount = 6
	}
	return time.Second * time.Duration(1<<(retryCount-1))
}

func truncateError(reason string) string {
	if len(reason) <= 2000 {
		return reason
	}
	return reason[:2000]
}
