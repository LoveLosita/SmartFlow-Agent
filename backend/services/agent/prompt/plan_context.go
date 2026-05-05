package agentprompt

import (
	"fmt"
	"strconv"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
)

// buildPlanConversationMessage 生成 plan 节点看到的真实对话视图。
func buildPlanConversationMessage(ctx *agentmodel.ConversationContext) string {
	return buildConversationHistoryMessage(ctx, "规划参考对话")
}

// buildPlanWorkspace 渲染 plan 节点自己的工作区。
//
// 设计说明：
// 1. 这里既保留“当前已有计划/任务类约束”，也显式补充“规划视角的工具摘要”；
// 2. planner 需要先理解工具边界，才能把步骤收敛到最小闭环，而不是按抽象语义乱拆；
// 3. 工具摘要不展开全量 schema，只提供规划真正需要的：负责什么、不负责什么、常见闭环、完成证据、域切换条件。
func buildPlanWorkspace(state *agentmodel.CommonState) string {
	lines := []string{"规划工作区："}
	if state == nil {
		lines = append(lines, "- 当前缺少流程状态，请主要依据最近对话与本轮输入继续规划。")
		lines = append(lines, buildPlanToolPlanningSummary())
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

	lines = append(lines, buildPlanToolPlanningSummary())
	return strings.Join(lines, "\n")
}

// renderPlanCurrentStepSummary 返回 plan 节点需要知道的当前步骤进度。
func renderPlanCurrentStepSummary(state *agentmodel.CommonState) string {
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
func renderPlanStepOutline(steps []agentmodel.PlanStep) string {
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
func renderPlanTaskClassIDs(state *agentmodel.CommonState) string {
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
func renderPlanTaskClassMeta(state *agentmodel.CommonState) string {
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

// buildPlanToolPlanningSummary 生成“规划视角的工具摘要”。
//
// 步骤化说明：
// 1. 先讲 domain：让 planner 先判断目标应该停留在哪个业务域；
// 2. 再讲 schedule packs：让 planner 知道若进入 schedule，该选最小哪组能力；
// 3. 最后讲 hook 推导规则：因为 context_hook 只有一份，必须和“首个可执行闭环”对齐。
func buildPlanToolPlanningSummary() string {
	sections := []string{
		"规划视角的工具摘要：",
		buildPlanToolDomainTaskClassSummary(),
		buildPlanToolDomainScheduleSummary(),
		buildPlanToolPackSummary(),
		buildPlanContextHookSummary(),
	}
	return strings.Join(sections, "\n")
}

func buildPlanToolDomainTaskClassSummary() string {
	lines := []string{
		"1. taskclass 域：",
		"- 负责什么：创建 / 更新任务类，沉淀学习目标、总节数、难度、节次偏好、禁排时段、排除星期、内容拆分授权、任务项结构。",
		"- 不负责什么：不给出具体日期/节次落位，不负责把任务真正排进日程。",
		"- 常见一步闭环：任务类设计或修正通常可由 taskclass 域单步闭环，核心写入动作为 upsert_task_class。",
		"- 何时停留在本域：用户仍在描述目标、偏好、约束、拆分方式，而不是要求现在排进日程。",
		"- 何时切到下一个域：只有用户明确要求“排进日程 / 给出具体时间安排 / 现在就排一版”，或当前目标本身已变成排程执行。",
		"- done_when 证据偏好：优先锚定 upsert_task_class 成功回执、validation.ok=true、validation.issues 已清空。",
	}
	return strings.Join(lines, "\n")
}

func buildPlanToolDomainScheduleSummary() string {
	lines := []string{
		"2. schedule 域：",
		"- 负责什么：查询日程现状、粗排、具体落位、局部移动/交换、批量同规则调整、排程健康分析。",
		"- 不负责什么：不凭空补考试时间、DDL、个人空闲、外部时间事实；这类信息拿不到时应 ask_user。",
		"- 常见一步闭环：单次查询通常一个读工具即可闭环；单次移动/交换/放置通常一个写工具即可闭环；局部分析通常一个 analyze 工具即可闭环。",
		"- 何时停留在本域：用户明确要求查询、安排、调整、优化当前日程。",
		"- 何时先回 taskclass：如果用户还在定义“学什么、学多少、怎么拆、哪些时段不要学”，而不是要求立刻排程，应先停留在 taskclass。",
		"- done_when 证据偏好：优先锚定查询 observation、写工具成功回执、rough_build_done 标记、analyze observation 已能直接支撑下一步。",
	}
	return strings.Join(lines, "\n")
}

func buildPlanToolPackSummary() string {
	lines := []string{
		"3. schedule packs 选择参考：",
		"- detail_read：查看总览、查询区间、看任务详情；适合“先读事实再决定”的首步。",
		"- mutation：place / move / swap / batch_move / unplace；适合真正落日程或调日程。",
		"- analyze：analyze_health / analyze_rhythm；适合先判断是否还有优化空间、该往哪里动。",
		"- queue：适合“按同一规则逐个处理一批任务”的计划，不必把整批任务细节都堆进 steps。",
		"- web：仅补通用学习资料或通识信息；不用于补个人时间事实。",
		"- deep_analyze：适合确实需要更深一层 schedule 分析时再加，默认不要为了“看起来完整”就提前注入。",
		"- 选 pack 原则：只选首个可执行 step 真的需要的最小 packs，不要为了保险一次全带上。",
	}
	return strings.Join(lines, "\n")
}

func buildPlanContextHookSummary() string {
	lines := []string{
		"4. context_hook 推导规则：",
		"- 先确定 steps，再看第一个可执行 step 需要哪个 domain / 哪组最小 packs，最后才写 hook。",
		"- 若第一个可执行 step 本质上是任务类写入或修正，hook 通常应为 taskclass，且一般不需要 packs。",
		"- 若第一个可执行 step 是 schedule 查询，hook 应为 schedule，并优先只带 detail_read。",
		"- 若第一个可执行 step 是 schedule 分析，hook 应为 schedule，并优先带 analyze；若分析后立刻要落写，再补 mutation。",
		"- 若第一个可执行 step 是批量同规则处理，hook 应在 schedule 基础上按需加 queue。",
		"- hook 只有一份，不要求提前覆盖整份计划的所有后续能力；execute 可以在后续按计划再切域或补 packs。",
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
