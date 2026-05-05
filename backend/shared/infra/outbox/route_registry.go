package outbox

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

var outboxRouteRegistry = struct {
	sync.RWMutex
	eventToService map[string]string
	serviceRoutes  map[string]ServiceRoute
}{
	eventToService: make(map[string]string),
	serviceRoutes:  make(map[string]ServiceRoute),
}

// RegisterServiceRoute 注册或覆盖某个服务的物理 outbox 路由。
//
// 职责边界：
// 1. 只登记“服务 -> table/topic/group”目录，不登记事件归属；
// 2. 同服务重复注册时以后者覆盖前者，方便显式配置覆盖默认目录；
// 3. 空服务名直接报错，避免把共享 topic 误当成新终态。
func RegisterServiceRoute(route ServiceRoute) error {
	route = normalizeServiceRoute(route)
	if route.ServiceName == "" {
		return errors.New("serviceName is empty")
	}

	outboxRouteRegistry.Lock()
	defer outboxRouteRegistry.Unlock()

	outboxRouteRegistry.serviceRoutes[route.ServiceName] = route
	return nil
}

// RegisterEventService 记录“事件类型 -> 服务归属”的全局路由。
//
// 职责边界：
// 1. 只登记跨进程都要识别的事件归属，不承载 handler 逻辑；
// 2. 同一 event_type 只能归属一个服务，重复登记同值视为幂等；
// 3. 若该服务还没有显式路由，则先写入默认服务目录，保证后续能查到 table/topic/group。
func RegisterEventService(eventType, serviceName string) error {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return errors.New("eventType is empty")
	}
	serviceName = normalizeServiceName(serviceName)
	if serviceName == "" {
		return errors.New("serviceName is empty")
	}

	outboxRouteRegistry.Lock()
	defer outboxRouteRegistry.Unlock()

	if existing, ok := outboxRouteRegistry.eventToService[eventType]; ok {
		if existing != serviceName {
			return fmt.Errorf("eventType %s already registered to service %s", eventType, existing)
		}
		return nil
	}

	outboxRouteRegistry.eventToService[eventType] = serviceName
	return nil
}

// ResolveEventService 查询某个事件类型的归属服务。
//
// 返回值说明：
// 1. serviceName 为登记结果；
// 2. ok=false 表示当前路由表里还没有这个事件类型的归属信息。
func ResolveEventService(eventType string) (serviceName string, ok bool) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return "", false
	}

	outboxRouteRegistry.RLock()
	defer outboxRouteRegistry.RUnlock()

	serviceName, ok = outboxRouteRegistry.eventToService[eventType]
	return serviceName, ok
}

// ResolveServiceRoute 查询某个服务的物理 outbox 配置。
//
// 返回值说明：
// 1. route 始终返回一个可执行的目录结果，未显式注册时回退默认目录；
// 2. ok=true 表示命中显式注册目录，ok=false 表示走默认目录；
// 3. 这样既能支持显式配置覆盖，也能让基础设施在启动初期就有稳定默认值。
func ResolveServiceRoute(serviceName string) (route ServiceRoute, ok bool) {
	serviceName = normalizeServiceName(serviceName)
	if serviceName == "" {
		return DefaultServiceRoute(""), false
	}

	outboxRouteRegistry.RLock()
	route, ok = outboxRouteRegistry.serviceRoutes[serviceName]
	outboxRouteRegistry.RUnlock()
	if ok {
		return normalizeServiceRoute(route), true
	}
	if route, ok = configuredServiceRoute(serviceName); ok {
		return route, true
	}
	return DefaultServiceRoute(serviceName), false
}

// ResolveEventRoute 先按事件查服务，再按服务查物理目录。
//
// 返回值说明：
// 1. route 包含事件所在服务的 table/topic/group；
// 2. ok=true 只表示“事件 -> 服务归属”已登记；
// 3. 服务目录若未显式注册，会自动回退到默认目录。
func ResolveEventRoute(eventType string) (route ServiceRoute, ok bool) {
	serviceName, ok := ResolveEventService(eventType)
	if !ok {
		return ServiceRoute{}, false
	}
	route, _ = ResolveServiceRoute(serviceName)
	return route, true
}

func configuredServiceRoute(serviceName string) (ServiceRoute, bool) {
	cfg, ok := ResolveServiceConfig(serviceName)
	if !ok {
		return ServiceRoute{}, false
	}
	return normalizeServiceRoute(ServiceRoute{
		ServiceName: cfg.Name,
		TableName:   cfg.TableName,
		Topic:       cfg.Topic,
		GroupID:     cfg.GroupID,
	}), true
}
