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

// OutboxDAO 封装 outbox 表读写逻辑。
// outbox 状态机约定：
// pending -> published -> consumed（成功终态）
// pending/published -> pending（失败重试）
// pending/published -> dead（不可恢复或达到最大重试）
type OutboxDAO struct {
	db *gorm.DB
}

func NewOutboxDAO(db *gorm.DB) *OutboxDAO {
	return &OutboxDAO{db: db}
}

// CreateChatHistoryMessage 创建“聊天记录持久化”的 outbox 消息。
// 关键点：
// 1) 初始状态为 pending；
// 2) NextRetryAt=now，允许被“首次同步投递”或“扫描器”立即处理；
// 3) payload 以 JSON 形式落表，保证消费端可重放。
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

// ListDueMessages 查询“到期可重试”的 pending 消息。
// 查询条件：status=pending 且 next_retry_at<=当前时间。
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

// MarkPublished 将消息标记为“已写入 Kafka”。
// 注意：
// 1) 仅在非终态（非 consumed/dead）下更新，避免覆盖最终状态；
// 2) 清理 next_retry_at，避免已投递消息继续被扫描器重复拉取。
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

// MarkDead 将消息置为死信终态。
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

// MarkFailedForRetry 在失败时推进重试状态。
// 关键点：
// 1) 事务 + FOR UPDATE 防并发覆盖（尤其是 dispatch/consume 并发场景）；
// 2) retry_count 自增；
// 3) 达到 max_retry 后转 dead，否则按指数退避设置 next_retry_at。
func (d *OutboxDAO) MarkFailedForRetry(ctx context.Context, id int64, reason string) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var msg model.AgentOutboxMessage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&msg).Error
		if err != nil {
			return err
		}
		// 终态直接跳过，保持幂等。
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

// PersistChatHistoryAndMarkConsumed 执行“消费业务”并回写 consumed。
// 这里把“写 chat_histories”与“更新 outbox 状态”放进同一事务，保证原子性。
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
		// 幂等保护：重复消费不重复落库。
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

// calcRetryBackoff 指数退避（上限 2^5=32 秒）。
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
