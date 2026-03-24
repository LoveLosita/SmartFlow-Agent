package agentnode

import (
	agentllm "github.com/LoveLosita/smartflow/backend/agent2/llm"
	agentstream "github.com/LoveLosita/smartflow/backend/agent2/stream"
)

// TaskQueryNodeDeps 描述“随口问任务”节点层的公共依赖。
type TaskQueryNodeDeps struct {
	LLM          *agentllm.Client
	StageEmitter agentstream.StageEmitter
}

// TaskQueryNodes 是“随口问任务”节点逻辑容器。
type TaskQueryNodes struct {
	deps TaskQueryNodeDeps
}

// NewTaskQueryNodes 创建任务查询节点容器。
func NewTaskQueryNodes(deps TaskQueryNodeDeps) *TaskQueryNodes {
	if deps.StageEmitter == nil {
		deps.StageEmitter = agentstream.NoopStageEmitter()
	}
	return &TaskQueryNodes{deps: deps}
}
