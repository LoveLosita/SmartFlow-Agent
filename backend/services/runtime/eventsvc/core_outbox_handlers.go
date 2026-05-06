package eventsvc

import (
	"errors"

	"github.com/LoveLosita/smartflow/backend/services/memory"
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
)

// RegisterCoreOutboxHandlers 注册单体残留内仍由 agent 边界消费的 outbox handler。
//
// 职责边界：
// 1. 只负责聚合注册当前单体残留内仍归 agent 进程消费的 handler；
// 2. 不负责创建 eventBus/outboxRepo/DAO，也不负责启动或关闭事件总线。
// 3. 不改变单个 Register* 函数的职责；具体 payload 解析、幂等消费和业务落库仍由各自 handler 负责。
// 4. memory.extract.requested 已在阶段 6 CP1 迁往 cmd/memory，这里只登记其路由，不再注册消费 handler。
func RegisterCoreOutboxHandlers(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
) error {
	if err := validateCoreOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo); err != nil {
		return err
	}
	if err := RegisterMemoryExtractRoute(); err != nil {
		return err
	}

	return registerOutboxHandlerRoutes(coreOutboxHandlerRoutes(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule))
}

// RegisterAllOutboxHandlers 注册当前阶段所有 outbox handler。
//
// 职责边界：
// 1. 只负责把当前单体残留域的 core / active_scheduler 路由一次性接线；
// 2. 不负责创建依赖，也不负责启动事件总线；
// 3. notification 已独立到 cmd/notification，自有 outbox consumer 不再由单体注册；
// 4. 供当前启动流程在“总线启动前”统一完成显式路由注册。
func RegisterAllOutboxHandlers(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
	activeTriggerWorkflow ActiveScheduleTriggeredProcessor,
) error {
	if err := validateAllOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule, activeTriggerWorkflow); err != nil {
		return err
	}

	return registerOutboxHandlerRoutes(allOutboxHandlerRoutes(
		eventBus,
		outboxRepo,
		repoManager,
		agentRepo,
		cacheRepo,
		memoryModule,
		activeTriggerWorkflow,
	))
}

// validateCoreOutboxHandlerDeps 校验核心 outbox handler 聚合注册所需依赖。
//
// 职责边界：
// 1. 只做 nil 校验，不做数据库、Redis、Kafka 连通性探测，避免注册函数承担启动健康检查职责。
// 2. 返回 error 表示依赖缺失；返回 nil 表示可以安全进入逐项注册流程。
func validateCoreOutboxHandlerDeps(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
) error {
	if eventBus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if repoManager == nil {
		return errors.New("repo manager is nil")
	}
	if agentRepo == nil {
		return errors.New("agent repo is nil")
	}
	if cacheRepo == nil {
		return errors.New("cache repo is nil")
	}
	return nil
}

// validateAllOutboxHandlerDeps 在核心依赖基础上，额外校验 active_scheduler 相关依赖。
func validateAllOutboxHandlerDeps(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
	activeTriggerWorkflow ActiveScheduleTriggeredProcessor,
) error {
	if err := validateCoreOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo); err != nil {
		return err
	}
	if activeTriggerWorkflow == nil {
		return errors.New("active schedule triggered processor is nil")
	}
	return nil
}

// coreOutboxHandlerRoutes 只描述 core 阶段的 outbox 路由。
func coreOutboxHandlerRoutes(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
) []outboxHandlerRoute {
	return []outboxHandlerRoute{
		{
			EventType: EventTypeChatHistoryPersistRequested,
			Service:   outboxHandlerServiceAgent,
			Register: func() error {
				return RegisterChatHistoryPersistHandler(eventBus, outboxRepo, repoManager)
			},
		},
		{
			EventType: EventTypeAgentStateSnapshotPersist,
			Service:   outboxHandlerServiceAgent,
			Register: func() error {
				return RegisterAgentStateSnapshotHandler(eventBus, outboxRepo, repoManager)
			},
		},
		{
			EventType: EventTypeAgentTimelinePersistRequested,
			Service:   outboxHandlerServiceAgent,
			Register: func() error {
				return RegisterAgentTimelinePersistHandler(eventBus, outboxRepo, agentRepo, cacheRepo)
			},
		},
	}
}

// allOutboxHandlerRoutes 把当前阶段所有 outbox 路由一次性展开，供启动入口统一接线。
func allOutboxHandlerRoutes(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
	activeTriggerWorkflow ActiveScheduleTriggeredProcessor,
) []outboxHandlerRoute {
	routes := coreOutboxHandlerRoutes(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule)
	routes = append(routes,
		outboxHandlerRoute{
			EventType: sharedevents.ActiveScheduleTriggeredEventType,
			Service:   outboxHandlerServiceActiveScheduler,
			Register: func() error {
				return RegisterActiveScheduleTriggeredHandler(eventBus, outboxRepo, activeTriggerWorkflow)
			},
		},
	)
	return routes
}
