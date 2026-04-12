package schedule

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

// GetOverview 获取规划窗口总览（任务视角，全量）。
//
// 设计约束：
// 1. 日内“总占用”保留课程占位影响，避免 LLM 误判可用空间；
// 2. 明细层不展开课程列表，只展开任务（非课程）清单；
// 3. 当前按“窗口不超过 30 天”场景直接全量返回，不做结果截断。
func GetOverview(state *ScheduleState) string {
	totalSlots := state.Window.TotalDays * 12

	// 1. 统计总占用（含课程占位）与空闲。
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

	// 2. 统计“任务视角”状态分布，并单独统计课程条目数。
	taskExistingCount := 0
	taskSuggestedCount := 0
	taskPendingCount := 0
	courseExistingCount := 0
	for i := range state.Tasks {
		task := state.Tasks[i]
		if isCourseScheduleTask(task) {
			if IsExistingTask(task) {
				courseExistingCount++
			}
			continue
		}
		switch {
		case IsPendingTask(task):
			taskPendingCount++
		case IsSuggestedTask(task):
			taskSuggestedCount++
		case IsExistingTask(task):
			taskExistingCount++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("规划窗口共%d天，每天12个时段，总计%d个时段。\n", state.Window.TotalDays, totalSlots))
	sb.WriteString(fmt.Sprintf(
		"当前已占用%d个，空闲%d个。课程占位条目%d个（仅用于占位统计）；任务条目：已安排(existing)%d个、已预排(suggested)%d个、待安排(pending)%d个。\n",
		totalOccupied, totalFree, courseExistingCount, taskExistingCount, taskSuggestedCount, taskPendingCount,
	))

	// 3. 逐天总览：保留课程占位计数，但只展示任务明细。
	sb.WriteString("\n每日概况：\n")
	for day := 1; day <= state.Window.TotalDays; day++ {
		sb.WriteString(buildTaskOnlyOverviewDayLine(state, day) + "\n")
	}

	// 4. 任务清单全量展开（不截断）。
	sb.WriteString("\n任务清单（全量，已过滤课程）：\n")
	sb.WriteString(buildTaskOnlyOverviewList(state))

	// 5. 任务类约束（排课策略与限制）。
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
	sb.WriteString(fmt.Sprintf("%s 全天：\n\n", formatDayLabel(state, day)))

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
	sb.WriteString(fmt.Sprintf("%s第%s：\n\n", formatDayLabel(state, day), formatSlotRange(startSlot, endSlot)))

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

// FindFirstFree 查找首个可用空位，并返回该日详细信息。
//
// 参数说明：
// 1. duration 必填，表示需要的连续时段数；
// 2. day 选填，指定单天搜索；
// 3. dayStart/dayEnd 选填，指定按天范围搜索（闭区间）；
// 4. day 与 dayStart/dayEnd 互斥，避免语义冲突。
//
// 说明：
// 1. 返回“首个命中候选位 + 当日负载明细”，供 LLM 直接决策；
// 2. 当前阶段按用户要求全量返回，不做文本截断。
func FindFirstFree(state *ScheduleState, duration int, day, dayStart, dayEnd *int) string {
	if duration <= 0 {
		return "查询失败：duration 必须大于 0。"
	}

	// 1. 参数互斥校验：单天搜索与范围搜索只能二选一。
	if day != nil && (dayStart != nil || dayEnd != nil) {
		return "查询失败：day 与 day_start/day_end 不能同时传入。"
	}

	// 2. 确定搜索范围。
	days := make([]int, 0)
	if day != nil {
		if *day < 1 || *day > state.Window.TotalDays {
			return fmt.Sprintf("查询失败：第%d天不在规划窗口范围内（1-%d）。", *day, state.Window.TotalDays)
		}
		days = append(days, *day)
	} else {
		startDay := 1
		endDay := state.Window.TotalDays
		if dayStart != nil {
			startDay = *dayStart
		}
		if dayEnd != nil {
			endDay = *dayEnd
		}
		if startDay < 1 || startDay > state.Window.TotalDays {
			return fmt.Sprintf("查询失败：day_start=%d 不在规划窗口范围内（1-%d）。", startDay, state.Window.TotalDays)
		}
		if endDay < 1 || endDay > state.Window.TotalDays {
			return fmt.Sprintf("查询失败：day_end=%d 不在规划窗口范围内（1-%d）。", endDay, state.Window.TotalDays)
		}
		if startDay > endDay {
			return fmt.Sprintf("查询失败：day_start=%d 不能大于 day_end=%d。", startDay, endDay)
		}

		for d := startDay; d <= endDay; d++ {
			days = append(days, d)
		}
	}

	// 3. 按天从前往后寻找“首个可直接放置”的空位。
	for _, d := range days {
		freeRanges := findFreeRangesOnDay(state, d)
		for _, r := range freeRanges {
			rDur := r.slotEnd - r.slotStart + 1
			if rDur < duration {
				continue
			}
			slotStart := r.slotStart
			slotEnd := r.slotStart + duration - 1
			return buildFindFirstFreeReport(state, d, duration, slotStart, slotEnd, false, nil)
		}
	}

	// 4. 若没有纯空位，再尝试首个可嵌入宿主时段。
	for _, d := range days {
		host, slotStart, slotEnd := findFirstEmbeddablePosition(state, d, duration)
		if host != nil {
			return buildFindFirstFreeReport(state, d, duration, slotStart, slotEnd, true, host)
		}
	}

	// 5. 无可用位置时返回摘要，辅助 LLM 判断是否需要换天或降时长。
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("未找到满足%d个连续时段的可用位置。\n", duration))
	sb.WriteString("各天最大连续空闲区（前10天）：\n")
	limit := 10
	if len(days) < limit {
		limit = len(days)
	}
	for i := 0; i < limit; i++ {
		d := days[i]
		freeRanges := findFreeRangesOnDay(state, d)
		maxDur := 0
		for _, r := range freeRanges {
			dur := r.slotEnd - r.slotStart + 1
			if dur > maxDur {
				maxDur = dur
			}
		}
		sb.WriteString(fmt.Sprintf("%s：最大连续空闲%d节\n", formatDayLabel(state, d), maxDur))
	}
	return sb.String()
}

// buildFindFirstFreeReport 构造首个可用位的详细报告。
func buildFindFirstFreeReport(
	state *ScheduleState,
	day int,
	duration int,
	slotStart int,
	slotEnd int,
	isEmbedded bool,
	host *ScheduleTask,
) string {
	var sb strings.Builder
	if isEmbedded && host != nil {
		sb.WriteString(fmt.Sprintf("首个可用位置：%s（可嵌入宿主 [%d]%s）。\n",
			formatDaySlotLabel(state, day, slotStart, slotEnd), host.StateID, host.Name))
	} else {
		sb.WriteString(fmt.Sprintf("首个可用位置：%s（可直接放置）。\n", formatDaySlotLabel(state, day, slotStart, slotEnd)))
	}
	sb.WriteString(fmt.Sprintf("匹配条件：需要%d个连续时段。\n", duration))

	dayTotalOccupied := countDayOccupied(state, day)
	dayTaskOccupied := countDayTaskOccupied(state, day)
	dayCourseOccupied := dayTotalOccupied - dayTaskOccupied
	sb.WriteString(fmt.Sprintf("当日负载：总占%d/12（课程占%d/12，任务占%d/12）。\n", dayTotalOccupied, dayCourseOccupied, dayTaskOccupied))

	sb.WriteString("当日任务明细（全量，已过滤课程）：\n")
	taskEntries := collectTaskEntriesOnDay(state, day)
	if len(taskEntries) == 0 {
		sb.WriteString("  无任务明细。\n")
	} else {
		for _, td := range taskEntries {
			sb.WriteString(fmt.Sprintf("  - [%d]%s | 状态:%s | 类别:%s | 时段:%s\n",
				td.task.StateID, td.task.Name, taskStatusLabel(*td.task), td.task.Category, formatSlotRange(td.slotStart, td.slotEnd)))
		}
	}

	sb.WriteString("当日连续空闲区：\n")
	freeRanges := findFreeRangesOnDay(state, day)
	if len(freeRanges) == 0 {
		sb.WriteString("  无连续空闲区。\n")
	} else {
		for _, r := range freeRanges {
			sb.WriteString("  - " + buildFreeRangeLine(state, r) + "\n")
		}
	}
	return sb.String()
}

// isCourseScheduleTask 判断任务是否属于“课程占位”。
// 用于 get_overview 的任务视角过滤：课程只参与占位统计，不参与任务明细展开。
func isCourseScheduleTask(task ScheduleTask) bool {
	if task.Source != "event" {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(task.EventType), "course") {
		return true
	}
	return strings.TrimSpace(task.Category) == "课程"
}

// taskStatusLabel 返回任务状态标签（existing/suggested/pending）。
func taskStatusLabel(task ScheduleTask) string {
	switch {
	case IsPendingTask(task):
		return "pending"
	case IsSuggestedTask(task):
		return "suggested"
	default:
		return "existing"
	}
}

// collectTaskEntriesOnDay 收集某天的“任务视角”明细（过滤课程）。
func collectTaskEntriesOnDay(state *ScheduleState, day int) []taskOnDay {
	all := getTasksOnDay(state, day)
	result := make([]taskOnDay, 0, len(all))
	for _, item := range all {
		if item.task == nil {
			continue
		}
		if isCourseScheduleTask(*item.task) {
			continue
		}
		result = append(result, item)
	}
	return result
}

// countDayTaskOccupied 统计某天任务（过滤课程）的占用时段数。
func countDayTaskOccupied(state *ScheduleState, day int) int {
	occupied := 0
	for i := range state.Tasks {
		t := state.Tasks[i]
		if isCourseScheduleTask(t) {
			continue
		}
		if t.EmbedHost != nil {
			continue // 嵌入任务不重复计占用
		}
		for _, slot := range t.Slots {
			if slot.Day == day {
				occupied += slot.SlotEnd - slot.SlotStart + 1
			}
		}
	}
	return occupied
}

// buildTaskOnlyOverviewDayLine 生成某天“课程占位 + 任务明细”的摘要行。
func buildTaskOnlyOverviewDayLine(state *ScheduleState, day int) string {
	totalOccupied := countDayOccupied(state, day)
	taskOccupied := countDayTaskOccupied(state, day)
	courseOccupied := totalOccupied - taskOccupied
	taskEntries := collectTaskEntriesOnDay(state, day)
	dayLabel := formatDayLabel(state, day)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s：总占%d/12（课程占%d/12，任务占%d/12）", dayLabel, totalOccupied, courseOccupied, taskOccupied))
	if len(taskEntries) == 0 {
		sb.WriteString(" — 任务：无")
		return sb.String()
	}

	sb.WriteString(" — 任务：")
	for i, item := range taskEntries {
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(fmt.Sprintf("[%d]%s(%s,%s)",
			item.task.StateID,
			item.task.Name,
			taskStatusLabel(*item.task),
			formatSlotRange(item.slotStart, item.slotEnd),
		))
	}
	return sb.String()
}

// buildTaskOnlyOverviewList 输出“全量任务清单”（过滤课程）。
func buildTaskOnlyOverviewList(state *ScheduleState) string {
	tasks := make([]ScheduleTask, 0, len(state.Tasks))
	for i := range state.Tasks {
		task := state.Tasks[i]
		if isCourseScheduleTask(task) {
			continue
		}
		tasks = append(tasks, task)
	}
	if len(tasks) == 0 {
		return "无任务条目。\n"
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].StateID < tasks[j].StateID })

	var sb strings.Builder
	for _, t := range tasks {
		classID := ""
		if t.TaskClassID > 0 {
			classID = fmt.Sprintf(" | task_class_id:%d", t.TaskClassID)
		}
		if IsPendingTask(t) {
			sb.WriteString(fmt.Sprintf("[%d]%s | 状态:%s | 类别:%s%s | 需%d个连续时段\n",
				t.StateID, t.Name, taskStatusLabel(t), t.Category, classID, t.Duration))
			continue
		}
		sb.WriteString(fmt.Sprintf("[%d]%s | 状态:%s | 类别:%s%s | 时段:%s\n",
			t.StateID, t.Name, taskStatusLabel(t), t.Category, classID, formatTaskSlotsBriefWithState(state, t.Slots)))
	}
	return sb.String()
}

// findFirstEmbeddablePosition 查找某天首个可嵌入位置。
func findFirstEmbeddablePosition(state *ScheduleState, day, duration int) (*ScheduleTask, int, int) {
	type candidate struct {
		task      *ScheduleTask
		slotStart int
		slotEnd   int
	}
	candidates := make([]candidate, 0)

	for _, host := range getEmbeddableTasks(state) {
		if host == nil || host.EmbeddedBy != nil {
			continue
		}
		for _, slot := range host.Slots {
			if slot.Day != day {
				continue
			}
			span := slot.SlotEnd - slot.SlotStart + 1
			if span < duration {
				continue
			}
			candidates = append(candidates, candidate{
				task:      host,
				slotStart: slot.SlotStart,
				slotEnd:   slot.SlotStart + duration - 1,
			})
		}
	}

	if len(candidates) == 0 {
		return nil, 0, 0
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].slotStart < candidates[j].slotStart })
	best := candidates[0]
	return best.task, best.slotStart, best.slotEnd
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
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	if statusFilter == "" {
		statusFilter = "all"
	}
	if err := validateListTasksStatus(statusFilter); err != nil {
		return fmt.Sprintf("查询失败：%s", err.Error())
	}
	categoryFilter := ""
	if category != nil {
		categoryFilter = strings.TrimSpace(*category)
	}
	hasCategoryFilter := categoryFilter != ""

	// 2. 过滤 + 分组。
	var existingTasks, suggestedTasks, pendingTasks []ScheduleTask
	for i := range state.Tasks {
		t := state.Tasks[i]
		// 类别过滤。
		if hasCategoryFilter && t.Category != categoryFilter {
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
		if len(pendingTasks) == 0 {
			return formatListTasksEmptyResult(statusFilter, categoryFilter)
		}
		return formatPendingList(pendingTasks)
	}

	// 5. 纯已预排模式：只输出已预排任务。
	if statusFilter == "suggested" {
		if len(suggestedTasks) == 0 {
			return formatListTasksEmptyResult(statusFilter, categoryFilter)
		}
		return formatSuggestedList(state, suggestedTasks)
	}

	// 6. 纯已安排模式：只输出已安排任务。
	if statusFilter == "existing" {
		if len(existingTasks) == 0 {
			return formatListTasksEmptyResult(statusFilter, categoryFilter)
		}
		return formatExistingList(state, existingTasks)
	}

	// 7. 全部模式：统计 + 分组输出。
	total := len(existingTasks) + len(suggestedTasks) + len(pendingTasks)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("共%d个任务，已安排(existing)%d个，已预排(suggested)%d个，待安排(pending)%d个。\n", total, len(existingTasks), len(suggestedTasks), len(pendingTasks)))

	if len(existingTasks) > 0 {
		sb.WriteString("\n已安排(existing)：\n")
		sb.WriteString(formatExistingList(state, existingTasks))
	}
	if len(suggestedTasks) > 0 {
		sb.WriteString("\n已预排(suggested)：\n")
		sb.WriteString(formatSuggestedList(state, suggestedTasks))
	}
	if len(pendingTasks) > 0 {
		sb.WriteString("\n待安排(pending)：\n")
		sb.WriteString(formatPendingList(pendingTasks))
	}

	return sb.String()
}

// formatListTasksEmptyResult 统一构造 list_tasks 空结果文案。
//
// 设计意图：
// 1. 明确告诉模型“为什么为空”，避免把空字符串误解为工具异常或上下文缺失；
// 2. 对常见误用 category=ID 列表给出直接纠偏提示，减少死循环重试。
func formatListTasksEmptyResult(statusFilter, categoryFilter string) string {
	statusLabel := map[string]string{
		"all":       "任意状态",
		"existing":  "已安排(existing)",
		"suggested": "已预排(suggested)",
		"pending":   "待安排(pending)",
	}
	target := statusLabel[statusFilter]
	if target == "" {
		target = statusFilter
	}

	if strings.TrimSpace(categoryFilter) == "" {
		return fmt.Sprintf("查询结果为空：当前没有%s任务。", target)
	}
	if looksLikeTaskClassIDList(categoryFilter) {
		return fmt.Sprintf("查询结果为空：category=%q 未匹配到任务。category 参数按任务类名称匹配，不支持 task_class_ids 列表。", categoryFilter)
	}
	return fmt.Sprintf("查询结果为空：category=%q 下没有%s任务。", categoryFilter, target)
}

// looksLikeTaskClassIDList 判断 category 文本是否像“逗号分隔的数字 ID 列表”。
func looksLikeTaskClassIDList(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	parts := strings.Split(value, ",")
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// validateListTasksStatus 校验 list_tasks.status 的输入值。
//
// 职责边界：
// 1. 负责拦截非法 status，避免“静默返回 0 条”误导模型；
// 2. 不负责自动拆分或容错纠偏（如 existing,suggested），统一要求调用方改成合法单值。
func validateListTasksStatus(status string) error {
	// 1. status 已在调用方归一化为小写并去空格。
	// 2. 合法值仅允许 all / existing / suggested / pending。
	switch status {
	case "all", "existing", "suggested", "pending":
		return nil
	}

	// 3. 对最常见误用给出明确修复建议，避免模型继续循环错误调用。
	if strings.Contains(status, ",") {
		return fmt.Errorf("status 只支持单值 all/existing/suggested/pending，不支持 \"%s\"。如需同时查看 existing+suggested，请使用 all", status)
	}
	return fmt.Errorf("status=%q 非法，仅支持 all/existing/suggested/pending", status)
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
	statusLabel := "已安排(existing)"
	if IsPendingTask(*task) {
		statusLabel = "待安排(pending)"
	} else if IsSuggestedTask(*task) {
		statusLabel = "已预排(suggested)"
	} else if task.Locked {
		statusLabel = "已安排(existing,固定)"
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
			sb.WriteString(fmt.Sprintf("  %s\n", formatDaySlotLabel(state, slot.Day, slot.SlotStart, slot.SlotEnd)))
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
func formatExistingList(state *ScheduleState, tasks []ScheduleTask) string {
	var sb strings.Builder
	for _, t := range tasks {
		label := formatTaskLabelWithCategory(t)
		// 格式化所有时段位置。
		slotParts := make([]string, 0, len(t.Slots))
		for _, slot := range t.Slots {
			slotParts = append(slotParts, fmt.Sprintf("%s(%s)", formatDayLabel(state, slot.Day), formatSlotRange(slot.SlotStart, slot.SlotEnd)))
		}
		sb.WriteString(fmt.Sprintf("  %s — %s\n", label, strings.Join(slotParts, " ")))
	}
	return sb.String()
}

// formatSuggestedList 格式化已预排任务列表。
// 格式如：[3]复习线代 — 已预排至 第2天第3-4节，类别：学习
func formatSuggestedList(state *ScheduleState, tasks []ScheduleTask) string {
	var sb strings.Builder
	if len(tasks) > 0 {
		sb.WriteString(fmt.Sprintf("已预排任务共%d个：\n\n", len(tasks)))
	}
	for _, t := range tasks {
		sb.WriteString(fmt.Sprintf("[%d]%s — 已预排至 %s，类别：%s\n", t.StateID, t.Name, formatTaskSlotsBriefWithState(state, t.Slots), t.Category))
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
