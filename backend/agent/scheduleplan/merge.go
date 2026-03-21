package scheduleplan

import (
	"context"
	"fmt"

	"github.com/LoveLosita/smartflow/backend/model"
)

// runMergeNode 负责“合并日内结果 + 冲突校验 + 回退快照”。
//
// 职责边界：
// 1. 负责把 DailyResults 合并回全量 HybridEntries；
// 2. 负责执行时间冲突检测；
// 3. 负责在冲突时回退原始数据；
// 4. 负责产出 MergeSnapshot，供 final_check 失败时回退。
func runMergeNode(
	ctx context.Context,
	st *SchedulePlanState,
	emitStage func(stage, detail string),
) (*SchedulePlanState, error) {
	_ = ctx
	if st == nil || len(st.DailyResults) == 0 {
		return st, nil
	}

	emitStage("schedule_plan.merge.start", "正在合并日内优化结果。")

	// 1. 先保存 merge 前原始数据，作为冲突时的第一层回退兜底。
	originalEntries := deepCopyEntries(st.HybridEntries)

	// 2. 展平 daily results。
	merged := make([]model.HybridScheduleEntry, 0)
	for _, dayMap := range st.DailyResults {
		for _, dayEntries := range dayMap {
			merged = append(merged, dayEntries...)
		}
	}

	// 3. 冲突校验。
	//
	// 3.1 判断依据：同一 (week, day, section) 只能有一个条目占用；
	// 3.2 失败处理：一旦冲突，整批回退到 merge 前原始结果；
	// 3.3 回退策略：回退后仍继续链路，避免请求直接失败。
	if conflict := detectConflicts(merged); conflict != "" {
		st.HybridEntries = originalEntries
		emitStage("schedule_plan.merge.conflict", fmt.Sprintf("检测到冲突并回退：%s", conflict))
	} else {
		st.HybridEntries = merged
		emitStage("schedule_plan.merge.done", fmt.Sprintf("合并完成，共 %d 个条目。", len(merged)))
	}

	// 4. 无论是否冲突，都生成“可回退快照”。
	st.MergeSnapshot = deepCopyEntries(st.HybridEntries)
	return st, nil
}

// detectConflicts 检测条目是否存在时间冲突。
//
// 返回语义：
// 1. 返回空字符串：无冲突；
// 2. 返回非空字符串：冲突描述，可直接用于日志/阶段提示。
func detectConflicts(entries []model.HybridScheduleEntry) string {
	type slotKey struct {
		week, day, section int
	}
	occupied := make(map[slotKey]string)
	for _, entry := range entries {
		// 1. 仅“阻塞建议任务”的条目参与冲突校验。
		// 2. 可嵌入且当前未占用的课程槽位不应被判定为冲突。
		if !entryBlocksSuggested(entry) {
			continue
		}
		for section := entry.SectionFrom; section <= entry.SectionTo; section++ {
			key := slotKey{week: entry.Week, day: entry.DayOfWeek, section: section}
			if prevName, exists := occupied[key]; exists {
				return fmt.Sprintf(
					"W%dD%d 第%d节 冲突：[%s] 与 [%s]",
					entry.Week, entry.DayOfWeek, section, prevName, entry.Name,
				)
			}
			occupied[key] = entry.Name
		}
	}
	return ""
}
