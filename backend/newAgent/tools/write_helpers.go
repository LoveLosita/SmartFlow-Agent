package newagenttools

import (
	"fmt"
	"sort"
	"strings"
)

// ==================== 写工具专用辅助函数 ====================

// ==================== 校验函数 ====================

// validateDay 校验 day 是否在规划窗口范围内。
func validateDay(state *ScheduleState, day int) error {
	if day < 1 || day > state.Window.TotalDays {
		return fmt.Errorf("第%d天不在规划窗口范围内（1-%d）", day, state.Window.TotalDays)
	}
	return nil
}

// validateSlotRange 校验时段范围是否合法（1-12，start <= end）。
func validateSlotRange(start, end int) error {
	if start < 1 {
		return fmt.Errorf("起始时段 %d 不能小于1", start)
	}
	if end > 12 {
		return fmt.Errorf("结束时段 %d 不能大于12", end)
	}
	if start > end {
		return fmt.Errorf("起始时段 %d 不能大于结束时段 %d", start, end)
	}
	return nil
}

// checkLocked 检查任务是否被锁定。锁定任务不可移动/交换/移除。
func checkLocked(task ScheduleTask) error {
	if task.Locked {
		return fmt.Errorf("[%d]%s 是固定课程，不可操作", task.StateID, task.Name)
	}
	return nil
}

// ==================== 冲突检测 ====================

// findConflict 查找指定范围 [start, end] 内是否有冲突。
// 排除 excludeStateIDs 中的任务（用于 move/swap 排除自身旧位置）。
// 可嵌入宿主（can_embed=true）不算冲突——嵌入场景由 place 单独处理。
// 返回第一个冲突任务，无冲突返回 nil。
func findConflict(state *ScheduleState, day, start, end int, excludeStateIDs ...int) *ScheduleTask {
	// 构建排除集合
	exclude := make(map[int]bool, len(excludeStateIDs))
	for _, id := range excludeStateIDs {
		exclude[id] = true
	}

	for i := range state.Tasks {
		t := &state.Tasks[i]
		// 排除指定任务
		if exclude[t.StateID] {
			continue
		}
		// 可嵌入宿主不算冲突
		if t.CanEmbed {
			continue
		}
		// 嵌入任务与宿主共享时段，不算独立冲突
		if t.EmbedHost != nil {
			continue
		}
		// 只检查已安排的任务
		if len(t.Slots) == 0 {
			continue
		}
		for _, slot := range t.Slots {
			if slot.Day == day {
				// 检查范围是否有交集：[start,end] ∩ [slot.SlotStart,slot.SlotEnd]
				if start <= slot.SlotEnd && end >= slot.SlotStart {
					return t
				}
			}
		}
	}
	return nil
}

// findEmbedHost 查找指定范围 [start, end] 内是否有可嵌入的宿主。
// 条件：can_embed=true 且未被嵌入（embedded_by == nil）。
// 返回第一个匹配的宿主，无匹配返回 nil。
func findEmbedHost(state *ScheduleState, day, start, end int) *ScheduleTask {
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if !t.CanEmbed || t.EmbeddedBy != nil {
			continue
		}
		for _, slot := range t.Slots {
			if slot.Day == day {
				// 完全包含在宿主时段内才能嵌入
				if start >= slot.SlotStart && end <= slot.SlotEnd {
					return t
				}
			}
		}
	}
	return nil
}

// ==================== 计算辅助 ====================

// taskDuration 计算任务所有 Slots 的总时段数。
// 如 Slots = [{1,1,2}, {3,1,2}] → 总时长 = 2+2 = 4。
// 用于 swap 时比较两个任务的时长是否一致。
func taskDuration(task ScheduleTask) int {
	total := 0
	for _, slot := range task.Slots {
		total += slot.SlotEnd - slot.SlotStart + 1
	}
	return total
}

// countPending 统计当前 state 中“真实待安排”任务数量。
//
// 说明：
// 1. 这里只统计 pending 且无 Slots 的任务；
// 2. 旧快照里 pending+Slots 会被 suggested 兼容层吸收，不再算入待安排。
func countPending(state *ScheduleState) int {
	count := 0
	for i := range state.Tasks {
		if IsPendingTask(state.Tasks[i]) {
			count++
		}
	}
	return count
}

// ==================== 任务时段辅助 ====================

// formatTaskSlotsBrief 将任务的时段列表格式化为简短描述。
// 如 "第1天(1-2节) 第4天(3-4节)"。
func formatTaskSlotsBrief(slots []TaskSlot) string {
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		parts = append(parts, fmt.Sprintf("第%d天第%s", slot.Day, formatSlotRange(slot.SlotStart, slot.SlotEnd)))
	}
	return strings.Join(parts, " ")
}

// collectAffectedDays 从旧位置和新位置中收集所有涉及的天（去重排序）。
func collectAffectedDays(oldSlots, newSlots []TaskSlot) []int {
	days := make(map[int]bool)
	for _, s := range oldSlots {
		days[s.Day] = true
	}
	for _, s := range newSlots {
		days[s.Day] = true
	}
	return sortedKeys(days)
}

// collectAffectedDaysFromSlots 从单个 slot 列表中收集涉及的天。
func collectAffectedDaysFromSlots(slots []TaskSlot) []int {
	days := make(map[int]bool)
	for _, s := range slots {
		days[s.Day] = true
	}
	return sortedKeys(days)
}

// sortedKeys 将 map 的 key 排序后返回。
func sortedKeys(m map[int]bool) []int {
	keys := make([]int, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// uniqueSorted 对 int 切片去重并排序。
func uniqueSorted(s []int) []int {
	seen := make(map[int]bool)
	result := make([]int, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	sort.Ints(result)
	return result
}

// ==================== 输出格式化 ====================

// formatDayOccupancy 格式化某天的占用摘要。
// 如 "第5天当前占用：[3]复习线代(1-3节)，占用3/12。"
// 如 "第4天当前占用：0/12。"（空天）
func formatDayOccupancy(state *ScheduleState, day int) string {
	tasks := getTasksOnDay(state, day)
	occupied := countDayOccupied(state, day)

	if len(tasks) == 0 {
		return fmt.Sprintf("第%d天当前占用：0/12。", day)
	}

	parts := make([]string, 0, len(tasks))
	for _, td := range tasks {
		label := formatTaskLabel(*td.task)
		parts = append(parts, fmt.Sprintf("%s(%s)", label, formatSlotRange(td.slotStart, td.slotEnd)))
	}

	return fmt.Sprintf("第%d天当前占用：%s，占用%d/12。", day, strings.Join(parts, " "), occupied)
}

// formatFreeHint 格式化某天的空闲时段提示。
// 如 "空闲时段：第5-12节。"
// 无空闲时返回空字符串。
func formatFreeHint(state *ScheduleState, day int) string {
	ranges := findFreeRangesOnDay(state, day)
	if len(ranges) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ranges))
	for _, r := range ranges {
		parts = append(parts, formatSlotRange(r.slotStart, r.slotEnd))
	}
	return fmt.Sprintf("空闲时段：%s。", strings.Join(parts, "、"))
}
