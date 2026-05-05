package events

import (
	"errors"

	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/memory"
	"github.com/LoveLosita/smartflow/backend/notification"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
)

// RegisterCoreOutboxHandlers 注册核心业务 outbox handler。
//
// 职责边界：
// 1. 只负责聚合注册当前核心业务 handler，便于 start / worker/all 等启动入口复用同一套接线顺序。
// 2. 不负责创建 eventBus/outboxRepo/DAO/memoryModule，也不负责启动或关闭事件总线。
// 3. 不改变单个 Register* 函数的职责；具体 payload 解析、幂等消费和业务落库仍由各自 handler 负责。
// 4. 这里以显式 route table 的方式列出“事件类型 -> 服务归属 -> handler”，避免后续新增事件时只改启动入口不改接线表。
func RegisterCoreOutboxHandlers(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
	adjuster ports.TokenUsageAdjuster,
) error {
	if err := validateCoreOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule); err != nil {
		return err
	}

	return registerOutboxHandlerRoutes(coreOutboxHandlerRoutes(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule, adjuster))
}

// RegisterAllOutboxHandlers 注册当前阶段所有 outbox handler。
//
// 职责边界：
// 1. 只负责把 core / active_scheduler / notification 三类路由一次性接线；
// 2. 不负责创建依赖，也不负责启动事件总线；
// 3. 供当前启动流程在“总线启动前”统一完成显式路由注册。
func RegisterAllOutboxHandlers(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
	activeTriggerWorkflow ActiveScheduleTriggeredProcessor,
	notificationService *notification.NotificationService,
	forumRewardRecorder ForumRewardGrantRecorder,
	adjuster ports.TokenUsageAdjuster,
) error {
	if err := validateAllOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule, activeTriggerWorkflow, notificationService, forumRewardRecorder); err != nil {
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
		notificationService,
		forumRewardRecorder,
		adjuster,
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
	memoryModule *memory.Module,
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
	if memoryModule == nil {
		return errors.New("memory module is nil")
	}
	return nil
}

// validateAllOutboxHandlerDeps 在核心依赖基础上，额外校验 active_scheduler 和 notification 相关依赖。
func validateAllOutboxHandlerDeps(
	eventBus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	memoryModule *memory.Module,
	activeTriggerWorkflow ActiveScheduleTriggeredProcessor,
	notificationService *notification.NotificationService,
	forumRewardRecorder ForumRewardGrantRecorder,
) error {
	if err := validateCoreOutboxHandlerDeps(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule); err != nil {
		return err
	}
	if activeTriggerWorkflow == nil {
		return errors.New("active schedule triggered processor is nil")
	}
	if notificationService == nil {
		return errors.New("notification service is nil")
	}
	if forumRewardRecorder == nil {
		return errors.New("forum reward grant recorder is nil")
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
	adjuster ports.TokenUsageAdjuster,
) []outboxHandlerRoute {
	return []outboxHandlerRoute{
		{
			EventType: EventTypeChatHistoryPersistRequested,
			Service:   outboxHandlerServiceAgent,
			Register: func() error {
				return RegisterChatHistoryPersistHandler(eventBus, outboxRepo, repoManager, adjuster)
			},
		},
		{
			EventType: EventTypeTaskUrgencyPromoteRequested,
			Service:   outboxHandlerServiceTask,
			Register: func() error {
				return RegisterTaskUrgencyPromoteHandler(eventBus, outboxRepo, repoManager)
			},
		},
		{
			EventType: EventTypeChatTokenUsageAdjustRequested,
			Service:   outboxHandlerServiceAgent,
			Register: func() error {
				return RegisterChatTokenUsageAdjustHandler(eventBus, outboxRepo, repoManager, adjuster)
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
		{
			EventType: EventTypeMemoryExtractRequested,
			Service:   outboxHandlerServiceMemory,
			Register: func() error {
				return RegisterMemoryExtractRequestedHandler(eventBus, outboxRepo, memoryModule)
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
	notificationService *notification.NotificationService,
	forumRewardRecorder ForumRewardGrantRecorder,
	adjuster ports.TokenUsageAdjuster,
) []outboxHandlerRoute {
	routes := coreOutboxHandlerRoutes(eventBus, outboxRepo, repoManager, agentRepo, cacheRepo, memoryModule, adjuster)
	routes = append(routes,
		outboxHandlerRoute{
			EventType: sharedevents.ForumPostLikedEventType,
			Service:   outboxHandlerServiceTokenStore,
			Register: func() error {
				return RegisterForumPostLikedRewardHandler(eventBus, outboxRepo, forumRewardRecorder)
			},
		},
		outboxHandlerRoute{
			EventType: sharedevents.ForumPostImportedEventType,
			Service:   outboxHandlerServiceTokenStore,
			Register: func() error {
				return RegisterForumPostImportedRewardHandler(eventBus, outboxRepo, forumRewardRecorder)
			},
		},
		outboxHandlerRoute{
			EventType: sharedevents.ActiveScheduleTriggeredEventType,
			Service:   outboxHandlerServiceActiveScheduler,
			Register: func() error {
				return RegisterActiveScheduleTriggeredHandler(eventBus, outboxRepo, activeTriggerWorkflow)
			},
		},
		outboxHandlerRoute{
			EventType: sharedevents.NotificationFeishuRequestedEventType,
			Service:   outboxHandlerServiceNotification,
			Register: func() error {
				return RegisterFeishuNotificationHandler(eventBus, outboxRepo, notificationService)
			},
		},
	)
	return routes
}
