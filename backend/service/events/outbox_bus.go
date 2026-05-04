package events

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
)

// OutboxBus 是启动侧和业务侧共享的 outbox 门面。
//
// 职责边界：
// 1. 对上只暴露 Publish / RegisterEventHandler / Start / Close 这四个能力；
// 2. 对内可以按 service 维度路由到多个底层 engine；
// 3. 不承载任何业务处理逻辑，只做事件归属、topic/group 和 engine 选择。
type OutboxBus interface {
	outboxinfra.EventPublisher
	RegisterEventHandler(eventType string, handler outboxinfra.MessageHandler) error
	Start(ctx context.Context)
	Close()
}

type routedOutboxBus struct {
	buses map[string]OutboxBus
}

// NewRoutedOutboxBus 把多个 service 级 outbox bus 组装成一个门面。
//
// 1. 这里不创建底层 engine，只接收上层已经建好的 service bus；
// 2. 事件发布和 handler 注册都按事件归属路由到对应 service；
// 3. 任一 service bus 缺失时直接报错，避免静默回落到共享 topic。
func NewRoutedOutboxBus(buses map[string]OutboxBus) OutboxBus {
	normalized := make(map[string]OutboxBus, len(buses))
	for serviceName, bus := range buses {
		serviceName = strings.TrimSpace(serviceName)
		if serviceName == "" || bus == nil {
			continue
		}
		normalized[serviceName] = bus
	}
	if len(normalized) == 0 {
		return nil
	}
	return &routedOutboxBus{buses: normalized}
}

// NewServiceOutboxBus 基于 service 级 topic / group 创建底层 outbox engine。
//
// 1. topic / group 由 service 名称推导，不再要求调用方显式传入共享 topic；
// 2. kafka 未启用时返回 nil，调用侧可以继续走同步降级路径；
// 3. 这里不注册 handler，注册仍由启动侧统一完成。
func NewServiceOutboxBus(repo *outboxinfra.Repository, baseCfg kafkabus.Config, serviceName string) (OutboxBus, error) {
	if repo == nil {
		return nil, errors.New("outbox repository is nil")
	}

	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return nil, errors.New("serviceName is empty")
	}

	route, _ := outboxinfra.ResolveServiceRoute(serviceName)
	cfg := baseCfg
	cfg.Topic = strings.TrimSpace(route.Topic)
	cfg.GroupID = strings.TrimSpace(route.GroupID)
	cfg.ServiceName = strings.TrimSpace(route.ServiceName)
	if cfg.ServiceName == "" {
		cfg.ServiceName = serviceName
	}

	bus, err := outboxinfra.NewEventBus(repo.WithRoute(route), cfg)
	if err != nil {
		return nil, err
	}
	if bus == nil {
		return nil, nil
	}
	return bus, nil
}

func (b *routedOutboxBus) Publish(ctx context.Context, req outboxinfra.PublishRequest) error {
	serviceBus, err := b.resolveBusByEventType(req.EventType)
	if err != nil {
		return err
	}
	return serviceBus.Publish(ctx, req)
}

func (b *routedOutboxBus) RegisterEventHandler(eventType string, handler outboxinfra.MessageHandler) error {
	if handler == nil {
		return errors.New("handler is nil")
	}
	serviceBus, err := b.resolveBusByEventType(eventType)
	if err != nil {
		return err
	}
	return serviceBus.RegisterEventHandler(eventType, handler)
}

func (b *routedOutboxBus) Start(ctx context.Context) {
	if b == nil {
		return
	}
	for _, serviceName := range orderedOutboxServiceNames(b.buses) {
		b.buses[serviceName].Start(ctx)
	}
}

func (b *routedOutboxBus) Close() {
	if b == nil {
		return
	}
	for _, serviceName := range orderedOutboxServiceNames(b.buses) {
		b.buses[serviceName].Close()
	}
}

func (b *routedOutboxBus) resolveBusByEventType(eventType string) (OutboxBus, error) {
	if b == nil {
		return nil, errors.New("outbox bus is not initialized")
	}

	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return nil, errors.New("eventType is empty")
	}

	serviceName, ok := outboxinfra.ResolveEventService(eventType)
	if !ok {
		return nil, fmt.Errorf("outbox route not registered: eventType=%s", eventType)
	}

	serviceBus, ok := b.buses[strings.TrimSpace(serviceName)]
	if !ok || serviceBus == nil {
		return nil, fmt.Errorf("service outbox bus is missing: service=%s eventType=%s", serviceName, eventType)
	}
	return serviceBus, nil
}

func orderedOutboxServiceNames(buses map[string]OutboxBus) []string {
	ordered := make([]string, 0, len(buses))
	seen := make(map[string]struct{}, len(buses))

	for _, serviceName := range OutboxServiceNames() {
		if _, ok := buses[serviceName]; ok {
			ordered = append(ordered, serviceName)
			seen[serviceName] = struct{}{}
		}
	}

	extras := make([]string, 0)
	for serviceName := range buses {
		if _, ok := seen[serviceName]; ok {
			continue
		}
		extras = append(extras, serviceName)
	}
	sort.Strings(extras)
	return append(ordered, extras...)
}

// OutboxServiceNames 返回当前阶段启用的 service 级 outbox 名称。
func OutboxServiceNames() []string {
	return []string{
		string(outboxHandlerServiceAgent),
		string(outboxHandlerServiceTask),
		string(outboxHandlerServiceMemory),
	}
}

// ResolveOutboxTopicForService 把 service 名称映射成独立 Kafka topic。
//
// 1. 这里保留现在的命名风格：smartflow.<service>.outbox；
// 2. 空 service 只作为兜底，不作为主路径；
// 3. 调用侧不再传共享 topic，避免入口继续依赖旧结构。
func ResolveOutboxTopicForService(serviceName string) string {
	route, _ := outboxinfra.ResolveServiceRoute(serviceName)
	if topic := strings.TrimSpace(route.Topic); topic != "" {
		return topic
	}
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return kafkabus.DefaultTopic
	}
	return "smartflow." + serviceName + ".outbox"
}

// ResolveOutboxGroupForService 把 service 名称映射成独立 Kafka group。
func ResolveOutboxGroupForService(serviceName string) string {
	route, _ := outboxinfra.ResolveServiceRoute(serviceName)
	if groupID := strings.TrimSpace(route.GroupID); groupID != "" {
		return groupID
	}
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return kafkabus.DefaultGroup
	}
	return "smartflow-" + serviceName + "-outbox-consumer"
}

// ResolveOutboxTopicForEvent 根据事件归属 service 计算 topic。
func ResolveOutboxTopicForEvent(eventType string) (string, error) {
	route, ok := outboxinfra.ResolveEventRoute(eventType)
	if !ok {
		return "", fmt.Errorf("outbox route not registered: eventType=%s", strings.TrimSpace(eventType))
	}
	if topic := strings.TrimSpace(route.Topic); topic != "" {
		return topic, nil
	}
	return ResolveOutboxTopicForService(route.ServiceName), nil
}
