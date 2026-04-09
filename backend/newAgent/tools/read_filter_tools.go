package newagenttools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// queryAvailableSlotsResult 描述 query_available_slots 的结构化返回。
type queryAvailableSlotsResult struct {
	Tool            string                   `json:"tool"`
	Count           int                      `json:"count"`
	StrictCount     int                      `json:"strict_count"`
	EmbeddedCount   int                      `json:"embedded_count"`
	FallbackUsed    bool                     `json:"fallback_used"`
	DayScope        string                   `json:"day_scope"`
	DayOfWeek       []int                    `json:"day_of_week"`
	WeekFilter      []int                    `json:"week_filter"`
	WeekFrom        int                      `json:"week_from"`
	WeekTo          int                      `json:"week_to"`
	Span            int                      `json:"span"`
	AllowEmbed      bool                     `json:"allow_embed"`
	ExcludeSections []int                    `json:"exclude_sections"`
	Slots           []queryAvailableSlotItem `json:"slots"`
}

// queryAvailableSlotItem 描述单个候选坑位。
type queryAvailableSlotItem struct {
	Day       int    `json:"day"`
	Week      int    `json:"week"`
	DayOfWeek int    `json:"day_of_week"`
	SlotStart int    `json:"slot_start"`
	SlotEnd   int    `json:"slot_end"`
	SlotType  string `json:"slot_type,omitempty"`
}

// queryTargetTasksResult 描述 query_target_tasks 的结构化返回。
type queryTargetTasksResult struct {
	Tool       string                `json:"tool"`
	Count      int                   `json:"count"`
	Status     string                `json:"status"`
	DayScope   string                `json:"day_scope"`
	DayOfWeek  []int                 `json:"day_of_week"`
	WeekFilter []int                 `json:"week_filter"`
	WeekFrom   int                   `json:"week_from"`
	WeekTo     int                   `json:"week_to"`
	Enqueue    bool                  `json:"enqueue"`
	Enqueued   int                   `json:"enqueued"`
	Queue      *queryTargetQueueInfo `json:"queue,omitempty"`
	Items      []queryTargetTaskItem `json:"items"`
}

// queryTargetQueueInfo 描述 query_target_tasks 入队后的队列摘要。
type queryTargetQueueInfo struct {
	PendingCount   int `json:"pending_count"`
	CompletedCount int `json:"completed_count"`
	SkippedCount   int `json:"skipped_count"`
	CurrentTaskID  int `json:"current_task_id,omitempty"`
	CurrentAttempt int `json:"current_attempt,omitempty"`
}

// queryTargetTaskItem 描述候选任务。
type queryTargetTaskItem struct {
	TaskID      int                   `json:"task_id"`
	Name        string                `json:"name"`
	Category    string                `json:"category,omitempty"`
	Status      string                `json:"status"`
	Duration    int                   `json:"duration,omitempty"`
	TaskClassID int                   `json:"task_class_id,omitempty"`
	Slots       []queryTargetTaskSlot `json:"slots,omitempty"`
}

// queryTargetTaskSlot 描述任务在工具状态中的坐标。
type queryTargetTaskSlot struct {
	Day       int `json:"day"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	SlotStart int `json:"slot_start"`
	SlotEnd   int `json:"slot_end"`
}

// queryAvailableOptions 是 query_available_slots 的参数快照。
type queryAvailableOptions struct {
	DayScope        string
	DayOfWeekSet    map[int]struct{}
	WeekSet         map[int]struct{}
	WeekFrom        int
	WeekTo          int
	Span            int
	Limit           int
	AllowEmbed      bool
	ExcludedSection map[int]struct{}
	AfterSection    *int
	BeforeSection   *int
	ExactFrom       *int
	ExactTo         *int
}

// queryTargetOptions 是 query_target_tasks 的参数快照。
type queryTargetOptions struct {
	DayScope     string
	DayOfWeekSet map[int]struct{}
	WeekSet      map[int]struct{}
	WeekFrom     int
	WeekTo       int
	Status       string
	Limit        int
	TaskIDSet    map[int]struct{}
	Category     string
	Enqueue      bool
	ResetQueue   bool
}

// QueryAvailableSlots 返回“候选坑位池”。
//
// 职责边界：
// 1. 只负责读状态并返回结构化 JSON，不做任何写入；
// 2. 优先返回纯空位（strict），不足时再补可嵌入位（embedded）；
// 3. 不负责移动策略决策，最终落点由模型结合目标再选择。
func QueryAvailableSlots(state *ScheduleState, args map[string]any) string {
	// 1. 解析参数并做合法性校验。
	options, err := parseQueryAvailableOptions(state, args)
	if err != nil {
		return fmt.Sprintf(`{"tool":"query_available_slots","success":false,"error":"%s"}`, err.Error())
	}

	// 2. 解析“可迭代天集合”：先解析 day/day_start/day_end，再叠加 week/day_scope/day_of_week 过滤。
	candidateDays, err := resolveCandidateDays(state, args, options.DayScope, options.DayOfWeekSet, options.WeekSet, options.WeekFrom, options.WeekTo)
	if err != nil {
		return fmt.Sprintf(`{"tool":"query_available_slots","success":false,"error":"%s"}`, err.Error())
	}

	// 3. 两阶段收集：
	// 3.1 先收集 strict（纯空位），保证“先空位后嵌入”的默认策略；
	// 3.2 strict 不足 limit 时，再补 embed 候选（仅在 allow_embed=true 时）。
	slots := make([]queryAvailableSlotItem, 0, options.Limit)
	seen := make(map[string]struct{}, options.Limit*2)

	collect := func(embedAllowed bool, slotType string) {
		if len(slots) >= options.Limit {
			return
		}
		for _, day := range candidateDays {
			week, dayOfWeek, ok := state.DayToWeekDay(day)
			if !ok {
				continue
			}
			for slotStart := 1; slotStart+options.Span-1 <= 12; slotStart++ {
				slotEnd := slotStart + options.Span - 1
				if !matchSectionRange(slotStart, slotEnd, options.ExcludedSection, options.AfterSection, options.BeforeSection, options.ExactFrom, options.ExactTo) {
					continue
				}

				accepted := false
				if !embedAllowed {
					accepted = isStrictSlotAvailable(state, day, slotStart, slotEnd)
				} else {
					accepted = isEmbeddableSlotAvailable(state, day, slotStart, slotEnd)
				}
				if !accepted {
					continue
				}

				key := fmt.Sprintf("%d-%d-%d", day, slotStart, slotEnd)
				if _, exists := seen[key]; exists {
					continue
				}
				seen[key] = struct{}{}
				slots = append(slots, queryAvailableSlotItem{
					Day:       day,
					Week:      week,
					DayOfWeek: dayOfWeek,
					SlotStart: slotStart,
					SlotEnd:   slotEnd,
					SlotType:  slotType,
				})
				if len(slots) >= options.Limit {
					return
				}
			}
		}
	}

	collect(false, "empty")
	strictCount := len(slots)
	if options.AllowEmbed && len(slots) < options.Limit {
		collect(true, "embedded_candidate")
	}
	embeddedCount := len(slots) - strictCount

	// 4. 组装结构化返回（JSON 字符串）。
	result := queryAvailableSlotsResult{
		Tool:            "query_available_slots",
		Count:           len(slots),
		StrictCount:     strictCount,
		EmbeddedCount:   embeddedCount,
		FallbackUsed:    embeddedCount > 0,
		DayScope:        options.DayScope,
		DayOfWeek:       sortedSetKeys(options.DayOfWeekSet),
		WeekFilter:      sortedSetKeys(options.WeekSet),
		WeekFrom:        options.WeekFrom,
		WeekTo:          options.WeekTo,
		Span:            options.Span,
		AllowEmbed:      options.AllowEmbed,
		ExcludeSections: sortedSetKeys(options.ExcludedSection),
		Slots:           slots,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"tool":"query_available_slots","success":false,"error":"query encode failed"}`
	}
	return string(raw)
}

// QueryTargetTasks 返回“候选任务集合”。
//
// 职责边界：
// 1. 只做筛选与结构化返回，不直接执行 move/swap；
// 2. 默认 status=suggested，减少模型误选 existing/pending；
// 3. 仅返回状态事实，不做“该不该移动”的语义判断。
func QueryTargetTasks(state *ScheduleState, args map[string]any) string {
	// 1. 解析参数。
	options, err := parseQueryTargetOptions(state, args)
	if err != nil {
		return fmt.Sprintf(`{"tool":"query_target_tasks","success":false,"error":"%s"}`, err.Error())
	}

	// 2. 解析“可迭代天集合”过滤器。
	candidateDays, err := resolveCandidateDays(state, args, options.DayScope, options.DayOfWeekSet, options.WeekSet, options.WeekFrom, options.WeekTo)
	if err != nil {
		return fmt.Sprintf(`{"tool":"query_target_tasks","success":false,"error":"%s"}`, err.Error())
	}
	calendarFilterActive := isQueryTargetCalendarFilterActive(args, options)
	daySet := make(map[int]struct{}, len(candidateDays))
	for _, d := range candidateDays {
		daySet[d] = struct{}{}
	}

	// 3. 扫描任务并按筛选条件收敛。
	items := make([]queryTargetTaskItem, 0, options.Limit)
	for i := range state.Tasks {
		task := state.Tasks[i]
		if !matchTaskStatus(task, options.Status) {
			continue
		}
		if len(options.TaskIDSet) > 0 {
			if _, ok := options.TaskIDSet[task.StateID]; !ok {
				continue
			}
		}
		if options.Category != "" && task.Category != options.Category {
			continue
		}

		taskSlots := make([]queryTargetTaskSlot, 0, len(task.Slots))
		for _, slot := range task.Slots {
			week, dayOfWeek, ok := state.DayToWeekDay(slot.Day)
			if !ok {
				continue
			}
			// 3.1 若存在日历过滤条件，只保留命中过滤的坐标。
			if calendarFilterActive && len(daySet) > 0 {
				if _, hit := daySet[slot.Day]; !hit {
					continue
				}
			}
			taskSlots = append(taskSlots, queryTargetTaskSlot{
				Day:       slot.Day,
				Week:      week,
				DayOfWeek: dayOfWeek,
				SlotStart: slot.SlotStart,
				SlotEnd:   slot.SlotEnd,
			})
		}

		// 3.2 pending 任务默认无 slots；当存在日历过滤条件时，不应混入“未知坐标任务”。
		if len(taskSlots) == 0 && calendarFilterActive {
			continue
		}
		sort.Slice(taskSlots, func(i, j int) bool {
			if taskSlots[i].Day != taskSlots[j].Day {
				return taskSlots[i].Day < taskSlots[j].Day
			}
			if taskSlots[i].SlotStart != taskSlots[j].SlotStart {
				return taskSlots[i].SlotStart < taskSlots[j].SlotStart
			}
			return taskSlots[i].SlotEnd < taskSlots[j].SlotEnd
		})

		items = append(items, queryTargetTaskItem{
			TaskID:      task.StateID,
			Name:        strings.TrimSpace(task.Name),
			Category:    strings.TrimSpace(task.Category),
			Status:      buildTaskStatusLabel(task),
			Duration:    task.Duration,
			TaskClassID: task.TaskClassID,
			Slots:       taskSlots,
		})
	}

	// 4. 稳定排序：先按最早坐标，再按 task_id。
	sort.Slice(items, func(i, j int) bool {
		leftHasSlot := len(items[i].Slots) > 0
		rightHasSlot := len(items[j].Slots) > 0
		if leftHasSlot != rightHasSlot {
			return leftHasSlot
		}
		if leftHasSlot {
			left := items[i].Slots[0]
			right := items[j].Slots[0]
			if left.Day != right.Day {
				return left.Day < right.Day
			}
			if left.SlotStart != right.SlotStart {
				return left.SlotStart < right.SlotStart
			}
		}
		return items[i].TaskID < items[j].TaskID
	})
	if len(items) > options.Limit {
		items = items[:options.Limit]
	}

	// 5. 队列化（可选）：将筛选结果自动纳入“待处理队列”。
	//
	// 步骤化说明：
	// 1. 默认 enqueue=true，让 LLM 优先走“逐项处理”而不是一次性批量组合；
	// 2. reset_queue=true 时会清空旧队列后再入队，适合开启新一轮筛选；
	// 3. 入队仅保存 task_id，不复制任务全文，避免队列状态膨胀。
	queueInfo := (*queryTargetQueueInfo)(nil)
	enqueued := 0
	if options.Enqueue {
		taskIDs := make([]int, 0, len(items))
		for _, item := range items {
			taskIDs = append(taskIDs, item.TaskID)
		}
		if options.ResetQueue {
			enqueued = ReplaceTaskProcessingQueue(state, taskIDs)
		} else {
			enqueued = appendTaskIDsToQueue(state, taskIDs)
		}
		queue := ensureTaskProcessingQueue(state)
		queueInfo = &queryTargetQueueInfo{
			PendingCount:   len(queue.PendingTaskIDs),
			CompletedCount: len(queue.CompletedTaskIDs),
			SkippedCount:   len(queue.SkippedTaskIDs),
			CurrentTaskID:  queue.CurrentTaskID,
			CurrentAttempt: queue.CurrentAttempts,
		}
	}

	// 6. 结构化返回。
	result := queryTargetTasksResult{
		Tool:       "query_target_tasks",
		Count:      len(items),
		Status:     options.Status,
		DayScope:   options.DayScope,
		DayOfWeek:  sortedSetKeys(options.DayOfWeekSet),
		WeekFilter: sortedSetKeys(options.WeekSet),
		WeekFrom:   options.WeekFrom,
		WeekTo:     options.WeekTo,
		Enqueue:    options.Enqueue,
		Enqueued:   enqueued,
		Queue:      queueInfo,
		Items:      items,
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"tool":"query_target_tasks","success":false,"error":"query encode failed"}`
	}
	return string(raw)
}

// parseQueryAvailableOptions 解析 query_available_slots 参数。
func parseQueryAvailableOptions(state *ScheduleState, args map[string]any) (queryAvailableOptions, error) {
	scope := normalizeDayScope(readStringAny(args, "day_scope", "all"))

	allowEmbed := readBoolAnyWithDefault(args, true, "allow_embed", "allow_embedding")
	slotTypeHints := readStringSliceAny(args, "slot_types")
	if single := strings.TrimSpace(readStringAny(args, "slot_type", "")); single != "" {
		slotTypeHints = append(slotTypeHints, single)
	}
	for _, hint := range slotTypeHints {
		normalized := strings.ToLower(strings.TrimSpace(hint))
		if normalized == "pure" || normalized == "empty" || normalized == "strict" {
			allowEmbed = false
			break
		}
	}

	span, ok := readIntAny(args, "span", "section_duration", "task_duration", "duration")
	if !ok || span <= 0 {
		span = 2
	}
	if span > 12 {
		return queryAvailableOptions{}, fmt.Errorf("span=%d 非法，必须在 1~12", span)
	}

	limit, ok := readIntAny(args, "limit")
	if !ok || limit <= 0 {
		limit = 12
	}

	weekSet := intSliceToSet(readIntSliceAny(args, "week_filter", "weeks"))
	weekFrom, hasWeekFrom := readIntAny(args, "week_from", "from_week")
	weekTo, hasWeekTo := readIntAny(args, "week_to", "to_week")
	if week, hasWeek := readIntAny(args, "week"); hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	defaultWeekFrom, defaultWeekTo := inferWeekBounds(state)
	if !hasWeekFrom {
		weekFrom = defaultWeekFrom
	}
	if !hasWeekTo {
		weekTo = defaultWeekTo
	}

	excluded := intSliceToSet(readIntSliceAny(args, "exclude_sections", "exclude_section"))
	afterSection, hasAfter := readIntAny(args, "after_section")
	beforeSection, hasBefore := readIntAny(args, "before_section")
	exactFrom, hasExactFrom := readIntAny(args, "section_from", "target_section_from")
	exactTo, hasExactTo := readIntAny(args, "section_to", "target_section_to")
	if hasExactFrom != hasExactTo {
		return queryAvailableOptions{}, fmt.Errorf("精确节次查询需要同时提供 section_from 和 section_to")
	}
	if hasExactFrom {
		if exactFrom < 1 || exactTo > 12 || exactFrom > exactTo {
			return queryAvailableOptions{}, fmt.Errorf("精确节次区间非法：%d-%d", exactFrom, exactTo)
		}
		span = exactTo - exactFrom + 1
	}

	options := queryAvailableOptions{
		DayScope:        scope,
		DayOfWeekSet:    intSliceToSet(readIntSliceAny(args, "day_of_week", "days", "day_filter")),
		WeekSet:         weekSet,
		WeekFrom:        weekFrom,
		WeekTo:          weekTo,
		Span:            span,
		Limit:           limit,
		AllowEmbed:      allowEmbed,
		ExcludedSection: excluded,
	}
	if hasAfter {
		options.AfterSection = &afterSection
	}
	if hasBefore {
		options.BeforeSection = &beforeSection
	}
	if hasExactFrom {
		options.ExactFrom = &exactFrom
		options.ExactTo = &exactTo
	}
	return options, nil
}

// parseQueryTargetOptions 解析 query_target_tasks 参数。
func parseQueryTargetOptions(state *ScheduleState, args map[string]any) (queryTargetOptions, error) {
	scope := normalizeDayScope(readStringAny(args, "day_scope", "all"))
	status := strings.ToLower(strings.TrimSpace(readStringAny(args, "status", "suggested")))
	if status == "" {
		status = "suggested"
	}
	switch status {
	case "all", "existing", "suggested", "pending":
	default:
		return queryTargetOptions{}, fmt.Errorf("status=%q 非法，仅支持 all/existing/suggested/pending", status)
	}

	limit, ok := readIntAny(args, "limit")
	if !ok || limit <= 0 {
		limit = 16
	}

	weekSet := intSliceToSet(readIntSliceAny(args, "week_filter", "weeks"))
	weekFrom, hasWeekFrom := readIntAny(args, "week_from", "from_week")
	weekTo, hasWeekTo := readIntAny(args, "week_to", "to_week")
	if week, hasWeek := readIntAny(args, "week"); hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	defaultWeekFrom, defaultWeekTo := inferWeekBounds(state)
	if !hasWeekFrom {
		weekFrom = defaultWeekFrom
	}
	if !hasWeekTo {
		weekTo = defaultWeekTo
	}

	taskIDs := readIntSliceAny(args, "task_ids", "task_item_ids")
	if singleTaskID, ok := readIntAny(args, "task_id", "task_item_id"); ok {
		taskIDs = append(taskIDs, singleTaskID)
	}

	return queryTargetOptions{
		DayScope:     scope,
		DayOfWeekSet: intSliceToSet(readIntSliceAny(args, "day_of_week", "days", "day_filter")),
		WeekSet:      weekSet,
		WeekFrom:     weekFrom,
		WeekTo:       weekTo,
		Status:       status,
		Limit:        limit,
		TaskIDSet:    intSliceToSet(taskIDs),
		Category:     strings.TrimSpace(readStringAny(args, "category", "")),
		Enqueue:      readBoolAnyWithDefault(args, true, "enqueue"),
		ResetQueue:   readBoolAnyWithDefault(args, false, "reset_queue"),
	}, nil
}

// resolveCandidateDays 解析并返回候选 day 列表。
//
// 处理规则：
// 1. 先解析 day / day_start / day_end（互斥）形成基础集合；
// 2. 再叠加 day_scope / day_of_week / week_* 过滤；
// 3. 返回升序去重结果；若过滤后为空，返回空切片但不报错。
func resolveCandidateDays(
	state *ScheduleState,
	args map[string]any,
	dayScope string,
	dayOfWeekSet map[int]struct{},
	weekSet map[int]struct{},
	weekFrom int,
	weekTo int,
) ([]int, error) {
	if state == nil {
		return nil, fmt.Errorf("state 为空")
	}

	day, hasDay := readIntAny(args, "day")
	dayStart, hasDayStart := readIntAny(args, "day_start")
	dayEnd, hasDayEnd := readIntAny(args, "day_end")
	if hasDay && (hasDayStart || hasDayEnd) {
		return nil, fmt.Errorf("day 与 day_start/day_end 不能同时传入")
	}

	baseDays := make([]int, 0, state.Window.TotalDays)
	if hasDay {
		if err := validateDay(state, day); err != nil {
			return nil, err
		}
		baseDays = append(baseDays, day)
	} else {
		start := 1
		end := state.Window.TotalDays
		if hasDayStart {
			start = dayStart
		}
		if hasDayEnd {
			end = dayEnd
		}
		if start > end {
			return nil, fmt.Errorf("day_start=%d 不能大于 day_end=%d", start, end)
		}
		if err := validateDay(state, start); err != nil {
			return nil, err
		}
		if err := validateDay(state, end); err != nil {
			return nil, err
		}
		for d := start; d <= end; d++ {
			baseDays = append(baseDays, d)
		}
	}

	result := make([]int, 0, len(baseDays))
	for _, d := range baseDays {
		week, dayOfWeek, ok := state.DayToWeekDay(d)
		if !ok {
			continue
		}
		if len(dayOfWeekSet) > 0 {
			if _, hit := dayOfWeekSet[dayOfWeek]; !hit {
				continue
			}
		} else if !matchDayScope(dayOfWeek, dayScope) {
			continue
		}
		if len(weekSet) > 0 {
			if _, hit := weekSet[week]; !hit {
				continue
			}
		}
		if week < weekFrom || week > weekTo {
			continue
		}
		result = append(result, d)
	}
	sort.Ints(result)
	return uniqueInts(result), nil
}

// matchSectionRange 判断候选节次是否满足过滤条件。
func matchSectionRange(
	slotStart int,
	slotEnd int,
	excluded map[int]struct{},
	after *int,
	before *int,
	exactFrom *int,
	exactTo *int,
) bool {
	if exactFrom != nil && exactTo != nil {
		if slotStart != *exactFrom || slotEnd != *exactTo {
			return false
		}
	}
	if after != nil && slotStart <= *after {
		return false
	}
	if before != nil && slotEnd >= *before {
		return false
	}
	for section := slotStart; section <= slotEnd; section++ {
		if _, hit := excluded[section]; hit {
			return false
		}
	}
	return true
}

// isStrictSlotAvailable 判断某段是否为“纯空位”。
func isStrictSlotAvailable(state *ScheduleState, day int, slotStart int, slotEnd int) bool {
	for i := range state.Tasks {
		task := state.Tasks[i]
		if len(task.Slots) == 0 {
			continue
		}
		if task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			if rangesOverlap(slotStart, slotEnd, slot.SlotStart, slot.SlotEnd) {
				return false
			}
		}
	}
	return true
}

// isEmbeddableSlotAvailable 判断某段是否可作为“可嵌入候选位”。
//
// 判定规则：
// 1. 该段不能与不可嵌入任务冲突；
// 2. 该段必须完全落在某个 can_embed=true 且未被占用嵌入位的宿主中；
// 3. 若命中 can_embed 但宿主已被嵌入（embedded_by!=nil），视为不可用。
func isEmbeddableSlotAvailable(state *ScheduleState, day int, slotStart int, slotEnd int) bool {
	hostFound := false
	for i := range state.Tasks {
		task := state.Tasks[i]
		if len(task.Slots) == 0 {
			continue
		}
		if task.EmbedHost != nil {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != day {
				continue
			}
			if !rangesOverlap(slotStart, slotEnd, slot.SlotStart, slot.SlotEnd) {
				continue
			}

			if !task.CanEmbed {
				return false
			}
			if task.EmbeddedBy != nil {
				return false
			}
			if slotStart >= slot.SlotStart && slotEnd <= slot.SlotEnd {
				hostFound = true
				continue
			}
			// 与可嵌入宿主部分重叠但不被完全包含，也不能作为合法嵌入位。
			return false
		}
	}
	return hostFound
}

// matchTaskStatus 判断任务是否命中 status 过滤。
func matchTaskStatus(task ScheduleTask, status string) bool {
	switch status {
	case "all":
		return true
	case "existing":
		return IsExistingTask(task)
	case "suggested":
		return IsSuggestedTask(task)
	case "pending":
		return IsPendingTask(task)
	default:
		return false
	}
}

// isQueryTargetCalendarFilterActive 判断是否显式启用了日历坐标过滤。
func isQueryTargetCalendarFilterActive(args map[string]any, options queryTargetOptions) bool {
	if _, ok := readIntAny(args, "day"); ok {
		return true
	}
	if _, ok := readIntAny(args, "day_start"); ok {
		return true
	}
	if _, ok := readIntAny(args, "day_end"); ok {
		return true
	}
	if _, ok := readIntAny(args, "week"); ok {
		return true
	}
	if _, ok := readIntAny(args, "week_from", "from_week"); ok {
		return true
	}
	if _, ok := readIntAny(args, "week_to", "to_week"); ok {
		return true
	}
	if len(readIntSliceAny(args, "week_filter", "weeks")) > 0 {
		return true
	}
	if len(options.DayOfWeekSet) > 0 {
		return true
	}
	scopeRaw := strings.TrimSpace(readStringAny(args, "day_scope"))
	return normalizeDayScope(scopeRaw) != "all" && scopeRaw != ""
}

// buildTaskStatusLabel 返回任务状态标签。
func buildTaskStatusLabel(task ScheduleTask) string {
	if IsPendingTask(task) {
		return "pending"
	}
	if IsSuggestedTask(task) {
		return "suggested"
	}
	return "existing"
}

// rangesOverlap 判断两个闭区间是否重叠。
func rangesOverlap(startA, endA, startB, endB int) bool {
	return startA <= endB && endA >= startB
}

// normalizeDayScope 归一化 day_scope。
func normalizeDayScope(scope string) string {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case "weekend", "workday", "all":
		return scope
	default:
		return "all"
	}
}

// matchDayScope 判断 day_of_week 是否命中 day_scope。
func matchDayScope(dayOfWeek int, scope string) bool {
	switch scope {
	case "weekend":
		return dayOfWeek == 6 || dayOfWeek == 7
	case "workday":
		return dayOfWeek >= 1 && dayOfWeek <= 5
	default:
		return true
	}
}

// inferWeekBounds 推导窗口内的最小/最大周。
func inferWeekBounds(state *ScheduleState) (int, int) {
	if state == nil || len(state.Window.DayMapping) == 0 {
		return 0, 0
	}
	minWeek := state.Window.DayMapping[0].Week
	maxWeek := state.Window.DayMapping[0].Week
	for _, mapping := range state.Window.DayMapping {
		if mapping.Week < minWeek {
			minWeek = mapping.Week
		}
		if mapping.Week > maxWeek {
			maxWeek = mapping.Week
		}
	}
	return minWeek, maxWeek
}

// readIntAny 按别名顺序读取 int 参数。
func readIntAny(args map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		value, ok := argsInt(args, key)
		if ok {
			return value, true
		}
	}
	return 0, false
}

// readStringAny 按别名顺序读取 string 参数。
func readStringAny(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := argsString(args, key); ok {
			return value
		}
	}
	return ""
}

// readBoolAnyWithDefault 按别名顺序读取 bool 参数。
func readBoolAnyWithDefault(args map[string]any, defaultValue bool, keys ...string) bool {
	for _, key := range keys {
		raw, exists := args[key]
		if !exists {
			continue
		}
		switch value := raw.(type) {
		case bool:
			return value
		case string:
			lower := strings.ToLower(strings.TrimSpace(value))
			if lower == "true" {
				return true
			}
			if lower == "false" {
				return false
			}
		}
	}
	return defaultValue
}

// readIntSliceAny 按别名顺序读取 int 列表参数。
func readIntSliceAny(args map[string]any, keys ...string) []int {
	for _, key := range keys {
		if values, ok := argsIntSlice(args, key); ok {
			return values
		}
	}
	return nil
}

// readStringSliceAny 按别名顺序读取 string 列表参数。
func readStringSliceAny(args map[string]any, keys ...string) []string {
	for _, key := range keys {
		raw, exists := args[key]
		if !exists {
			continue
		}
		switch values := raw.(type) {
		case []string:
			out := make([]string, 0, len(values))
			for _, item := range values {
				trimmed := strings.TrimSpace(item)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		case []any:
			out := make([]string, 0, len(values))
			for _, item := range values {
				text, ok := item.(string)
				if !ok {
					continue
				}
				trimmed := strings.TrimSpace(text)
				if trimmed != "" {
					out = append(out, trimmed)
				}
			}
			return out
		case string:
			trimmed := strings.TrimSpace(values)
			if trimmed == "" {
				return nil
			}
			return []string{trimmed}
		}
	}
	return nil
}

// intSliceToSet 将 int 列表转为集合。
func intSliceToSet(values []int) map[int]struct{} {
	if len(values) == 0 {
		return map[int]struct{}{}
	}
	set := make(map[int]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

// sortedSetKeys 返回集合的升序 key 切片。
func sortedSetKeys(set map[int]struct{}) []int {
	if len(set) == 0 {
		return []int{}
	}
	keys := make([]int, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

// uniqueInts 对整数切片去重并保持升序。
func uniqueInts(values []int) []int {
	if len(values) == 0 {
		return values
	}
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Ints(result)
	return result
}
