package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct {
	db *gorm.DB
}

// NewRepository 构造 outbox 仓储。
// 该仓储只关心“数据库状态机”，不关心 Kafka 投递/消费。
func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// CreateMessage 是通用 outbox 入队入口。
//
// 设计说明：
// 1) 该方法只做“把消息安全写入本地 outbox 表”，不做任何 Kafka 网络调用；
// 2) next_retry_at 初始化为当前时间，表示“可立即被扫描器捞取”；
// 3) biz_type 由业务方传入，用于消费侧分发到不同处理器；
// 4) payload 会被序列化为 JSON 字符串存入 payload 字段，后续再按 biz_type 反序列化。
//
// 这也是 Outbox 模式的核心：请求路径只承担本地写库成本，把外部系统不确定性（Kafka 延迟/抖动）
// 转移给后台异步循环处理。
func (d *Repository) CreateMessage(ctx context.Context, bizType, topic, messageKey string, payload any, maxRetry int) (int64, error) {
	// 1. 防御式兜底：若调用方未传 maxRetry，则统一使用默认值 20。
	//    这样可以避免某些链路遗漏配置导致消息无限重试或零重试。
	if maxRetry <= 0 {
		maxRetry = 20
	}

	// 2. 先把业务载荷序列化成 JSON 字符串。
	//    序列化失败属于“请求入队前失败”，此时不应创建 outbox 记录，直接返回错误即可。
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	// 3. 组装 outbox 初始记录：
	//    - status=pending：表示待投递；
	//    - retry_count=0：尚未重试；
	//    - next_retry_at=now：扫描器可立即捞取并尝试首次投递。
	now := time.Now()
	msg := model.AgentOutboxMessage{
		BizType:     bizType,
		Topic:       topic,
		MessageKey:  messageKey,
		Payload:     string(raw),
		Status:      model.OutboxStatusPending,
		RetryCount:  0,
		MaxRetry:    maxRetry,
		NextRetryAt: &now,
	}

	// 4. 落库成功后返回 outbox 主键，供上层日志/追踪链路使用。
	if err = d.db.WithContext(ctx).Create(&msg).Error; err != nil {
		return 0, err
	}
	return msg.ID, nil
}

// CreateChatHistoryMessage 是聊天记录持久化的兼容入口。
// 说明：为了避免现有业务调用一次性改太多，先保留该方法作为 CreateMessage 的薄封装。
func (d *Repository) CreateChatHistoryMessage(ctx context.Context, topic, messageKey string, payload model.ChatHistoryPersistPayload, maxRetry int) (int64, error) {
	return d.CreateMessage(ctx, model.OutboxBizTypeChatHistoryPersist, topic, messageKey, payload, maxRetry)
}

// GetByID 按主键读取 outbox 记录。
// 该方法通常用于 dispatch 前“再读一次最新状态”，避免使用过期快照。
func (d *Repository) GetByID(ctx context.Context, id int64) (*model.AgentOutboxMessage, error) {
	var msg model.AgentOutboxMessage
	if err := d.db.WithContext(ctx).Where("id = ?", id).First(&msg).Error; err != nil {
		return nil, err
	}
	return &msg, nil
}

// ListDueMessages 拉取“到期可投递”的 pending 消息。
// 条件说明：
// 1) status = pending：只处理待投递状态；
// 2) next_retry_at <= now：到达可重试/可首次投递时间；
// 3) 按 next_retry_at + id 升序：保证老消息优先，降低饥饿概率。
func (d *Repository) ListDueMessages(ctx context.Context, limit int) ([]model.AgentOutboxMessage, error) {
	// 1. 限流兜底，避免误传 0 导致一次拉取过多消息。
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
func (d *Repository) MarkPublished(ctx context.Context, id int64) error {
	// 1. published 代表“已成功写入 Kafka”。
	// 2. 清理 last_error/next_retry_at，表示当前无需重试。
	now := time.Now()
	updates := map[string]interface{}{
		"status":        model.OutboxStatusPublished,
		"published_at":  &now,
		"last_error":    nil,
		"next_retry_at": nil,
	}
	// 3. 额外加状态保护，避免并发下把 consumed/dead 错误覆盖回 published。
	result := d.db.WithContext(ctx).
		Model(&model.AgentOutboxMessage{}).
		Where("id = ? AND status NOT IN (?, ?)", id, model.OutboxStatusConsumed, model.OutboxStatusDead).
		Updates(updates)
	return result.Error
}

// MarkDead 把消息标记为死信（最终失败，不再重试）。
// 常见场景：载荷不可反序列化、biz_type 未注册等“不可恢复错误”。
func (d *Repository) MarkDead(ctx context.Context, id int64, reason string) error {
	// 1. 错误文本统一裁剪，避免超长错误撑爆字段或日志。
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

// MarkFailedForRetry 把一次失败写回 outbox 状态机，并计算下一次重试窗口。
// 该方法必须在事务内完成“读当前状态 + 写新状态”，保证并发时计数和状态一致。
func (d *Repository) MarkFailedForRetry(ctx context.Context, id int64, reason string) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 行级锁读取，避免多个 goroutine 同时更新同一条消息导致 retry_count 乱序。
		var msg model.AgentOutboxMessage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&msg).Error
		if err != nil {
			return err
		}

		// 2. 若已是最终态（consumed/dead），直接幂等返回。
		//    这样即使出现重复调用，也不会把最终态改坏。
		if msg.Status == model.OutboxStatusConsumed || msg.Status == model.OutboxStatusDead {
			return nil
		}

		// 3. 递增重试计数并判断是否达到最大重试次数。
		nextRetryCount := msg.RetryCount + 1
		now := time.Now()
		status := model.OutboxStatusPending
		var nextRetryAt *time.Time
		if nextRetryCount >= msg.MaxRetry {
			// 3.1 达到上限：转 dead，停止后续扫描重试。
			status = model.OutboxStatusDead
			nextRetryAt = nil
		} else {
			// 3.2 未到上限：按指数退避计算下一次可重试时间。
			t := now.Add(calcRetryBackoff(nextRetryCount))
			nextRetryAt = &t
		}

		// 4. 写回失败原因与状态快照，便于排查问题。
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

// PersistChatHistoryAndMarkConsumed 负责“消费成功后落业务库 + 标记 outbox consumed”。
// 之所以必须放在同一个事务里，是为了保证“业务落库”和“状态推进”原子一致：
// - 若业务写入失败，不应把 outbox 标记为 consumed；
// - 若标记 consumed 失败，也应回滚业务写入，避免出现不可追踪的不一致。
func (d *Repository) PersistChatHistoryAndMarkConsumed(ctx context.Context, outboxID int64, payload model.ChatHistoryPersistPayload) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. 先锁定 outbox 记录，确保同一条消息不会被并发消费者重复推进状态。
		var outboxMsg model.AgentOutboxMessage
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", outboxID).First(&outboxMsg).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// 1.1 幂等兜底：记录不存在时视为“无事可做”。
				return nil
			}
			return err
		}
		// 1.2 若已 consumed/dead，说明已被处理过或已终止，直接幂等返回。
		if outboxMsg.Status == model.OutboxStatusConsumed {
			return nil
		}
		if outboxMsg.Status == model.OutboxStatusDead {
			return nil
		}

		// 2. 写入聊天历史业务表（chat_histories）。
		//    这里不包含 token 统计等扩展字段，只负责核心消息落库。
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

		// 3. 同一事务内原子更新会话统计信息：
		//    - message_count + 1
		//    - last_message_at = now
		// 这样可以保证 message_count 与 chat_histories 的真实落库条数一致。
		now := time.Now()
		chatUpdates := map[string]interface{}{
			"message_count":   gorm.Expr("message_count + ?", 1),
			"last_message_at": &now,
		}
		chatResult := tx.Model(&model.AgentChat{}).
			Where("user_id = ? AND chat_id = ?", payload.UserID, payload.ConversationID).
			Updates(chatUpdates)
		if chatResult.Error != nil {
			return chatResult.Error
		}
		if chatResult.RowsAffected == 0 {
			// 会话不存在时回滚，让 outbox 继续重试/告警，而不是吞掉不一致。
			return fmt.Errorf("conversation not found when updating stats: user_id=%d chat_id=%s", payload.UserID, payload.ConversationID)
		}

		// 4. 业务写入成功后，把 outbox 推进到 consumed 最终态。
		//    并清理错误与重试字段，表示该消息生命周期结束。
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

// calcRetryBackoff 计算指数退避时间。
// 规则：1s, 2s, 4s, 8s, 16s, 32s（最多封顶到第 6 档）。
func calcRetryBackoff(retryCount int) time.Duration {
	if retryCount <= 0 {
		return time.Second
	}
	if retryCount > 6 {
		retryCount = 6
	}
	return time.Second * time.Duration(1<<(retryCount-1))
}

// truncateError 限制错误文本最大长度，防止写库失败或日志污染。
func truncateError(reason string) string {
	if len(reason) <= 2000 {
		return reason
	}
	return reason[:2000]
}
