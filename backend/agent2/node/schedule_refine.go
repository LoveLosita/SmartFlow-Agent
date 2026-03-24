package agentnode

import (
	agentllm "github.com/LoveLosita/smartflow/backend/agent2/llm"
	agentstream "github.com/LoveLosita/smartflow/backend/agent2/stream"
)

// ScheduleRefineNodeDeps 描述“连续微调排程”节点层公共依赖。
type ScheduleRefineNodeDeps struct {
	LLM          *agentllm.Client
	StageEmitter agentstream.StageEmitter
}

// ScheduleRefineNodes 是“连续微调排程”节点逻辑容器。
type ScheduleRefineNodes struct {
	deps ScheduleRefineNodeDeps
}

// NewScheduleRefineNodes 创建连续微调节点容器。
func NewScheduleRefineNodes(deps ScheduleRefineNodeDeps) *ScheduleRefineNodes {
	if deps.StageEmitter == nil {
		deps.StageEmitter = agentstream.NoopStageEmitter()
	}
	return &ScheduleRefineNodes{deps: deps}
}
