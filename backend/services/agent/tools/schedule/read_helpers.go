package schedule

import (
	"fmt"
	"sort"
	"strings"
)

// ==================== 内部辅助类型 ====================

// taskOnDay 表示某个任务在某一天的一个时段占用。
// 一个任务可能出现在多天，每天可能有多段占用（如周一1-2节 + 周三3-4节）。
type taskOnDay struct {
	task      *ScheduleTask
	slotStart int
	slotEnd   int
}

// freeRange 表示一段连续空闲区间。
type freeRange struct {
	day       int
	slotStart int
	slotEnd   int
}

// ==================== 格式化辅助函数 ====================

// formatSlotRange 将时段范围格式化为人类可读的字符串。
// start == end 时输出 "3节"，否则输出 "1-2节"。
func formatSlotRange(start, end int) string {
	if start == end {
		return fmt.Sprintf("%d节", start)
	}
	return fmt.Sprintf("%d-%d节", start, end)
}

// formatTaskLabel 输出任务的简短标签，如 "[1]高等数学"。
// LLM 交互时统一使用此格式引用任务。
func formatTaskLabel(task ScheduleTask) string {
	return fmt.Sprintf("[%d]%s", task.StateID, task.Name)
}

// formatTaskLabelWithCategory 输出带类别和锁定标记的标签。
// 如 "[1]高等数学(课程,固定)" 或 "[2]英语(课程)"。
// 用于 get_overview 的概要输出。
func formatTaskLabelWithCategory(task ScheduleTask) string {
	label := fmt.Sprintf("[%d]%s(%s", task.StateID, task.Name, task.Category)
	if task.Locked {
		label += ",固定"
	}
	label += ")"
	return label
}

// ==================== 占用计算辅助函数 ====================

// getTasksOnDay 获取某天所有“当前有落位”的任务占用列表。
//
// 说明：
// 1. existing 与 suggested 都属于“有落位”；
// 2. 旧快照里若残留 pending+Slots，也会通过 Slots 被兼容识别；
// 3. 嵌入任务（有 EmbedHost 的）也会被返回，因为它们实际共享了该时段。
// 返回值按 slotStart 升序排列。
func getTasksOnDay(state *ScheduleState, day int) []taskOnDay {
	var result []taskOnDay
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if !hasSlotOnDay(t, day) {
			continue
		}
		for _, slot := range t.Slots {
			if slot.Day == day {
				result = append(result, taskOnDay{
					task:      t,
					slotStart: slot.SlotStart,
					slotEnd:   slot.SlotEnd,
				})
			}
		}
	}
	// 按 slotStart 升序排列，方便逐段输出。
	sort.Slice(result, func(i, j int) bool {
		return result[i].slotStart < result[j].slotStart
	})
	return result
}

// hasSlotOnDay 判断任务是否在某天有时段占用。
func hasSlotOnDay(task *ScheduleTask, day int) bool {
	for _, slot := range task.Slots {
		if slot.Day == day {
			return true
		}
	}
	return false
}

// countDayOccupied 统计某天的已占用时段总数。
// 每个时段（slot）是独立的节次单位，一个 TaskSlot(day=1, start=1, end=2) 占 2 个时段。
// 嵌入任务与宿主共享时段，不重复计算。
func countDayOccupied(state *ScheduleState, day int) int {
	occupied := 0
	for i := range state.Tasks {
		t := &state.Tasks[i]
		// 嵌入任务不重复计算占用——它和宿主共享时段。
		if t.EmbedHost != nil {
			continue
		}
		for _, slot := range t.Slots {
			if slot.Day == day {
				occupied += slot.SlotEnd - slot.SlotStart + 1
			}
		}
	}
	return occupied
}

// slotOccupiedBy 查询某天某节被哪个任务占用。
// 排除嵌入任务（EmbedHost != nil），因为嵌入任务与宿主共享时段。
// 返回 nil 表示该节空闲。
func slotOccupiedBy(state *ScheduleState, day, slot int) *ScheduleTask {
	for i := range state.Tasks {
		t := &state.Tasks[i]
		// 嵌入任务不视为独立占用。
		if t.EmbedHost != nil {
			continue
		}
		for _, s := range t.Slots {
			if s.Day == day && slot >= s.SlotStart && slot <= s.SlotEnd {
				return t
			}
		}
	}
	return nil
}

// ==================== 空闲区间计算 ====================

// findFreeRangesOnDay 计算某天所有连续空闲区间。
// 算法：
//  1. 构建 12 个时段的占用数组（排除嵌入任务，嵌入任务共享宿主时段）
//  2. 扫描连续空闲段
//
// 返回值按 slotStart 升序排列。
func findFreeRangesOnDay(state *ScheduleState, day int) []freeRange {
	// 1. 构建占用数组：occupied[slot] = true 表示该节被占用。
	occupied := make([]bool, 13) // 下标 1-12，0 不使用
	for i := range state.Tasks {
		t := &state.Tasks[i]
		// 嵌入任务与宿主共享时段，不算独立占用。
		if t.EmbedHost != nil {
			continue
		}
		for _, slot := range t.Slots {
			if slot.Day == day {
				for s := slot.SlotStart; s <= slot.SlotEnd; s++ {
					if s >= 1 && s <= 12 {
						occupied[s] = true
					}
				}
			}
		}
	}

	// 2. 扫描连续空闲段。
	var ranges []freeRange
	start := 0
	for s := 1; s <= 12; s++ {
		if !occupied[s] {
			if start == 0 {
				start = s
			}
		} else {
			if start > 0 {
				ranges = append(ranges, freeRange{day: day, slotStart: start, slotEnd: s - 1})
				start = 0
			}
		}
	}
	if start > 0 {
		ranges = append(ranges, freeRange{day: day, slotStart: start, slotEnd: 12})
	}
	return ranges
}

// getEmbeddableTasks 获取所有可嵌入时段的任务列表。
// 条件：CanEmbed == true，用于 query_available_slots 和 get_overview 输出可嵌入位置。
func getEmbeddableTasks(state *ScheduleState) []*ScheduleTask {
	var result []*ScheduleTask
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if t.CanEmbed && len(t.Slots) > 0 {
			result = append(result, t)
		}
	}
	return result
}

// ==================== 通用输出构建 ====================

// buildOverviewDayLine 构建某天的概况行。
// 格式如：第1天：占6/12 — [1]高等数学(1-2节) [2]英语(3-4节)
// 空闲天输出如：第3天：占0/12
func buildOverviewDayLine(state *ScheduleState, day int) string {
	occupied := countDayOccupied(state, day)
	tasks := getTasksOnDay(state, day)
	dayLabel := formatDayLabel(state, day)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s：占%d/12", dayLabel, occupied))

	if len(tasks) > 0 {
		sb.WriteString(" — ")
		for i, td := range tasks {
			if i > 0 {
				sb.WriteString(" ")
			}
			label := formatTaskLabel(*td.task)
			// 如果任务可嵌入且宿主未被嵌入，标注"可嵌入"。
			suffix := ""
			if td.task.CanEmbed && td.task.EmbeddedBy == nil {
				suffix = ",可嵌入"
			}
			sb.WriteString(fmt.Sprintf("%s(%s%s)", label, formatSlotRange(td.slotStart, td.slotEnd), suffix))
		}
	}
	return sb.String()
}

// buildFreeRangeLine 格式化空闲区间行。
// 格式如：第3天 第1-6节（6时段连续空闲）
func buildFreeRangeLine(state *ScheduleState, r freeRange) string {
	dur := r.slotEnd - r.slotStart + 1
	return fmt.Sprintf("%s第%s（%d时段连续空闲）", formatDayLabel(state, r.day), formatSlotRange(r.slotStart, r.slotEnd), dur)
}

// formatSourceName 将 source 字段转为用户可读的来源名称。
// "event" → "课程表"，"task_item" → "任务"。
// 不暴露原始 source 字段值，统一使用中文描述。
func formatSourceName(source string) string {
	switch source {
	case "event":
		return "课程表"
	case "task_item":
		return "任务"
	default:
		return source
	}
}
