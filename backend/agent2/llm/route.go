package agentllm

import (
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
)

// RouteDecisionOutput 是一级路由模型的结构化输出契约。
//
// 说明：
// 1. 这里只定义“模型应该吐什么 JSON”；
// 2. 真正的 prompt 归 prompt/ 管；
// 3. 真正的业务分发归 router/ 管。
type RouteDecisionOutput struct {
	Action     string  `json:"action"`
	TrustRoute bool    `json:"trust_route"`
	Detail     string  `json:"detail"`
	Confidence float64 `json:"confidence"`
}

// ToDecision 把模型契约输出映射成 agent2 内部统一路由结果。
func (o *RouteDecisionOutput) ToDecision() *agentmodel.RouteDecision {
	if o == nil {
		return &agentmodel.RouteDecision{Action: agentmodel.ActionChat}
	}

	action := normalizeRouteAction(o.Action)
	return &agentmodel.RouteDecision{
		Action:     action,
		TrustRoute: o.TrustRoute,
		Detail:     strings.TrimSpace(o.Detail),
		Confidence: o.Confidence,
	}
}

func normalizeRouteAction(raw string) agentmodel.AgentAction {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "quick_note", "quick_note_create":
		return agentmodel.ActionQuickNoteCreate
	case "task_query":
		return agentmodel.ActionTaskQuery
	case "schedule_plan", "schedule_plan_create":
		return agentmodel.ActionSchedulePlanCreate
	case "schedule_refine", "schedule_plan_refine":
		return agentmodel.ActionSchedulePlanRefine
	default:
		return agentmodel.ActionChat
	}
}
