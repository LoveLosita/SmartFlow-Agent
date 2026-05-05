package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// RepositoryPublisher 只负责把事件写入服务级 outbox 表。
//
// 职责边界：
// 1. 负责复用 Repository 的 eventType -> service -> table 路由能力写入 outbox；
// 2. 不启动 Kafka relay / consumer，也不注册任何 handler；
// 3. 适合独立 RPC 服务进程只发布事件、统一由 worker 进程消费的迁移期场景。
type RepositoryPublisher struct {
	repo     *Repository
	maxRetry int
}

// NewRepositoryPublisher 基于 outbox 仓储创建轻量发布器。
func NewRepositoryPublisher(repo *Repository, maxRetry int) *RepositoryPublisher {
	return &RepositoryPublisher{
		repo:     repo,
		maxRetry: maxRetry,
	}
}

// Publish 写入统一事件外壳，保持与 Engine.Publish 相同的 outbox payload 格式。
//
// 步骤说明：
// 1. 先校验事件类型和业务 payload，明显坏入参直接返回错误，避免写入不可消费消息；
// 2. 再把业务 payload 序列化成 RawMessage，并包进统一事件外壳，保证 worker 解析口径一致；
// 3. 最后交给 Repository 按事件路由落表；路由缺失时返回错误，由业务侧决定是否降级。
func (p *RepositoryPublisher) Publish(ctx context.Context, req PublishRequest) error {
	if p == nil || p.repo == nil {
		return errors.New("outbox repository publisher is nil")
	}

	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return errors.New("eventType is empty")
	}
	if req.Payload == nil {
		return errors.New("payload is nil")
	}

	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}

	eventVersion := strings.TrimSpace(req.EventVersion)
	if eventVersion == "" {
		eventVersion = DefaultEventVersion
	}

	eventID := strings.TrimSpace(req.EventID)
	messageKey := strings.TrimSpace(req.MessageKey)
	if messageKey == "" {
		messageKey = eventID
	}
	if messageKey == "" {
		messageKey = eventType
	}

	aggregateID := strings.TrimSpace(req.AggregateID)
	if aggregateID == "" {
		aggregateID = messageKey
	}

	_, err = p.repo.CreateMessage(ctx, eventType, messageKey, OutboxEventPayload{
		EventID:      eventID,
		EventType:    eventType,
		EventVersion: eventVersion,
		AggregateID:  aggregateID,
		Payload:      payloadJSON,
	}, p.maxRetry)
	return err
}

// PublishWithTx 使用外部事务写入 outbox 消息。
//
// 职责边界：
// 1. 只把底层 Repository 切到调用方传入的事务句柄，事件外壳和路由逻辑仍复用 Publish；
// 2. 不提交或回滚事务，事务生命周期由业务用例控制；
// 3. 适合“业务表更新 + outbox 入队”必须原子提交的场景。
func (p *RepositoryPublisher) PublishWithTx(ctx context.Context, tx *gorm.DB, req PublishRequest) error {
	if p == nil || p.repo == nil {
		return errors.New("outbox repository publisher 未初始化")
	}
	if tx == nil {
		return errors.New("gorm 事务句柄为空")
	}

	txPublisher := &RepositoryPublisher{
		repo:     p.repo.WithTx(tx),
		maxRetry: p.maxRetry,
	}
	return txPublisher.Publish(ctx, req)
}
