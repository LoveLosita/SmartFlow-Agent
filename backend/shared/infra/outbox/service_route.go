package outbox

import (
	"strings"
)

const (
	ServiceNameAgent           = "agent"
	ServiceNameTask            = "task"
	ServiceNameMemory          = "memory"
	ServiceNameActiveScheduler = "active-scheduler"
	ServiceNameNotification    = "notification"
	ServiceNameTaskClassForum  = "taskclass-forum"
	ServiceNameTokenStore      = "token-store"
)

// ServiceRoute 描述一个 outbox 服务的终态路由信息。
//
// 职责边界：
// 1. 只承载服务级 outbox 的 table/topic/group 目录信息；
// 2. 不承载 handler、事务或 Kafka 连接对象；
// 3. 允许上层按事件类型先查服务，再由服务查到自己的物理资源。
type ServiceRoute struct {
	ServiceName string
	TableName   string
	Topic       string
	GroupID     string
}

var builtinServiceRoutes = map[string]ServiceRoute{
	ServiceNameAgent: {
		ServiceName: ServiceNameAgent,
		TableName:   "agent_outbox_messages",
		Topic:       "smartflow.agent.outbox",
		GroupID:     "smartflow-agent-outbox-consumer",
	},
	ServiceNameTask: {
		ServiceName: ServiceNameTask,
		TableName:   "task_outbox_messages",
		Topic:       "smartflow.task.outbox",
		GroupID:     "smartflow-task-outbox-consumer",
	},
	ServiceNameMemory: {
		ServiceName: ServiceNameMemory,
		TableName:   "memory_outbox_messages",
		Topic:       "smartflow.memory.outbox",
		GroupID:     "smartflow-memory-outbox-consumer",
	},
	ServiceNameActiveScheduler: {
		ServiceName: ServiceNameActiveScheduler,
		TableName:   "active_scheduler_outbox_messages",
		Topic:       "smartflow.active-scheduler.outbox",
		GroupID:     "smartflow-active-scheduler-outbox-consumer",
	},
	ServiceNameNotification: {
		ServiceName: ServiceNameNotification,
		TableName:   "notification_outbox_messages",
		Topic:       "smartflow.notification.outbox",
		GroupID:     "smartflow-notification-outbox-consumer",
	},
	ServiceNameTaskClassForum: {
		ServiceName: ServiceNameTaskClassForum,
		TableName:   "taskclass_forum_outbox_messages",
		Topic:       "smartflow.taskclass-forum.outbox",
		GroupID:     "smartflow-taskclass-forum-outbox-consumer",
	},
	ServiceNameTokenStore: {
		ServiceName: ServiceNameTokenStore,
		TableName:   "token_store_outbox_messages",
		Topic:       "smartflow.token-store.outbox",
		GroupID:     "smartflow-token-store-outbox-consumer",
	},
}

// DefaultServiceRoutes 返回当前已知服务的默认路由清单。
//
// 说明：
// 1. 这里是“目录初始值”，用于自动建表和首次注册时兜底；
// 2. 运行时若显式注册了服务路由，会以显式注册结果为准；
// 3. 返回值是拷贝，调用方可安全遍历，不会污染全局目录。
func DefaultServiceRoutes() []ServiceRoute {
	return []ServiceRoute{
		builtinServiceRoutes[ServiceNameAgent],
		builtinServiceRoutes[ServiceNameTask],
		builtinServiceRoutes[ServiceNameMemory],
		builtinServiceRoutes[ServiceNameActiveScheduler],
		builtinServiceRoutes[ServiceNameNotification],
		builtinServiceRoutes[ServiceNameTaskClassForum],
		builtinServiceRoutes[ServiceNameTokenStore],
	}
}

// DefaultServiceRoute 根据服务名生成终态路由。
//
// 规则：
// 1. 已知服务直接返回约定映射；
// 2. 未知服务按命名约定生成 table/topic/group，避免继续落回共享 topic；
// 3. 空服务名回退到 agent 兼容路径，保住历史单体模式。
func DefaultServiceRoute(serviceName string) ServiceRoute {
	serviceName = normalizeServiceName(serviceName)
	if serviceName == "" {
		serviceName = ServiceNameAgent
	}
	if route, ok := builtinServiceRoutes[serviceName]; ok {
		return route
	}

	tablePrefix := strings.NewReplacer("-", "_").Replace(serviceName)
	if tablePrefix == "" {
		tablePrefix = ServiceNameAgent
	}

	return ServiceRoute{
		ServiceName: serviceName,
		TableName:   tablePrefix + "_outbox_messages",
		Topic:       "smartflow." + serviceName + ".outbox",
		GroupID:     "smartflow-" + serviceName + "-outbox-consumer",
	}
}

func normalizeServiceName(serviceName string) string {
	return strings.TrimSpace(serviceName)
}

// normalizeServiceRoute 把空字段补成可执行的默认值。
//
// 说明：
// 1. 只做字符串裁剪和缺省补齐，不做注册副作用；
// 2. 服务名为空时只保留历史兼容路径，不强行把它当成新服务；
// 3. 这一步是 route 目录的最后一道兜底，避免上层拿到半成品路由。
func normalizeServiceRoute(route ServiceRoute) ServiceRoute {
	route.ServiceName = normalizeServiceName(route.ServiceName)
	route.TableName = strings.TrimSpace(route.TableName)
	route.Topic = strings.TrimSpace(route.Topic)
	route.GroupID = strings.TrimSpace(route.GroupID)

	if route.ServiceName == "" {
		if route.TableName == "" {
			route.TableName = builtinServiceRoutes[ServiceNameAgent].TableName
		}
		if route.Topic == "" {
			route.Topic = builtinServiceRoutes[ServiceNameAgent].Topic
		}
		if route.GroupID == "" {
			route.GroupID = builtinServiceRoutes[ServiceNameAgent].GroupID
		}
		return route
	}

	defaultRoute := DefaultServiceRoute(route.ServiceName)
	if route.TableName == "" {
		route.TableName = defaultRoute.TableName
	}
	if route.Topic == "" {
		route.Topic = defaultRoute.Topic
	}
	if route.GroupID == "" {
		route.GroupID = defaultRoute.GroupID
	}
	return route
}
