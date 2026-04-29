package outbox

import (
	"context"
	"errors"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
)

// EventPublisher 是通用事件发布能力接口。
//
// 职责边界：
// 1. 只暴露“发布事件”这一件事，隐藏底层 outbox/kafka 实现细节；
// 2. 业务层只依赖该接口，避免直接耦合具体引擎结构体；
// 3. 该接口不承诺“立即消费成功”，只承诺“事件已入队或返回错误”。
type EventPublisher interface {
	Publish(ctx context.Context, req PublishRequest) error
}

// EventBus 是 outbox 异步总线的门面对象。
//
// 设计目的：
// 1. 对外提供“发布 + 注册处理器 + 启停”三类最小能力；
// 2. 对内复用 Engine，不重复实现状态机和调度逻辑；
// 3. 为后续引入更多事件类型提供统一扩展点。
type EventBus struct {
	engine *Engine
}

// NewEventBus 创建通用事件总线。
//
// 说明：
// 1. 当 kafka.enabled=false 时返回 nil，调用方可直接降级为同步模式；
// 2. 该方法只创建基础设施对象，不自动注册任何业务事件处理器；
// 3. 业务事件处理器注册应由上层在启动阶段显式完成，避免隐式副作用。
func NewEventBus(repo *Repository, cfg kafkabus.Config) (*EventBus, error) {
	engine, err := NewEngine(repo, cfg)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		return nil, nil
	}
	return &EventBus{engine: engine}, nil
}

// RegisterEventHandler 注册事件处理器。
//
// 失败语义：
// 1. bus 未初始化时直接返回错误；
// 2. event_type 为空或 handler 为空时返回错误；
// 3. 重复注册时采用“后者覆盖前者”并打日志（由 Engine 负责）。
func (b *EventBus) RegisterEventHandler(eventType string, handler MessageHandler) error {
	if b == nil || b.engine == nil {
		return errors.New("event bus is not initialized")
	}
	return b.engine.RegisterEventHandler(eventType, handler)
}

// Publish 发布事件到 outbox 队列。
//
// 关键语义：
// 1. 返回 nil 仅表示“已写入 outbox 成功”；
// 2. 真正 Kafka 投递与业务消费由后台异步循环完成；
// 3. 若返回 error，表示本次入队失败，调用方应按业务策略决定是否重试/降级。
func (b *EventBus) Publish(ctx context.Context, req PublishRequest) error {
	if b == nil || b.engine == nil {
		return errors.New("event bus is not initialized")
	}
	return b.engine.Publish(ctx, req)
}

// Start 启动事件总线后台循环（dispatch + consume）。
func (b *EventBus) Start(ctx context.Context) {
	if b == nil || b.engine == nil {
		return
	}
	b.engine.Start(ctx)
}

// StartDispatch 单独启动事件总线的 outbox 投递循环。
//
// 职责边界：
// 1. 只暴露 relay/dispatch 运行职责，便于独立进程只负责投递；
// 2. 不启动消费循环，避免与独立 consumer 进程争抢职责；
// 3. 不改变 Start(ctx) 的既有组合启动行为。
func (b *EventBus) StartDispatch(ctx context.Context) {
	if b == nil || b.engine == nil {
		return
	}
	b.engine.StartDispatch(ctx)
}

// StartConsume 单独启动事件总线的 Kafka 消费循环。
//
// 职责边界：
// 1. 只暴露 consumer 运行职责，便于独立进程只负责消费；
// 2. 不扫描 outbox、不投递 Kafka，状态推进仍复用 Engine 既有逻辑；
// 3. handler 注册仍由调用方在启动前显式完成。
func (b *EventBus) StartConsume(ctx context.Context) {
	if b == nil || b.engine == nil {
		return
	}
	b.engine.StartConsume(ctx)
}

// Close 关闭事件总线资源（producer/consumer）。
func (b *EventBus) Close() {
	if b == nil || b.engine == nil {
		return
	}
	b.engine.Close()
}
