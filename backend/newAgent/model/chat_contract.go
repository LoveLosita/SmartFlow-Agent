package model

import (
	"fmt"
	"strings"
)

// ChatRoute 表示 Chat 节点路由决策的目标路径。
type ChatRoute string

const (
	// ChatRouteDirectReply 简单任务：Chat 节点直接输出回复，不再调用下游节点。
	ChatRouteDirectReply ChatRoute = "direct_reply"

	// ChatRouteExecute 中等任务：需要用工具处理，直接进 Execute ReAct 循环。
	ChatRouteExecute ChatRoute = "execute"

	// ChatRouteDeepAnswer 复杂问答：需要深度思考但不需工具，Chat 节点原地开 thinking 回答。
	ChatRouteDeepAnswer ChatRoute = "deep_answer"

	// ChatRoutePlan 复杂规划：需要先制定计划，进 Plan 节点。
	ChatRoutePlan ChatRoute = "plan"
)

// ChatRoutingDecision 是 Chat 节点单次路由决策的结构化输出。
//
// 职责边界：
// 1. Route 决定后续处理路径；
// 2. NeedsRoughBuild 仅在 route=execute 且满足粗排条件时为 true；
// 3. NeedsRefineAfterRoughBuild 仅在 needs_rough_build=true 时有效；
// 4. AllowReorder 表示是否允许打乱 suggested 任务顺序，仅用户明确授权时应为 true；
// 5. Thinking 表示下游 Execute 节点是否应开启深度思考；
// 6. Raw 保留控制码原文，供日志排查；
// 7. 用户可见内容（speak）由流式输出自然产出，不由本结构承载。
type ChatRoutingDecision struct {
	Route                      ChatRoute
	NeedsRoughBuild            bool
	NeedsRefineAfterRoughBuild bool
	AllowReorder               bool
	Thinking                   bool
	Raw                        string
}

// Normalize 统一清洗路由决策中的字符串字段。
func (d *ChatRoutingDecision) Normalize() {
	if d == nil {
		return
	}
	d.Route = ChatRoute(strings.TrimSpace(string(d.Route)))
	d.Raw = strings.TrimSpace(d.Raw)
}

// Validate 校验路由决策的最小合法性。
func (d *ChatRoutingDecision) Validate() error {
	if d == nil {
		return fmt.Errorf("chat routing decision 不能为空")
	}

	d.Normalize()

	switch d.Route {
	case ChatRouteDirectReply, ChatRouteExecute, ChatRouteDeepAnswer, ChatRoutePlan:
		// ok
	case "":
		return fmt.Errorf("chat routing decision.route 不能为空")
	default:
		return fmt.Errorf("未知 route: %s", d.Route)
	}

	// 非 execute 路由不应携带粗排和粗排后微调标记，统一归一化为 false。
	if d.Route != ChatRouteExecute {
		d.NeedsRoughBuild = false
		d.NeedsRefineAfterRoughBuild = false
		d.AllowReorder = false
		d.Thinking = false
	}
	// 只有 needs_rough_build=true 时，needs_refine_after_rough_build 才有语义。
	if !d.NeedsRoughBuild {
		d.NeedsRefineAfterRoughBuild = false
	}

	return nil
}
