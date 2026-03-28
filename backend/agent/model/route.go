package agentmodel

// AgentAction 表示一级路由动作。
type AgentAction string

const (
	ActionChat               AgentAction = "chat"
	ActionQuickNoteCreate    AgentAction = "quick_note_create"
	ActionTaskQuery          AgentAction = "task_query"
	ActionSchedulePlanCreate AgentAction = "schedule_plan_create"
	ActionSchedulePlanRefine AgentAction = "schedule_plan_refine"
)

// RouteDecision 是统一一级分流结果。
type RouteDecision struct {
	Action     AgentAction
	TrustRoute bool
	Detail     string
	Confidence float64
}
