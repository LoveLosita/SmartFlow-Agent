package agentgraph

import agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"

const (
	TaskQueryGraphName = "task_query"

	TaskQueryNodePlan    = "task_query.plan"
	TaskQueryNodeTool    = "task_query.tool.query"
	TaskQueryNodeReflect = "task_query.reflect"
	TaskQueryNodeReply   = "task_query.reply"
)

// TaskQueryGraph 是“随口问任务”图编排骨架。
type TaskQueryGraph struct {
	Nodes *agentnode.TaskQueryNodes
}

// NewTaskQueryGraph 创建任务查询图骨架。
func NewTaskQueryGraph(nodes *agentnode.TaskQueryNodes) *TaskQueryGraph {
	return &TaskQueryGraph{Nodes: nodes}
}
