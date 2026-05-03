package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Repository 是 outbox 状态机仓储。
//
// 职责边界：
// 1. 只负责 outbox 状态流转与通用事务编排；
// 2. 不负责聊天、任务、通知等具体业务语义；
// 3. 同一仓储实例只面向一个服务级 outbox 目录，避免把共享表当成终态。
type Repository struct {
	db    *gorm.DB
	route ServiceRoute
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

// WithTx 用外部事务句柄构造同事务仓储实例。
func (d *Repository) WithTx(tx *gorm.DB) *Repository {
	if d == nil {
		return &Repository{db: tx}
	}
	return &Repository{db: tx, route: d.route}
}

// WithRoute 用指定服务目录构造服务级仓储。
//
// 职责边界：
// 1. 只切换 outbox 物理目录，不改变事务句柄；
// 2. 适合多个 service engine 共享同一 DB 连接；
// 3. 保留 route 的 table/topic/group，避免回落到共享 topic。
func (d *Repository) WithRoute(route ServiceRoute) *Repository {
	route = normalizeServiceRoute(route)
	if d == nil {
		return &Repository{route: route}
	}
	return &Repository{db: d.db, route: route}
}

// CreateMessage 把事件写入 outbox。
//
// 职责边界：
// 1. 只接受 eventType、messageKey、payload 和 maxRetry，不再允许业务侧显式传 topic；
// 2. table/topic/group 统一由 eventType -> service -> route 解析，确保服务级路由是唯一入口；
// 3. eventType 未注册时直接返回 error，避免消息静默落到默认表或默认 topic。
func (d *Repository) CreateMessage(ctx context.Context, eventType string, messageKey string, payload any, maxRetry int) (int64, error) {
	if d == nil || d.db == nil {
		return 0, errors.New("outbox repository is nil")
	}

	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return 0, errors.New("eventType is empty")
	}
	messageKey = strings.TrimSpace(messageKey)
	if maxRetry <= 0 {
		maxRetry = 20
	}

	route, err := d.resolvePublishRoute(eventType)
	if err != nil {
		return 0, err
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	msg := model.AgentOutboxMessage{
		EventType:   eventType,
		ServiceName: route.ServiceName,
		Topic:       route.Topic,
		MessageKey:  messageKey,
		Payload:     string(raw),
		Status:      model.OutboxStatusPending,
		RetryCount:  0,
		MaxRetry:    maxRetry,
		NextRetryAt: &now,
	}

	if err = d.db.WithContext(ctx).Table(route.TableName).Create(&msg).Error; err != nil {
		return 0, err
	}
	return msg.ID, nil
}

// GetByID 从当前仓储绑定的 outbox 表读取指定消息。
func (d *Repository) GetByID(ctx context.Context, id int64) (*model.AgentOutboxMessage, error) {
	var msg model.AgentOutboxMessage
	if err := d.scopedDB(ctx).Where("id = ?", id).First(&msg).Error; err != nil {
		return nil, err
	}
	return &msg, nil
}

// ListDueMessages 拉取到期可投递消息。
//
// 说明：
// 1. serviceName 为空时保持当前仓储目录内的扫描语义；
// 2. serviceName 非空时只扫描对应服务的消息；
// 3. 这样既能支持单服务 relay，也能支持后续多服务 relay。
func (d *Repository) ListDueMessages(ctx context.Context, serviceName string, limit int) ([]model.AgentOutboxMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now()
	var messages []model.AgentOutboxMessage
	query := d.scopedDB(ctx).
		Where("status = ? AND next_retry_at IS NOT NULL AND next_retry_at <= ?", model.OutboxStatusPending, now).
		Order("next_retry_at ASC, id ASC").
		Limit(limit)
	serviceName = strings.TrimSpace(serviceName)
	if serviceName != "" {
		query = query.Where("service_name = ?", serviceName)
	}
	if err := query.Find(&messages).Error; err != nil {
		return nil, err
	}
	return messages, nil
}

// MarkPublished 标记消息已成功投递到 Kafka。
func (d *Repository) MarkPublished(ctx context.Context, id int64) error {
	now := time.Now()
	updates := map[string]interface{}{
		"status":        model.OutboxStatusPublished,
		"published_at":  &now,
		"last_error":    nil,
		"next_retry_at": nil,
	}
	result := d.scopedDB(ctx).
		Model(&model.AgentOutboxMessage{}).
		Where("id = ? AND status NOT IN (?, ?)", id, model.OutboxStatusConsumed, model.OutboxStatusDead).
		Updates(updates)
	return result.Error
}

// MarkDead 把消息标记为死信。
func (d *Repository) MarkDead(ctx context.Context, id int64, reason string) error {
	now := time.Now()
	lastErr := truncateError(reason)
	updates := map[string]interface{}{
		"status":        model.OutboxStatusDead,
		"last_error":    &lastErr,
		"next_retry_at": nil,
		"updated_at":    now,
	}
	return d.scopedDB(ctx).Model(&model.AgentOutboxMessage{}).Where("id = ?", id).Updates(updates).Error
}

// MarkFailedForRetry 记录一次可重试失败并推进重试窗口。
//
// 步骤：
// 1. 行级锁读取当前消息状态；
// 2. 已进入 consumed/dead 时幂等短路；
// 3. retry_count+1，并根据最大次数决定继续 pending 还是转 dead；
// 4. 写回 last_error 和 next_retry_at，交给下一轮扫描继续投递。
func (d *Repository) MarkFailedForRetry(ctx context.Context, id int64, reason string) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var msg model.AgentOutboxMessage
		err := tx.Table(d.tableName()).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&msg).Error
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
		return tx.Table(d.tableName()).Model(&model.AgentOutboxMessage{}).Where("id = ?", id).Updates(updates).Error
	})
}

// ConsumeAndMarkConsumed 是通用“消费成功事务入口”。
//
// 步骤：
// 1. 事务内锁定 outbox 记录；
// 2. consumed/dead 状态幂等返回；
// 3. 执行业务回调 fn(tx)，让业务落库和 outbox 状态共用同一事务；
// 4. 业务成功后统一标记 consumed。
func (d *Repository) ConsumeAndMarkConsumed(ctx context.Context, outboxID int64, fn func(tx *gorm.DB) error) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var outboxMsg model.AgentOutboxMessage
		err := tx.Table(d.tableName()).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", outboxID).First(&outboxMsg).Error
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
		return tx.Table(d.tableName()).Model(&model.AgentOutboxMessage{}).Where("id = ?", outboxID).Updates(updates).Error
	})
}

func (d *Repository) scopedDB(ctx context.Context) *gorm.DB {
	return d.db.WithContext(ctx).Table(d.tableName())
}

func (d *Repository) tableName() string {
	if d == nil {
		return DefaultServiceRoute(ServiceNameAgent).TableName
	}

	route := normalizeServiceRoute(d.route)
	if route.TableName != "" {
		return route.TableName
	}
	return DefaultServiceRoute(ServiceNameAgent).TableName
}

func (d *Repository) resolvePublishRoute(eventType string) (ServiceRoute, error) {
	if d == nil {
		return ServiceRoute{}, errors.New("outbox repository is nil")
	}

	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return ServiceRoute{}, errors.New("eventType is empty")
	}

	route, ok := ResolveEventRoute(eventType)
	if !ok {
		return ServiceRoute{}, fmt.Errorf("outbox route not registered: eventType=%s", eventType)
	}
	if d.route.ServiceName != "" && route.ServiceName != d.route.ServiceName {
		return ServiceRoute{}, fmt.Errorf("eventType %s belongs to service %s, current repo service %s", eventType, route.ServiceName, d.route.ServiceName)
	}
	return normalizeServiceRoute(route), nil
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
