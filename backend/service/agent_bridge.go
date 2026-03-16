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
// 说明：
// 1) 外部调用签名保持不变；
// 2) 真实构造逻辑已下沉到 service/agentsvc 包。
func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, taskRepo *dao.TaskDAO, agentRedis *dao.AgentCache, eventPublisher outboxinfra.EventPublisher) *AgentService {
	return agentsvc.NewAgentService(aiHub, repo, taskRepo, agentRedis, eventPublisher)
}
