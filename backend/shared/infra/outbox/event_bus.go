package outbox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
)

// EventPublisher 是通用事件发布能力接口。
type EventPublisher interface {
	Publish(ctx context.Context, req PublishRequest) error
}

// EventBus 是 outbox 多服务引擎的门面。
//
// 职责边界：
// 1. 对外只暴露“发布、注册 handler、启动、关闭”四类能力；
// 2. 内部按事件归属把调用路由到对应 service engine；
// 3. 不再把共享 topic 当主路径，服务级路由始终优先。
type EventBus struct {
	repo *Repository
	cfg  kafkabus.Config

	mu      sync.RWMutex
	engines map[string]*Engine
}

// NewEventBus 创建多服务事件门面。
//
// 说明：
// 1. kafka.enabled=false 时返回 nil，调用方可直接降级；
// 2. 实际 service engine 在需要时按服务目录懒加载；
// 3. 懒加载不会改变既有事件契约，只是把物理资源拆到各自服务。
func NewEventBus(repo *Repository, cfg kafkabus.Config) (*EventBus, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if repo == nil {
		return nil, errors.New("outbox repository is nil")
	}
	return &EventBus{
		repo:    repo,
		cfg:     cfg,
		engines: make(map[string]*Engine),
	}, nil
}

// RegisterEventHandler 注册事件处理器。
func (b *EventBus) RegisterEventHandler(eventType string, handler MessageHandler) error {
	if b == nil {
		return errors.New("event bus is not initialized")
	}

	route, err := b.routeForEvent(eventType)
	if err != nil {
		return err
	}
	engine, err := b.ensureEngine(route)
	if err != nil {
		return err
	}
	return engine.RegisterEventHandler(eventType, handler)
}

// Publish 把事件路由到对应服务的 outbox 表与 Kafka 资源。
func (b *EventBus) Publish(ctx context.Context, req PublishRequest) error {
	if b == nil {
		return errors.New("event bus is not initialized")
	}

	route, err := b.routeForEvent(req.EventType)
	if err != nil {
		return err
	}
	engine, err := b.ensureEngine(route)
	if err != nil {
		return err
	}
	return engine.Publish(ctx, req)
}

// Start 启动所有已创建的 service engine。
func (b *EventBus) Start(ctx context.Context) {
	if b == nil {
		return
	}
	for _, engine := range b.snapshotEngines() {
		go engine.Start(ctx)
	}
}

// StartDispatch 只启动所有已创建 engine 的 dispatch 循环。
func (b *EventBus) StartDispatch(ctx context.Context) {
	if b == nil {
		return
	}
	for _, engine := range b.snapshotEngines() {
		go engine.StartDispatch(ctx)
	}
}

// StartConsume 只启动所有已创建 engine 的消费循环。
func (b *EventBus) StartConsume(ctx context.Context) {
	if b == nil {
		return
	}
	for _, engine := range b.snapshotEngines() {
		go engine.StartConsume(ctx)
	}
}

// Close 关闭所有 service engine 的 Kafka 资源。
func (b *EventBus) Close() {
	if b == nil {
		return
	}
	for _, engine := range b.snapshotEngines() {
		engine.Close()
	}
}

func (b *EventBus) routeForEvent(eventType string) (ServiceRoute, error) {
	route, ok := ResolveEventRoute(eventType)
	if !ok {
		return ServiceRoute{}, fmt.Errorf("outbox route not registered: eventType=%s", strings.TrimSpace(eventType))
	}
	return route, nil
}

func (b *EventBus) ensureEngine(route ServiceRoute) (*Engine, error) {
	serviceName := route.ServiceName
	if serviceName == "" {
		return nil, errors.New("serviceName is empty")
	}

	b.mu.RLock()
	if engine, ok := b.engines[serviceName]; ok {
		b.mu.RUnlock()
		return engine, nil
	}
	b.mu.RUnlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	if engine, ok := b.engines[serviceName]; ok {
		return engine, nil
	}

	cfg := b.cfg
	cfg.ServiceName = serviceName
	cfg.Topic = route.Topic
	cfg.GroupID = route.GroupID

	engine, err := NewEngine(b.repo, cfg)
	if err != nil {
		return nil, err
	}
	if engine == nil {
		return nil, nil
	}
	b.engines[serviceName] = engine
	return engine, nil
}

func (b *EventBus) snapshotEngines() []*Engine {
	b.mu.RLock()
	defer b.mu.RUnlock()
	engines := make([]*Engine, 0, len(b.engines))
	for _, engine := range b.engines {
		engines = append(engines, engine)
	}
	return engines
}
