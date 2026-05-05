package agentprompt

import (
	"fmt"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
)

// buildDeliverConversationMessage 生成 deliver 节点看到的轻量历史提示。
//
// 职责边界：
// 1. 这里不再承载完整历史，也不再把旧轮次对话重新灌回 deliver；
// 2. 真正可供收口的本轮 execute 窗口放到 msg2，由工作区统一呈现；
// 3. 这里只给模型一个明确提示：历史已经折叠，请不要主动回顾旧轮次。
func buildDeliverConversationMessage(ctx *agentmodel.ConversationContext) string {
	return "历史视图：已折叠到交付工作区的本轮 execute 窗口，请仅依据 msg2 收口，不要回顾旧轮次。"
}

// buildDeliverRoughBuildPrefix 构造 deliver 在“粗排已完成”场景下的专属前缀。
//
// 职责边界：
// 1. 这里只负责把粗排相关的任务类信息补进 msg3 前缀，不改写交付总结本身；
// 2. 只有在上下文里明确存在 rough_build_done 时才注入，避免普通交付场景被额外信息污染；
// 3. 这段前缀用于补齐第一次粗排没有正式计划时的任务类详情，优先让 deliver 看到 task_class_ids 和任务类约束。
func buildDeliverRoughBuildPrefix(ctx *agentmodel.ConversationContext, state *agentmodel.CommonState) string {
	if !hasExecuteRoughBuildDone(ctx) {
		return ""
	}

	lines := []string{
		"粗排补充信息：",
		"- 本轮已经完成粗排，相关任务类已进入 suggested/existing，不要把它们说成正式计划。",
	}

	if taskClassIDs := renderPlanTaskClassIDs(state); taskClassIDs != "" {
		lines = append(lines, "- "+taskClassIDs)
	}
	if taskClassMeta := renderPlanTaskClassMeta(state); taskClassMeta != "" {
		lines = append(lines, "任务类详情：")
		lines = append(lines, taskClassMeta)
	}

	if state == nil || !state.HasPlan() {
		lines = append(lines, "- 当前没有正式计划，请把这批任务类的粗排结果作为总结重点。")
	}

	return strings.Join(lines, "\n")
}

// buildDeliverWorkspace 渲染 deliver 节点自己的结果态工作区。
//
// 设计说明：
// 1. 先保留 deliver 原本依赖的结果态信息：terminal outcome、计划进度、步骤简表；
// 2. 再把基于 execute_loop_closed 切出来的“本轮 execute 窗口”拼到 msg2，作为唯一的本轮事实视图；
// 3. 没有正式计划时也保留 execute 窗口，保证 deliver 仍能基于当前轮活跃上下文诚实收口。
func buildDeliverWorkspace(state *agentmodel.CommonState, ctx *agentmodel.ConversationContext) string {
	lines := []string{"交付工作区："}
	if state == nil {
		lines = append(lines, "- 当前缺少流程状态，请仅基于可见结果态与本轮 execute 窗口诚实收口。")
		lines = append(lines, "", buildDeliverExecuteWindow(ctx))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, renderDeliverTerminalSummary(state))
	if !state.HasPlan() {
		lines = append(lines, "- 当前没有正式计划，请只概括本次互动。")
		lines = append(lines, "", buildDeliverExecuteWindow(ctx))
		return strings.Join(lines, "\n")
	}

	total := len(state.PlanSteps)
	completed := countCompletedPlanSteps(state)
	lines = append(lines, fmt.Sprintf("- 计划进度：已完成 %d/%d 步。", completed, total))
	lines = append(lines, "计划步骤：")
	lines = append(lines, renderDeliverStepOutline(state, completed))
	lines = append(lines, "", buildDeliverExecuteWindow(ctx))

	return strings.Join(lines, "\n")
}

// renderDeliverTerminalSummary 返回 deliver 节点需要知道的收口状态。
func renderDeliverTerminalSummary(state *agentmodel.CommonState) string {
	if state == nil || !state.HasTerminalOutcome() || state.TerminalOutcome == nil {
		return "- 当前没有正式终止结果，请按最近对话和计划进度自然总结。"
	}

	outcome := state.TerminalOutcome
	line := fmt.Sprintf("- 收口状态：%s", outcome.Status)
	if stage := strings.TrimSpace(outcome.Stage); stage != "" {
		line += fmt.Sprintf("；阶段：%s", stage)
	}
	if msg := strings.TrimSpace(outcome.UserMessage); msg != "" {
		line += fmt.Sprintf("；用户提示：%s", msg)
	}
	return line
}

// renderDeliverStepOutline 生成 deliver 节点使用的步骤简表。
func renderDeliverStepOutline(state *agentmodel.CommonState, completed int) string {
	if state == nil || len(state.PlanSteps) == 0 {
		return "- 暂无。"
	}

	lines := make([]string, 0, len(state.PlanSteps))
	for i, step := range state.PlanSteps {
		status := "未完成"
		if i < completed {
			status = "已完成"
		}

		content := strings.TrimSpace(step.Content)
		if content == "" {
			content = "（步骤正文为空）"
		}
		line := fmt.Sprintf("%d. [%s] %s", i+1, status, content)
		if doneWhen := strings.TrimSpace(step.DoneWhen); doneWhen != "" {
			line += fmt.Sprintf(" | 完成判定：%s", doneWhen)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// countCompletedPlanSteps 统计当前已经完成的计划步骤数。
func countCompletedPlanSteps(state *agentmodel.CommonState) int {
	if state == nil {
		return 0
	}

	total := len(state.PlanSteps)
	if total == 0 {
		return 0
	}
	if state.CurrentStep <= 0 {
		if state.IsCompleted() {
			return total
		}
		return 0
	}
	if state.CurrentStep >= total {
		return total
	}
	return state.CurrentStep
}
