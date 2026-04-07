package newagenttools

import (
	"fmt"
	"sort"
	"strings"
)

// ==================== 读工具：LLM 只通过这些函数感知日程状态 ====================
// 所有读工具：
//   - 只读不改，不修改 state
//   - 返回自然语言 + 轻结构（缩进、列表），LLM 直接理解
//   - 只报当前真实状态，不做建议/推荐/假设
//   - 不暴露 source、source_id、event_type 内部字段

// GetOverview 获取规划窗口的粗粒度总览，用于建立全局感知。
// 无参数，返回整个窗口的占用统计 + 每日概况 + 可嵌入时段 + 待安排任务。
func GetOverview(state *ScheduleState) string {
	totalSlots := state.Window.TotalDays * 12

	// 1. 统计总占用时段数（排除嵌入任务，嵌入与宿主共享时段）。
	totalOccupied := 0
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if t.EmbedHost != nil {
			continue // 嵌入任务不重复计算占用
		}
		for _, slot := range t.Slots {
			totalOccupied += slot.SlotEnd - slot.SlotStart + 1
		}
	}
	totalFree := totalSlots - totalOccupied

	// 2. 统计任务状态分布。
	existingCount := 0
	suggestedCount := 0
	pendingCount := 0
	for i := range state.Tasks {
		task := state.Tasks[i]
		switch {
		case IsPendingTask(task):
			pendingCount++
		case IsSuggestedTask(task):
			suggestedCount++
		case IsExistingTask(task):
			existingCount++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("规划窗口共%d天，每天12个时段，总计%d个时段。\n", state.Window.TotalDays, totalSlots))
	sb.WriteString(fmt.Sprintf("当前已占用%d个，空闲%d个。已确定任务%d个，已预排任务%d个，待安排任务%d个。\n", totalOccupied, totalFree, existingCount, suggestedCount, pendingCount))

	// 3. 逐天概况。
	sb.WriteString("\n每日概况：\n")
	for day := 1; day <= state.Window.TotalDays; day++ {
		sb.WriteString(buildOverviewDayLine(state, day) + "\n")
	}

	// 4. 可嵌入时段汇总（单独列出，方便 LLM 快速定位）。
	embeddable := getEmbeddableTasks(state)
	if len(embeddable) > 0 {
		sb.WriteString("\n可嵌入时段：")
		parts := make([]string, 0, len(embeddable))
		for _, t := range embeddable {
			for _, slot := range t.Slots {
				label := formatTaskLabel(*t)
				embedStatus := "当前无嵌入任务"
				if t.EmbeddedBy != nil {
					guest := state.TaskByStateID(*t.EmbeddedBy)
					if guest != nil {
						embedStatus = fmt.Sprintf("已嵌入[%d]%s", guest.StateID, guest.Name)
					}
				}
				parts = append(parts, fmt.Sprintf("第%d天 %s(%s)", slot.Day, label, embedStatus))
			}
		}
		sb.WriteString(strings.Join(parts, "；") + "\n")
	}

	// 5. 已预排任务汇总。
	if suggestedCount > 0 {
		sb.WriteString("已预排：")
		suggestedParts := make([]string, 0, suggestedCount)
		for i := range state.Tasks {
			t := &state.Tasks[i]
			if IsSuggestedTask(*t) {
				suggestedParts = append(suggestedParts, fmt.Sprintf("[%d]%s(%s)", t.StateID, t.Name, formatTaskSlotsBrief(t.Slots)))
			}
		}
		sb.WriteString(strings.Join(suggestedParts, " ") + "\n")
	}

	// 6. 待安排任务汇总。
	if pendingCount > 0 {
		sb.WriteString("待安排：")
		pendingParts := make([]string, 0, pendingCount)
		for i := range state.Tasks {
			t := &state.Tasks[i]
			if IsPendingTask(*t) {
				pendingParts = append(pendingParts, fmt.Sprintf("[%d]%s(需%d时段)", t.StateID, t.Name, t.Duration))
			}
		}
		sb.WriteString(strings.Join(pendingParts, " ") + "\n")
	}

	// 7. 任务类约束（排课策略与限制）。
	if len(state.TaskClasses) > 0 {
		sb.WriteString("\n任务类约束（排课时请遵守）：\n")
		for _, tc := range state.TaskClasses {
			strategy := formatStrategy(tc.Strategy)
			allow := "否"
			if tc.AllowFillerCourse {
				allow = "是"
			}
			line := fmt.Sprintf("  [%s] 策略=%s 总预算=%d节 允许嵌水课=%s", tc.Name, strategy, tc.TotalSlots, allow)
			if len(tc.ExcludedSlots) > 0 {
				parts := make([]string, len(tc.ExcludedSlots))
				for i, s := range tc.ExcludedSlots {
					parts[i] = fmt.Sprintf("%d", s)
				}
				line += fmt.Sprintf(" 排除时段=[%s]", strings.Join(parts, ","))
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String()
}

// formatStrategy 将 strategy 字段值转为中文描述。
func formatStrategy(strategy string) string {
	switch strategy {
	case "steady":
		return "均匀分布"
	case "rapid":
		return "集中突击"
	default:
		if strategy == "" {
			return "默认"
		}
		return strategy
	}
}

// QueryRange 查看某天（或某天某段）的细粒度占用详情。
// day 必填，slotStart/slotEnd 选填（nil 表示查整天）。
// 整天模式按标准段（1-2, 3-4, ..., 11-12）分组输出。
// 指定范围模式逐节输出。
func QueryRange(state *ScheduleState, day int, slotStart, slotEnd *int) string {
	// 1. 校验 day 是否在有效范围内。
	if day < 1 || day > state.Window.TotalDays {
		return fmt.Sprintf("查询失败：第%d天不在规划窗口范围内（1-%d）。", day, state.Window.TotalDays)
	}

	// 2. 分两种模式：整天查询 vs 指定范围查询。
	if slotStart == nil || slotEnd == nil {
		return queryRangeFullDay(state, day)
	}
	return queryRangeSpecific(state, day, *slotStart, *slotEnd)
}

// queryRangeFullDay 整天查询模式：按标准段分组输出。
// 输出格式对齐 SCHEDULE_TOOLS.md 4.2 节示例。
func queryRangeFullDay(state *ScheduleState, day int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("第%d天 全天：\n\n", day))

	// 1. 按 6 个标准段输出（1-2, 3-4, 5-6, 7-8, 9-10, 11-12）。
	for start := 1; start <= 11; start += 2 {
		end := start + 1
		// 查该段的占用情况，找该段内所有占用任务。
		occupants := tasksInRange(state, day, start, end)
		if len(occupants) == 0 {
			sb.WriteString(fmt.Sprintf("第%s：空\n", formatSlotRange(start, end)))
		} else {
			desc := formatOccupants(occupants)
			sb.WriteString(fmt.Sprintf("第%s：%s\n", formatSlotRange(start, end), desc))
		}
	}

	// 2. 附加连续空闲区摘要。
	freeRanges := findFreeRangesOnDay(state, day)
	if len(freeRanges) > 0 {
		sb.WriteString("\n连续空闲区：")
		rangeParts := make([]string, 0, len(freeRanges))
		for _, r := range freeRanges {
			dur := r.slotEnd - r.slotStart + 1
			rangeParts = append(rangeParts, fmt.Sprintf("第%s(%d时段)", formatSlotRange(r.slotStart, r.slotEnd), dur))
		}
		sb.WriteString(strings.Join(rangeParts, "、") + "\n")
	}

	// 3. 附加可嵌入信息（仅当该天有可嵌入时段时输出）。
	embedInfo := formatEmbedInfoForDay(state, day)
	if embedInfo != "" {
		sb.WriteString("可嵌入：" + embedInfo + "\n")
	}

	return sb.String()
}

// queryRangeSpecific 指定范围查询模式：逐节输出。
func queryRangeSpecific(state *ScheduleState, day, startSlot, endSlot int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("第%d天 第%s：\n\n", day, formatSlotRange(startSlot, endSlot)))

	total := endSlot - startSlot + 1
	freeCount := 0
	for s := startSlot; s <= endSlot; s++ {
		occupant := slotOccupiedBy(state, day, s)
		if occupant == nil {
			sb.WriteString(fmt.Sprintf("第%d节：空\n", s))
			freeCount++
		} else {
			sb.WriteString(fmt.Sprintf("第%d节：[%d]%s\n", s, occupant.StateID, occupant.Name))
		}
	}

	if freeCount == total {
		sb.WriteString(fmt.Sprintf("\n该范围%d个时段全部空闲。\n", total))
	} else {
		sb.WriteString(fmt.Sprintf("\n该范围%d个时段中，%d个空闲，%d个被占用。\n", total, freeCount, total-freeCount))
	}

	return sb.String()
}

// FindFree 查找满足指定连续时段长度的空闲位置。
// duration 必填，day 选填（nil 表示搜索全部天）。
// 返回所有 >= duration 的空闲连续区间 + 可嵌入位置。
func FindFree(state *ScheduleState, duration int, day *int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("满足%d个连续空闲时段的位置：\n\n", duration))

	// 1. 确定搜索范围。
	days := make([]int, 0)
	if day != nil {
		if *day < 1 || *day > state.Window.TotalDays {
			return fmt.Sprintf("查询失败：第%d天不在规划窗口范围内（1-%d）。", *day, state.Window.TotalDays)
		}
		days = append(days, *day)
	} else {
		for d := 1; d <= state.Window.TotalDays; d++ {
			days = append(days, d)
		}
	}

	// 2. 逐天查找满足条件的空闲区间。
	found := 0
	for _, d := range days {
		freeRanges := findFreeRangesOnDay(state, d)
		for _, r := range freeRanges {
			rDur := r.slotEnd - r.slotStart + 1
			if rDur >= duration {
				sb.WriteString(fmt.Sprintf("第%d天 第%s（%d时段连续空闲）\n", d, formatSlotRange(r.slotStart, r.slotEnd), rDur))
				found++
			}
		}
	}

	if found == 0 {
		sb.WriteString("未找到满足条件的空闲时段。\n")
	}

	// 3. 可嵌入位置单独列出（水课时段，可叠加任务）。
	embeddable := getEmbeddableTasks(state)
	if len(embeddable) > 0 {
		sb.WriteString("\n可嵌入位置（水课时段，可叠加任务）：\n")
		for _, t := range embeddable {
			for _, slot := range t.Slots {
				// 检查是否在搜索范围内。
				if day != nil && slot.Day != *day {
					continue
				}
				embedStatus := "当前无嵌入任务"
				if t.EmbeddedBy != nil {
					guest := state.TaskByStateID(*t.EmbeddedBy)
					if guest != nil {
						embedStatus = fmt.Sprintf("已嵌入[%d]%s", guest.StateID, guest.Name)
					}
				}
				sb.WriteString(fmt.Sprintf("第%d天 第%s（[%d]%s，%s）\n", slot.Day, formatSlotRange(slot.SlotStart, slot.SlotEnd), t.StateID, t.Name, embedStatus))
			}
		}
	}

	return sb.String()
}

// ListTasks 列出任务清单，可按类别和状态过滤。
// category 选填（nil 不过滤），status 选填（nil 默认 "all"）。
// 输出按状态分组：已安排 -> 已预排 -> 待安排，组内按 stateID 升序。
func ListTasks(state *ScheduleState, category, status *string) string {
	// 1. 确定过滤状态。
	statusFilter := "all"
	if status != nil {
		statusFilter = *status
	}

	// 2. 过滤 + 分组。
	var existingTasks, suggestedTasks, pendingTasks []ScheduleTask
	for i := range state.Tasks {
		t := state.Tasks[i]
		// 类别过滤。
		if category != nil && t.Category != *category {
			continue
		}

		switch {
		case IsPendingTask(t):
			if statusFilter != "all" && statusFilter != "pending" {
				continue
			}
			pendingTasks = append(pendingTasks, t)
		case IsSuggestedTask(t):
			if statusFilter != "all" && statusFilter != "suggested" {
				continue
			}
			suggestedTasks = append(suggestedTasks, t)
		default:
			if statusFilter != "all" && statusFilter != "existing" {
				continue
			}
			existingTasks = append(existingTasks, t)
		}
	}

	// 3. 按 stateID 排序。
	sort.Slice(existingTasks, func(i, j int) bool { return existingTasks[i].StateID < existingTasks[j].StateID })
	sort.Slice(suggestedTasks, func(i, j int) bool { return suggestedTasks[i].StateID < suggestedTasks[j].StateID })
	sort.Slice(pendingTasks, func(i, j int) bool { return pendingTasks[i].StateID < pendingTasks[j].StateID })

	// 4. 纯待安排模式：只输出待安排任务。
	if statusFilter == "pending" {
		return formatPendingList(pendingTasks)
	}

	// 5. 纯已预排模式：只输出已预排任务。
	if statusFilter == "suggested" {
		return formatSuggestedList(suggestedTasks)
	}

	// 6. 纯已安排模式：只输出已安排任务。
	if statusFilter == "existing" {
		return formatExistingList(existingTasks)
	}

	// 7. 全部模式：统计 + 分组输出。
	total := len(existingTasks) + len(suggestedTasks) + len(pendingTasks)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("共%d个任务，已安排%d个，已预排%d个，待安排%d个。\n", total, len(existingTasks), len(suggestedTasks), len(pendingTasks)))

	if len(existingTasks) > 0 {
		sb.WriteString("\n已安排：\n")
		sb.WriteString(formatExistingList(existingTasks))
	}
	if len(suggestedTasks) > 0 {
		sb.WriteString("\n已预排：\n")
		sb.WriteString(formatSuggestedList(suggestedTasks))
	}
	if len(pendingTasks) > 0 {
		sb.WriteString("\n待安排：\n")
		sb.WriteString(formatPendingList(pendingTasks))
	}

	return sb.String()
}

// GetTaskInfo 查询单个任务的详细信息。
// taskID 必填，为 state 内的 state_id。
// 不存在时返回错误信息字符串。
func GetTaskInfo(state *ScheduleState, taskID int) string {
	task := state.TaskByStateID(taskID)
	if task == nil {
		return fmt.Sprintf("查询失败：任务ID %d 不存在。", taskID)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[%d]%s\n", task.StateID, task.Name))

	// 1. 类别、状态、来源。
	statusLabel := "已安排"
	if IsPendingTask(*task) {
		statusLabel = "待安排"
	} else if IsSuggestedTask(*task) {
		statusLabel = "已预排"
	} else if task.Locked {
		statusLabel = "已安排（固定）"
	}
	sb.WriteString(fmt.Sprintf("类别：%s | 状态：%s\n", task.Category, statusLabel))
	sb.WriteString(fmt.Sprintf("来源：%s\n", formatSourceName(task.Source)))

	// 2. 可嵌入信息（仅 can_embed 任务显示）。
	if task.CanEmbed {
		sb.WriteString("可嵌入：是（允许在此时段嵌入其他任务）\n")
	}

	// 3. 占用时段。
	if len(task.Slots) > 0 {
		sb.WriteString("占用时段：\n")
		for _, slot := range task.Slots {
			sb.WriteString(fmt.Sprintf("  第%d天 第%s\n", slot.Day, formatSlotRange(slot.SlotStart, slot.SlotEnd)))
		}
	}

	// 4. 任务时长信息。
	if IsPendingTask(*task) {
		sb.WriteString(fmt.Sprintf("需要时段：%d个连续时段\n", task.Duration))
	} else if IsSuggestedTask(*task) && task.Duration > 0 {
		sb.WriteString(fmt.Sprintf("原始需求：%d个连续时段\n", task.Duration))
	}

	// 5. 嵌入关系信息。
	if task.CanEmbed {
		if task.EmbeddedBy != nil {
			guest := state.TaskByStateID(*task.EmbeddedBy)
			if guest != nil {
				sb.WriteString(fmt.Sprintf("当前嵌入任务：[%d]%s\n", guest.StateID, guest.Name))
			}
		} else {
			sb.WriteString("当前嵌入任务：无\n")
		}
	}
	if task.EmbedHost != nil {
		host := state.TaskByStateID(*task.EmbedHost)
		if host != nil {
			sb.WriteString(fmt.Sprintf("嵌入宿主：[%d]%s\n", host.StateID, host.Name))
		}
	}

	return sb.String()
}

// ==================== 内部格式化函数 ====================

// tasksInRange 获取某天指定时段范围内的占用任务列表。
// 返回在该范围内有占用的所有任务（去重，按 slotStart 排序）。
func tasksInRange(state *ScheduleState, day, start, end int) []taskOnDay {
	tasks := getTasksOnDay(state, day)
	var result []taskOnDay
	for _, td := range tasks {
		// 判断是否有交集：任务的 [slotStart, slotEnd] 与查询范围 [start, end] 有重叠。
		if td.slotStart <= end && td.slotEnd >= start {
			result = append(result, td)
		}
	}
	return result
}

// formatOccupants 格式化占用任务列表为紧凑描述。
// 如 "[1]高等数学（固定）" 或 "[6]线代"
func formatOccupants(occupants []taskOnDay) string {
	parts := make([]string, 0, len(occupants))
	for _, o := range occupants {
		label := formatTaskLabel(*o.task)
		if o.task.Locked {
			parts = append(parts, label+"（固定）")
		} else if o.task.CanEmbed {
			parts = append(parts, label+"（可嵌入）")
		} else {
			parts = append(parts, label)
		}
	}
	return strings.Join(parts, " ")
}

// formatEmbedInfoForDay 格式化某天的可嵌入信息。
// 返回空字符串表示该天没有可嵌入时段。
func formatEmbedInfoForDay(state *ScheduleState, day int) string {
	var parts []string
	for i := range state.Tasks {
		t := &state.Tasks[i]
		if !t.CanEmbed {
			continue
		}
		for _, slot := range t.Slots {
			if slot.Day != day {
				continue
			}
			label := formatTaskLabel(*t)
			if t.Locked {
				parts = append(parts, fmt.Sprintf("第%s已有%s（固定，不可嵌入）", formatSlotRange(slot.SlotStart, slot.SlotEnd), label))
			} else {
				embedStatus := "可嵌入"
				if t.EmbeddedBy != nil {
					guest := state.TaskByStateID(*t.EmbeddedBy)
					if guest != nil {
						embedStatus = fmt.Sprintf("已嵌入[%d]%s", guest.StateID, guest.Name)
					}
				}
				parts = append(parts, fmt.Sprintf("第%s已有%s（%s）", formatSlotRange(slot.SlotStart, slot.SlotEnd), label, embedStatus))
			}
		}
	}
	return strings.Join(parts, "；")
}

// formatExistingList 格式化已安排任务列表。
// 格式如：  [1]高等数学(课程,固定) — 第1天(1-2节) 第4天(1-2节)
func formatExistingList(tasks []ScheduleTask) string {
	var sb strings.Builder
	for _, t := range tasks {
		label := formatTaskLabelWithCategory(t)
		// 格式化所有时段位置。
		slotParts := make([]string, 0, len(t.Slots))
		for _, slot := range t.Slots {
			slotParts = append(slotParts, fmt.Sprintf("第%d天(%s)", slot.Day, formatSlotRange(slot.SlotStart, slot.SlotEnd)))
		}
		sb.WriteString(fmt.Sprintf("  %s — %s\n", label, strings.Join(slotParts, " ")))
	}
	return sb.String()
}

// formatSuggestedList 格式化已预排任务列表。
// 格式如：[3]复习线代 — 已预排至 第2天第3-4节，类别：学习
func formatSuggestedList(tasks []ScheduleTask) string {
	var sb strings.Builder
	if len(tasks) > 0 {
		sb.WriteString(fmt.Sprintf("已预排任务共%d个：\n\n", len(tasks)))
	}
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("[%d]%s — 已预排至 %s，类别：%s\n", t.StateID, t.Name, formatTaskSlotsBrief(t.Slots), t.Category))
	}
	return sb.String()
}

// formatPendingList 格式化待安排任务列表。
// 格式如：[3]复习线代 — 需3个连续时段，类别：学习
func formatPendingList(tasks []ScheduleTask) string {
	var sb strings.Builder
	if len(tasks) > 0 {
		sb.WriteString(fmt.Sprintf("待安排任务共%d个：\n\n", len(tasks)))
	}
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("[%d]%s — 需%d个连续时段，类别：%s\n", t.StateID, t.Name, t.Duration, t.Category))
	}
	return sb.String()
}
