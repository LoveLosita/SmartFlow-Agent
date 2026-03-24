package agentnode

import (
	agentllm "github.com/LoveLosita/smartflow/backend/agent2/llm"
	agentstream "github.com/LoveLosita/smartflow/backend/agent2/stream"
)

// SchedulePlanNodeDeps 描述“首次排程”节点层公共依赖。
type SchedulePlanNodeDeps struct {
	LLM          *agentllm.Client
	StageEmitter agentstream.StageEmitter
}

// SchedulePlanNodes 是“首次排程”节点逻辑容器。
type SchedulePlanNodes struct {
	deps SchedulePlanNodeDeps
}

// NewSchedulePlanNodes 创建首次排程节点容器。
func NewSchedulePlanNodes(deps SchedulePlanNodeDeps) *SchedulePlanNodes {
	if deps.StageEmitter == nil {
		deps.StageEmitter = agentstream.NoopStageEmitter()
	}
	return &SchedulePlanNodes{deps: deps}
}
