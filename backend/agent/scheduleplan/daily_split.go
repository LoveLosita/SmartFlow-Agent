package scheduleplan

import (
	"context"
	"fmt"
)

// runDailySplitNode 负责“按天拆分 + 标签注入 + 跳过判断”。
//
// 职责边界：
// 1. 负责把全量 HybridEntries 拆成 DayGroup，供后续并发日内优化；
// 2. 负责把 TaskTags(task_item_id -> tag) 注入到条目的 ContextTag；
// 3. 负责识别“低收益天”（suggested<=2）并标记 SkipRefine；
// 4. 不负责调用模型，不负责并发执行，不负责结果合并。
func runDailySplitNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil || len(st.HybridEntries) == 0 {
		return st, nil
	}

	emitStage("schedule_plan.daily_split.start", "正在按天拆分排程并标记优化单元。")

	// 1. 初始化容器：
	// 1.1 groups 以 week/day 二级索引保存 DayGroup；
	// 1.2 这么做的目的是后续 daily_refine 可以直接并发遍历，不再重复分组。
	groups := make(map[int]map[int]*DayGroup)

	// 2. 遍历混合条目，执行“标签注入 + 分组”。
	for i := range st.HybridEntries {
		entry := &st.HybridEntries[i]

		// 2.1 仅对 suggested 条目注入 ContextTag。
		// 2.1.1 existing 条目是固定课表/已落库任务，不参与认知标签优化。
		// 2.1.2 注入失败时兜底 General，避免后续 prompt 出现空标签。
		if entry.Status == "suggested" && entry.TaskItemID > 0 {
			if tag, ok := st.TaskTags[entry.TaskItemID]; ok {
				entry.ContextTag = normalizeContextTag(tag)
			} else {
				entry.ContextTag = "General"
			}
		}

		// 2.2 建立分组索引。
		if groups[entry.Week] == nil {
			groups[entry.Week] = make(map[int]*DayGroup)
		}
		if groups[entry.Week][entry.DayOfWeek] == nil {
			groups[entry.Week][entry.DayOfWeek] = &DayGroup{
				Week:      entry.Week,
				DayOfWeek: entry.DayOfWeek,
			}
		}
		groups[entry.Week][entry.DayOfWeek].Entries = append(groups[entry.Week][entry.DayOfWeek].Entries, *entry)
	}

	// 3. 逐天计算 suggested 数量，标记是否跳过日内优化。
	//
	// 3.1 为什么阈值设为 <=2：
	// 3.1.1 suggested 很少时，模型优化收益通常不足以覆盖请求成本；
	// 3.1.2 直接跳过可减少无效模型调用和阶段等待。
	// 3.2 失败策略：
	// 3.2.1 这里只做内存标记，不会失败；
	// 3.2.2 即使阈值判断不完美，也只影响优化深度，不影响功能正确性。
	totalDays := 0
	skipDays := 0
	for _, dayMap := range groups {
		for _, dayGroup := range dayMap {
			totalDays++
			suggestedCount := 0
			for _, e := range dayGroup.Entries {
				if e.Status == "suggested" {
					suggestedCount++
				}
			}
			if suggestedCount <= 2 {
				dayGroup.SkipRefine = true
				skipDays++
			}
		}
	}

	// 4. 回填状态，交给后续节点使用。
	st.DailyGroups = groups
	emitStage(
		"schedule_plan.daily_split.done",
		fmt.Sprintf("已拆分为 %d 天，其中 %d 天跳过日内优化。", totalDays, skipDays),
	)
	return st, nil
}
