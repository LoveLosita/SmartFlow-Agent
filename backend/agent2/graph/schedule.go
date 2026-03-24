package agentgraph

import agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"

const (
	SchedulePlanGraphName   = "schedule_plan"
	ScheduleRefineGraphName = "schedule_refine"

	ScheduleNodeIntentRoute = "schedule.intent.route"
	ScheduleNodePlan        = "schedule.plan"
	ScheduleNodeRoughBuild  = "schedule.rough_build"
	ScheduleNodeReact       = "schedule.react"
	ScheduleNodeHardCheck   = "schedule.hard_check"
	ScheduleNodeReply       = "schedule.reply"
)

// SchedulePlanGraph 是“首次排程”图编排骨架。
type SchedulePlanGraph struct {
	Nodes *agentnode.SchedulePlanNodes
}

// NewSchedulePlanGraph 创建首次排程图骨架。
func NewSchedulePlanGraph(nodes *agentnode.SchedulePlanNodes) *SchedulePlanGraph {
	return &SchedulePlanGraph{Nodes: nodes}
}

// ScheduleRefineGraph 是“连续微调排程”图编排骨架。
type ScheduleRefineGraph struct {
	Nodes *agentnode.ScheduleRefineNodes
}

// NewScheduleRefineGraph 创建连续微调图骨架。
func NewScheduleRefineGraph(nodes *agentnode.ScheduleRefineNodes) *ScheduleRefineGraph {
	return &ScheduleRefineGraph{Nodes: nodes}
}
