package eventsvc

import (
	"fmt"
	"strings"

	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
)

// outboxHandlerService 表示 outbox 路由归属的业务服务。
//
// 这里只记录服务归属，不承载具体实现包名，方便在启动日志和路由表里直接阅读。
type outboxHandlerService string

const (
	outboxHandlerServiceAgent           outboxHandlerService = "agent"
	outboxHandlerServiceTask            outboxHandlerService = "task"
	outboxHandlerServiceMemory          outboxHandlerService = "memory"
	outboxHandlerServiceActiveScheduler outboxHandlerService = "active-scheduler"
	outboxHandlerServiceNotification    outboxHandlerService = "notification"
)

// outboxHandlerRoute 显式描述“事件类型 -> 服务归属 -> handler 注册动作”。
//
// 1. EventType 负责唯一定位 outbox 路由键；
// 2. Service 负责标明该路由归属的业务服务；
// 3. Register 只负责把对应 handler 挂到总线，不承载业务逻辑。
type outboxHandlerRoute struct {
	EventType string
	Service   outboxHandlerService
	Register  func() error
}

// registerOutboxHandlerRoutes 逐条注册路由表里的 handler。
//
// 1. 先把事件类型和服务归属写进路由表，避免启动入口散落多处 if err != nil；
// 2. 再统一执行注册动作，保证失败时能直接定位到具体 event_type 和 service；
// 3. 若某条路由缺少注册函数，直接返回 error，避免静默漏注册。
func registerOutboxHandlerRoutes(routes []outboxHandlerRoute) error {
	for _, route := range routes {
		if route.Register == nil {
			return fmt.Errorf("outbox handler route 缺少注册函数: event_type=%s service=%s", route.EventType, route.Service)
		}
		if err := outboxinfra.RegisterEventService(route.EventType, string(route.Service)); err != nil {
			return fmt.Errorf("登记 outbox 事件归属失败（event_type=%s, service=%s）: %w", route.EventType, route.Service, err)
		}
		if err := route.Register(); err != nil {
			return fmt.Errorf("注册 outbox handler 失败（event_type=%s, service=%s）: %w", route.EventType, route.Service, err)
		}
	}
	return nil
}

// scopedOutboxRepoForEvent 负责把通用 outbox 仓库收敛到某个事件所属的服务表。//
// 职责边界：
// 1. 只做事件->服务->表的路由，不碰业务写入语义；
// 2. 返回的仓库只适合当前事件的 MarkDead / ConsumeAndMarkConsumed / MarkFailedForRetry；
// 3. 路由缺失时直接返回错误，避免默默写回默认表。
func scopedOutboxRepoForEvent(outboxRepo *outboxinfra.Repository, eventType string) (*outboxinfra.Repository, error) {
	if outboxRepo == nil {
		return nil, fmt.Errorf("outbox repository is nil")
	}

	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return nil, fmt.Errorf("eventType is empty")
	}

	route, ok := outboxinfra.ResolveEventRoute(eventType)
	if !ok {
		return nil, fmt.Errorf("outbox route not registered: eventType=%s", eventType)
	}
	return outboxRepo.WithRoute(route), nil
}
