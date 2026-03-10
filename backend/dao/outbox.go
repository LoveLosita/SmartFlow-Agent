package dao

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OutboxDAO struct {
	db *gorm.DB
}

func NewOutboxDAO(db *gorm.DB) *OutboxDAO {
	return &OutboxDAO{db: db}
}

func (d *OutboxDAO) CreateChatHistoryMessage(ctx context.Context, topic, messageKey string, payload model.ChatHistoryPersistPayload, maxRetry int) (int64, error) {
	if maxRetry <= 0 {
		maxRetry = 20
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	msg := model.AgentOutboxMessage{
		BizType:     model.OutboxBizTypeChatHistoryPersist,
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

func (d *OutboxDAO) GetByID(ctx context.Context, id int64) (*model.AgentOutboxMessage, error) {
	var msg model.AgentOutboxMessage
	if err := d.db.WithContext(ctx).Where("id = ?", id).First(&msg).Error; err != nil {
		return nil, err
	}
	return &msg, nil
}

func (d *OutboxDAO) ListDueMessages(ctx context.Context, limit int) ([]model.AgentOutboxMessage, error) {
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

// MarkPublished 仅在消息未进入最终态时更新为 published，避免覆盖 consumed/dead。
func (d *OutboxDAO) MarkPublished(ctx context.Context, id int64) error {
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

func (d *OutboxDAO) MarkDead(ctx context.Context, id int64, reason string) error {
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

func (d *OutboxDAO) MarkFailedForRetry(ctx context.Context, id int64, reason string) error {
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

func (d *OutboxDAO) PersistChatHistoryAndMarkConsumed(ctx context.Context, outboxID int64, payload model.ChatHistoryPersistPayload) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var outboxMsg model.AgentOutboxMessage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", outboxID).First(&outboxMsg).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}
		if outboxMsg.Status == model.OutboxStatusConsumed {
			return nil
		}
		if outboxMsg.Status == model.OutboxStatusDead {
			return nil
		}

		chatMsg := payload.Message
		chatRole := payload.Role
		history := model.ChatHistory{
			UserID:         payload.UserID,
			ChatID:         payload.ConversationID,
			MessageContent: &chatMsg,
			Role:           &chatRole,
		}
		if err = tx.Create(&history).Error; err != nil {
			return err
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
