package newagentprompt

import (
	"fmt"
	"strconv"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

// buildPlanConversationMessage 生成 plan 节点看到的真实对话视图。
func buildPlanConversationMessage(ctx *newagentmodel.ConversationContext) string {
	return buildConversationHistoryMessage(ctx, "规划参考对话")
}

// buildPlanWorkspace 渲染 plan 节点自己的工作区。
//
// 设计说明：
// 1. 这里只保留“规划真正需要知道的东西”：已有计划、当前步骤、task_class_ids、任务类约束；
// 2. 不再复用通用胖状态摘要，避免把 execute / deliver 无关状态一起塞给 plan；
// 3. 若当前没有正式计划，则明确告诉模型“从零开始规划”，避免继续误沿用旧上下文。
func buildPlanWorkspace(state *newagentmodel.CommonState) string {
	lines := []string{"规划工作区："}
	if state == nil {
		lines = append(lines, "- 当前缺少流程状态，请主要依据最近对话与本轮输入继续规划。")
		return strings.Join(lines, "\n")
	}

	if !state.HasPlan() {
		lines = append(lines, "- 当前还没有正式计划。")
	} else {
		lines = append(lines, fmt.Sprintf("- 已有计划：共 %d 步。", len(state.PlanSteps)))
		lines = append(lines, renderPlanCurrentStepSummary(state))
		lines = append(lines, "计划简表：")
		lines = append(lines, renderPlanStepOutline(state.PlanSteps))
	}

	if taskClassIDs := renderPlanTaskClassIDs(state); taskClassIDs != "" {
		lines = append(lines, "- "+taskClassIDs)
	}
	if taskClassMeta := renderPlanTaskClassMeta(state); taskClassMeta != "" {
		lines = append(lines, "任务类约束：")
		lines = append(lines, taskClassMeta)
	}

	return strings.Join(lines, "\n")
}

// renderPlanCurrentStepSummary 返回 plan 节点需要知道的当前步骤进度。
func renderPlanCurrentStepSummary(state *newagentmodel.CommonState) string {
	if state == nil || !state.HasPlan() {
		return "- 当前步骤：暂无。"
	}

	current, total := state.PlanProgress()
	step, ok := state.CurrentPlanStep()
	if !ok {
		return fmt.Sprintf("- 当前步骤：计划共 %d 步，当前没有可继续沿用的有效步骤。", total)
	}

	content := strings.TrimSpace(step.Content)
	if content == "" {
		content = "（当前步骤正文为空）"
	}

	summary := fmt.Sprintf("- 当前步骤：第 %d/%d 步，%s", current, total, content)
	if doneWhen := strings.TrimSpace(step.DoneWhen); doneWhen != "" {
		summary += fmt.Sprintf("；完成判定：%s", doneWhen)
	}
	return summary
}

// renderPlanStepOutline 将完整计划压成 plan 节点可读的简表。
func renderPlanStepOutline(steps []newagentmodel.PlanStep) string {
	if len(steps) == 0 {
		return "- 暂无。"
	}

	lines := make([]string, 0, len(steps))
	for i, step := range steps {
		content := strings.TrimSpace(step.Content)
		if content == "" {
			content = "（步骤正文为空）"
		}
		line := fmt.Sprintf("%d. %s", i+1, content)
		if doneWhen := strings.TrimSpace(step.DoneWhen); doneWhen != "" {
			line += fmt.Sprintf(" | 完成判定：%s", doneWhen)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// renderPlanTaskClassIDs 返回批量排课场景下的 task_class_ids 简表。
func renderPlanTaskClassIDs(state *newagentmodel.CommonState) string {
	if state == nil || len(state.TaskClassIDs) == 0 {
		return ""
	}

	parts := make([]string, len(state.TaskClassIDs))
	for i, id := range state.TaskClassIDs {
		parts[i] = strconv.Itoa(id)
	}
	return fmt.Sprintf("task_class_ids=[%s]", strings.Join(parts, ", "))
}

// renderPlanTaskClassMeta 返回 plan 节点真正需要看的任务类边界。
//
// 说明：
// 1. 这里只保留名称、策略、总时段、日期范围这类规划相关信息；
// 2. 不再把所有字段原样平铺，避免工作区过胖；
// 3. 若某项字段为空，则直接省略，不制造噪声。
func renderPlanTaskClassMeta(state *newagentmodel.CommonState) string {
	if state == nil || len(state.TaskClasses) == 0 {
		return ""
	}

	lines := make([]string, 0, len(state.TaskClasses))
	for _, tc := range state.TaskClasses {
		line := fmt.Sprintf("- [ID=%d] %s", tc.ID, strings.TrimSpace(tc.Name))
		if strategy := strings.TrimSpace(tc.Strategy); strategy != "" {
			line += fmt.Sprintf("；策略：%s", strategy)
		}
		if tc.TotalSlots > 0 {
			line += fmt.Sprintf("；总时段预算：%d", tc.TotalSlots)
		}
		if tc.StartDate != "" || tc.EndDate != "" {
			line += fmt.Sprintf("；日期范围：%s ~ %s", tc.StartDate, tc.EndDate)
		}
		if len(tc.ExcludedDaysOfWeek) > 0 {
			line += fmt.Sprintf("；排除星期：%v", tc.ExcludedDaysOfWeek)
		}
		if tc.SubjectType != "" || tc.DifficultyLevel != "" || tc.CognitiveIntensity != "" {
			line += fmt.Sprintf("；语义画像：%s/%s/%s",
				planSemanticValue(tc.SubjectType),
				planSemanticValue(tc.DifficultyLevel),
				planSemanticValue(tc.CognitiveIntensity),
			)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func planSemanticValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "未标注"
	}
	return trimmed
}
