package service

import (
	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/service/agentsvc"
)

// AgentService 是 service 层对 agentsvc.AgentService 的兼容别名。
// 迁移目的：
// 1) 把 Agent 业务实现收拢到 service/agentsvc，提升目录整洁度；
// 2) 不破坏既有调用方（api/cmd 仍然可以引用 service.AgentService）。
type AgentService = agentsvc.AgentService

// NewAgentService 是迁移期兼容构造函数。
//
// 说明：
// 1) 外部调用签名不变，新增排程依赖通过可选方式注入（见 NewAgentServiceWithSchedule）；
// 2) 真实构造逻辑已下沉到 service/agentsvc 包。
func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, taskRepo *dao.TaskDAO, cacheDAO *dao.CacheDAO, agentRedis *dao.AgentCache, eventPublisher outboxinfra.EventPublisher) *AgentService {
	return agentsvc.NewAgentService(aiHub, repo, taskRepo, cacheDAO, agentRedis, eventPublisher)
}

// NewAgentServiceWithSchedule 在基础 AgentService 上注入排程依赖。
//
// 设计目的：
// 1) 通过函数注入避免 agentsvc 包直接依赖 service 层的 ScheduleService；
// 2) 排程依赖为可选：未注入时排程路由自动回退到普通聊天；
// 3) 保持 NewAgentService 签名不变，向下兼容。
func NewAgentServiceWithSchedule(
	aiHub *inits.AIHub,
	repo *dao.AgentDAO,
	taskRepo *dao.TaskDAO,
	cacheDAO *dao.CacheDAO,
	agentRedis *dao.AgentCache,
	eventPublisher outboxinfra.EventPublisher,
	scheduleSvc *ScheduleService,
) *AgentService {
	svc := agentsvc.NewAgentService(aiHub, repo, taskRepo, cacheDAO, agentRedis, eventPublisher)

	// 注入排程依赖：将 service 层方法包装为函数闭包，避免循环依赖。
	if scheduleSvc != nil {
		svc.SmartPlanningMultiRawFunc = scheduleSvc.SmartPlanningMultiRaw
		svc.HybridScheduleWithPlanMultiFunc = scheduleSvc.HybridScheduleWithPlanMulti
		svc.ResolvePlanningWindowFunc = scheduleSvc.ResolvePlanningWindowByTaskClasses
	}

	return svc
}
