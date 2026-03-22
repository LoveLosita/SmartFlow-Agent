package scheduleplan

import (
	"context"
	"fmt"
)

// runQuickRefineNode 是 small 微调分支的“轻量预算收缩节点”。
//
// 职责边界：
// 1. 负责在进入 weekly_refine 前收缩预算与并发，避免小改动走重链路；
// 2. 负责保留“可回退”的最低预算，避免直接压成 0 导致无动作可执行；
// 3. 不负责执行任何 Move/Swap（真正动作仍由 weekly_refine 完成）。
func runQuickRefineNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("schedule plan quick refine: nil state")
	}

	emitStage("schedule_plan.quick_refine.start", "检测到小幅微调，正在切换到快速优化路径。")

	// 1. 预算收缩策略：
	// 1.1 small 场景目标是“快速响应 + 可解释改动”，不追求大规模重排；
	// 1.2 因此把总预算压到最多 2 次尝试、有效预算压到最多 1 次成功动作；
	// 1.3 如果上游已配置更小预算，则尊重更小值，不做反向放大。
	if st.WeeklyTotalBudget <= 0 {
		st.WeeklyTotalBudget = schedulePlanDefaultWeeklyTotalBudget
	}
	if st.WeeklyAdjustBudget <= 0 {
		st.WeeklyAdjustBudget = schedulePlanDefaultWeeklyAdjustBudget
	}
	st.WeeklyTotalBudget = clampBudgetUpper(st.WeeklyTotalBudget, 2)
	st.WeeklyAdjustBudget = clampBudgetUpper(st.WeeklyAdjustBudget, 1)

	// 2. 预算一致性兜底：
	// 2.1 总预算至少为 1（否则 weekly worker 无法执行）；
	// 2.2 有效预算至少为 1（否则所有成功动作都不被允许）；
	// 2.3 有效预算永远不能超过总预算。
	if st.WeeklyTotalBudget < 1 {
		st.WeeklyTotalBudget = 1
	}
	if st.WeeklyAdjustBudget < 1 {
		st.WeeklyAdjustBudget = 1
	}
	if st.WeeklyAdjustBudget > st.WeeklyTotalBudget {
		st.WeeklyAdjustBudget = st.WeeklyTotalBudget
	}

	// 3. 小改动路径把周级并发收敛到 1，优先保证稳定与可观察性。
	st.WeeklyRefineConcurrency = 1

	emitStage(
		"schedule_plan.quick_refine.done",
		fmt.Sprintf(
			"快速微调预算已生效：总预算=%d，有效预算=%d，并发=%d。",
			st.WeeklyTotalBudget,
			st.WeeklyAdjustBudget,
			st.WeeklyRefineConcurrency,
		),
	)
	return st, nil
}

// clampBudgetUpper 把预算裁剪到“非负且不超过上限”。
func clampBudgetUpper(current int, upper int) int {
	if current < 0 {
		return 0
	}
	if current > upper {
		return upper
	}
	return current
}
