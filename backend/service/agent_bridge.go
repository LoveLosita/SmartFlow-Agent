package service

import (
	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/service/agentsvc"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

// AgentService 是 service 层对 agentsvc.AgentService 的兼容别名。
// 迁移目的：
// 1) 把 Agent 业务实现收拢到 service/agentsvc，提升目录整洁度；
// 2) 不破坏既有调用方（api/cmd 仍然可以引用 service.AgentService）。
type AgentService = agentsvc.AgentService

// NewAgentService 是迁移期兼容构造函数。
//
// 说明：
// 1) 继续保留 service 层入口形式，避免 api/cmd 侧直接感知 agentsvc 包路径；
// 2) 主动调度 session DAO 也在这里显式透传，避免聊天入口再去回查全局单例；
// 3) 真实构造逻辑已下沉到 service/agentsvc 包。
func NewAgentService(
	llmService *llmservice.Service,
	repo *dao.AgentDAO,
	taskRepo *dao.TaskDAO,
	cacheDAO *dao.CacheDAO,
	agentRedis *dao.AgentCache,
	activeScheduleDAO *dao.ActiveScheduleDAO,
	activeSessionDAO *dao.ActiveScheduleSessionDAO,
	eventPublisher outboxinfra.EventPublisher,
) *AgentService {
	return agentsvc.NewAgentService(llmService, repo, taskRepo, cacheDAO, agentRedis, activeScheduleDAO, activeSessionDAO, eventPublisher)
}

// NewAgentServiceWithSchedule 在基础 AgentService 上注入排程依赖。
//
// 设计目的：
// 1) 通过函数注入避免 agentsvc 包直接依赖 service 层的 ScheduleService；
// 2) 排程依赖为可选：未注入时排程路由自动回退到普通聊天；
// 3) 主动调度 session DAO 仍沿用统一构造注入，避免排程分支自己拼装仓储。
func NewAgentServiceWithSchedule(
	llmService *llmservice.Service,
	repo *dao.AgentDAO,
	taskRepo *dao.TaskDAO,
	cacheDAO *dao.CacheDAO,
	agentRedis *dao.AgentCache,
	activeScheduleDAO *dao.ActiveScheduleDAO,
	activeSessionDAO *dao.ActiveScheduleSessionDAO,
	eventPublisher outboxinfra.EventPublisher,
	scheduleSvc *ScheduleService,
	taskSvc *TaskService,
) *AgentService {
	svc := agentsvc.NewAgentService(llmService, repo, taskRepo, cacheDAO, agentRedis, activeScheduleDAO, activeSessionDAO, eventPublisher)

	// 注入排程依赖：将 service 层方法包装为函数闭包，避免循环依赖。
	if scheduleSvc != nil {
		svc.SmartPlanningMultiRawFunc = scheduleSvc.SmartPlanningMultiRaw
		svc.HybridScheduleWithPlanMultiFunc = scheduleSvc.HybridScheduleWithPlanMulti
		svc.ResolvePlanningWindowFunc = scheduleSvc.ResolvePlanningWindowByTaskClasses
	}

	// 注入任务紧急性提升依赖：复用 TaskService 的统一提升 + outbox 投递链路。
	if taskSvc != nil {
		svc.GetTasksWithUrgencyPromotionFunc = taskSvc.GetTasksWithUrgencyPromotion
	}

	return svc
}
