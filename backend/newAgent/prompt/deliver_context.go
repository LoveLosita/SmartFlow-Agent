package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

// buildDeliverConversationMessage 生成 deliver 节点看到的真实对话视图。
func buildDeliverConversationMessage(ctx *newagentmodel.ConversationContext) string {
	return buildConversationHistoryMessage(ctx, "执行对话记录")
}

// buildDeliverRoughBuildPrefix 构造 deliver 在“粗排已完成”场景下的专属前缀。
//
// 职责边界：
// 1. 这里只负责把粗排相关的任务类信息补进 msg3 前缀，不改写交付总结本身；
// 2. 只有在上下文里明确存在 rough_build_done 时才注入，避免普通交付场景被额外信息污染；
// 3. 这段前缀用于补齐第一次粗排没有正式计划时的任务类详情，优先让 deliver 看到 task_class_ids 和任务类约束。
func buildDeliverRoughBuildPrefix(ctx *newagentmodel.ConversationContext, state *newagentmodel.CommonState) string {
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

// buildDeliverWorkspace 渲染 deliver 节点自己的结果视图。
//
// 设计说明：
// 1. deliver 只需要结果态信息：计划简表、完成进度、收口状态；
// 2. 不再注入工具目录、任务类约束、ReAct 摘要等过程噪声；
// 3. 没有正式计划时，明确退回“只基于对话做总结”。
func buildDeliverWorkspace(state *newagentmodel.CommonState) string {
	lines := []string{"交付工作区："}
	if state == nil {
		lines = append(lines, "- 当前缺少流程状态，请仅基于最近对话做诚实总结。")
		return strings.Join(lines, "\n")
	}

	lines = append(lines, renderDeliverTerminalSummary(state))
	if !state.HasPlan() {
		lines = append(lines, "- 当前没有正式计划，请只概括本次互动。")
		return strings.Join(lines, "\n")
	}

	total := len(state.PlanSteps)
	completed := countCompletedPlanSteps(state)
	lines = append(lines, fmt.Sprintf("- 计划进度：已完成 %d/%d 步。", completed, total))
	lines = append(lines, "计划步骤：")
	lines = append(lines, renderDeliverStepOutline(state, completed))

	return strings.Join(lines, "\n")
}

// renderDeliverTerminalSummary 返回 deliver 节点需要知道的收口状态。
func renderDeliverTerminalSummary(state *newagentmodel.CommonState) string {
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
func renderDeliverStepOutline(state *newagentmodel.CommonState, completed int) string {
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
func countCompletedPlanSteps(state *newagentmodel.CommonState) int {
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
