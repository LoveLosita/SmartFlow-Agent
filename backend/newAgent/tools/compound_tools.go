package newagenttools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// minContextSnapshot 记录任务在复合重排前后的最小快照，用于输出摘要。
type minContextSnapshot struct {
	StateID    int
	Name       string
	ContextTag string
	Slot       TaskSlot
}

// refineTaskCandidate 是复合规划器使用的任务输入。
type refineTaskCandidate struct {
	TaskID      int
	Week        int
	DayOfWeek   int
	SectionFrom int
	SectionTo   int
	Name        string
	ContextTag  string
	OriginRank  int
}

// refineSlotCandidate 是复合规划器使用的候选坑位输入。
type refineSlotCandidate struct {
	Week        int
	DayOfWeek   int
	SectionFrom int
	SectionTo   int
}

// refineMovePlanItem 是规划器输出的一条移动方案。
type refineMovePlanItem struct {
	TaskID        int
	ToWeek        int
	ToDay         int
	ToSectionFrom int
	ToSectionTo   int
}

// refinePlanOptions 是复合规划器的可选参数。
type refinePlanOptions struct {
	ExistingDayLoad map[string]int
}

// MinContextSwitch 在给定任务集合内重排 suggested 任务，尽量减少上下文切换次数。
//
// 职责边界：
// 1. 只处理“已落位的 suggested 任务”重排，不负责粗排；
// 2. 仅在给定 task_ids 集合内部重排，不改动集合外任务；
// 3. 采用原子提交：任一校验失败则整体不生效。
func MinContextSwitch(state *ScheduleState, taskIDs []int) string {
	if state == nil {
		return "减少上下文切换失败：日程状态为空。"
	}

	// 1. 收集任务并做前置校验，确保规划输入可用。
	plannerTasks, beforeByID, excludeIDs, err := collectCompositePlannerTasks(state, taskIDs, "减少上下文切换")
	if err != nil {
		return err.Error()
	}

	// 2. 该工具固定在“当前任务已占坑位集合”内重排，不向外扩张候选位。
	currentSlots := buildCurrentSlotsFromPlannerTasks(plannerTasks)
	plannedMoves, err := planMinContextSwitchMoves(plannerTasks, currentSlots, refinePlanOptions{})
	if err != nil {
		return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
	}

	// 3. 映射回工具态坐标并在提交前做完整校验。
	afterByID, err := buildAfterSnapshotsFromPlannedMoves(state, beforeByID, plannedMoves)
	if err != nil {
		return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
	}
	for taskID, after := range afterByID {
		before := beforeByID[taskID]
		if err := validateDay(state, after.Slot.Day); err != nil {
			return fmt.Sprintf("减少上下文切换失败：任务 [%d]%s 目标天非法：%s。", before.StateID, before.Name, err.Error())
		}
		if err := validateSlotRange(after.Slot.SlotStart, after.Slot.SlotEnd); err != nil {
			return fmt.Sprintf("减少上下文切换失败：任务 [%d]%s 目标节次非法：%s。", before.StateID, before.Name, err.Error())
		}
		if conflict := findConflict(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd, excludeIDs...); conflict != nil {
			return fmt.Sprintf(
				"减少上下文切换失败：任务 [%d]%s 目标位置 %s 与 [%d]%s 冲突。",
				before.StateID,
				before.Name,
				formatDaySlotLabel(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd),
				conflict.StateID,
				conflict.Name,
			)
		}
	}

	// 4. 全量通过后再原子提交，避免半成品状态。
	clone := state.Clone()
	for taskID, after := range afterByID {
		task := clone.TaskByStateID(taskID)
		if task == nil {
			return fmt.Sprintf("减少上下文切换失败：任务ID %d 在提交阶段不存在。", taskID)
		}
		task.Slots = []TaskSlot{after.Slot}
	}
	state.Tasks = clone.Tasks

	beforeOrdered := sortMinContextSnapshots(beforeByID)
	afterOrdered := sortMinContextSnapshots(afterByID)
	beforeSwitches := countMinContextSwitches(beforeOrdered)
	afterSwitches := countMinContextSwitches(afterOrdered)

	changedLines := make([]string, 0, len(beforeOrdered))
	affectedDays := make(map[int]bool, len(beforeOrdered)*2)
	for _, before := range beforeOrdered {
		after := afterByID[before.StateID]
		if sameTaskSlot(before.Slot, after.Slot) {
			continue
		}
		changedLines = append(changedLines, fmt.Sprintf(
			"  [%d]%s：%s -> %s",
			before.StateID,
			before.Name,
			formatDaySlotLabel(state, before.Slot.Day, before.Slot.SlotStart, before.Slot.SlotEnd),
			formatDaySlotLabel(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd),
		))
		affectedDays[before.Slot.Day] = true
		affectedDays[after.Slot.Day] = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"最少上下文切换重排完成：共处理 %d 个任务，上下文切换次数 %d -> %d。\n",
		len(beforeByID), beforeSwitches, afterSwitches,
	))
	if len(changedLines) == 0 {
		sb.WriteString("当前任务顺序已是较优结果，无需调整。")
		return sb.String()
	}
	sb.WriteString("本次调整：\n")
	for _, line := range changedLines {
		sb.WriteString(line + "\n")
	}
	for _, day := range sortedKeys(affectedDays) {
		sb.WriteString(formatDayOccupancy(state, day) + "\n")
	}
	return strings.TrimSpace(sb.String())
}

// SpreadEven 在给定任务集合内执行“均匀化铺开”。
//
// 职责边界：
// 1. 仅处理 suggested 且已落位任务；
// 2. 先按筛选条件收集候选坑位，再调用确定性规划器；
// 3. 通过统一校验后原子提交，失败不落地。
func SpreadEven(state *ScheduleState, taskIDs []int, args map[string]any) string {
	if state == nil {
		return "均匀化调整失败：日程状态为空。"
	}

	// 1. 先做任务侧校验，避免后续规划在脏输入上执行。
	plannerTasks, beforeByID, excludeIDs, err := collectCompositePlannerTasks(state, taskIDs, "均匀化调整")
	if err != nil {
		return err.Error()
	}

	// 2. 按跨度需求收集候选坑位，确保每类跨度都有可用池。
	spanNeed := make(map[int]int, len(plannerTasks))
	for _, task := range plannerTasks {
		spanNeed[task.SectionTo-task.SectionFrom+1]++
	}
	candidateSlots, err := collectSpreadEvenCandidateSlotsBySpan(state, args, spanNeed)
	if err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}

	// 3. 用“范围内既有负载”作为打分基线，让结果更接近均匀分布。
	dayLoadBaseline := buildSpreadEvenDayLoadBaseline(state, excludeIDs, candidateSlots)
	plannedMoves, err := planEvenSpreadMoves(plannerTasks, candidateSlots, refinePlanOptions{
		ExistingDayLoad: dayLoadBaseline,
	})
	if err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}

	// 4. 回填 + 校验 + 原子提交。
	afterByID, err := buildAfterSnapshotsFromPlannedMoves(state, beforeByID, plannedMoves)
	if err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}
	for taskID, after := range afterByID {
		before := beforeByID[taskID]
		if err := validateDay(state, after.Slot.Day); err != nil {
			return fmt.Sprintf("均匀化调整失败：任务 [%d]%s 目标天非法：%s。", before.StateID, before.Name, err.Error())
		}
		if err := validateSlotRange(after.Slot.SlotStart, after.Slot.SlotEnd); err != nil {
			return fmt.Sprintf("均匀化调整失败：任务 [%d]%s 目标节次非法：%s。", before.StateID, before.Name, err.Error())
		}
		if conflict := findConflict(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd, excludeIDs...); conflict != nil {
			return fmt.Sprintf(
				"均匀化调整失败：任务 [%d]%s 目标位置 %s 与 [%d]%s 冲突。",
				before.StateID,
				before.Name,
				formatDaySlotLabel(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd),
				conflict.StateID,
				conflict.Name,
			)
		}
	}

	clone := state.Clone()
	for taskID, after := range afterByID {
		task := clone.TaskByStateID(taskID)
		if task == nil {
			return fmt.Sprintf("均匀化调整失败：任务ID %d 在提交阶段不存在。", taskID)
		}
		task.Slots = []TaskSlot{after.Slot}
	}
	state.Tasks = clone.Tasks

	beforeOrdered := sortMinContextSnapshots(beforeByID)
	changedLines := make([]string, 0, len(beforeOrdered))
	affectedDays := make(map[int]bool, len(beforeOrdered)*2)
	for _, before := range beforeOrdered {
		after := afterByID[before.StateID]
		if sameTaskSlot(before.Slot, after.Slot) {
			continue
		}
		changedLines = append(changedLines, fmt.Sprintf(
			"  [%d]%s：%s -> %s",
			before.StateID,
			before.Name,
			formatDaySlotLabel(state, before.Slot.Day, before.Slot.SlotStart, before.Slot.SlotEnd),
			formatDaySlotLabel(state, after.Slot.Day, after.Slot.SlotStart, after.Slot.SlotEnd),
		))
		affectedDays[before.Slot.Day] = true
		affectedDays[after.Slot.Day] = true
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"均匀化调整完成：共处理 %d 个任务，候选坑位 %d 个。\n",
		len(beforeByID), len(candidateSlots),
	))
	if len(changedLines) == 0 {
		sb.WriteString("规划结果与当前落位一致，无需调整。")
		return sb.String()
	}
	sb.WriteString("本次调整：\n")
	for _, line := range changedLines {
		sb.WriteString(line + "\n")
	}
	for _, day := range sortedKeys(affectedDays) {
		sb.WriteString(formatDayOccupancy(state, day) + "\n")
	}
	return strings.TrimSpace(sb.String())
}

func parseMinContextSwitchTaskIDs(args map[string]any) ([]int, error) {
	return parseCompositeTaskIDs(args)
}

func parseSpreadEvenTaskIDs(args map[string]any) ([]int, error) {
	return parseCompositeTaskIDs(args)
}

func parseCompositeTaskIDs(args map[string]any) ([]int, error) {
	if ids, ok := argsIntSlice(args, "task_ids"); ok && len(ids) > 0 {
		return ids, nil
	}
	if id, ok := argsInt(args, "task_id"); ok {
		return []int{id}, nil
	}
	return nil, fmt.Errorf("缺少必填参数 task_ids（兼容单值 task_id）")
}

// collectCompositePlannerTasks 统一收集复合工具输入任务，并做“可移动 suggested”校验。
func collectCompositePlannerTasks(
	state *ScheduleState,
	taskIDs []int,
	toolLabel string,
) ([]refineTaskCandidate, map[int]minContextSnapshot, []int, error) {
	normalizedIDs := uniquePositiveInts(taskIDs)
	if len(normalizedIDs) < 2 {
		return nil, nil, nil, fmt.Errorf("%s失败：task_ids 至少需要 2 个有效任务 ID", toolLabel)
	}

	plannerTasks := make([]refineTaskCandidate, 0, len(normalizedIDs))
	beforeByID := make(map[int]minContextSnapshot, len(normalizedIDs))
	excludeIDs := make([]int, 0, len(normalizedIDs))

	for rank, taskID := range normalizedIDs {
		task := state.TaskByStateID(taskID)
		if task == nil {
			return nil, nil, nil, fmt.Errorf("%s失败：任务ID %d 不存在", toolLabel, taskID)
		}
		if !IsSuggestedTask(*task) {
			return nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 不是 suggested 任务，仅 suggested 可参与该工具", toolLabel, task.StateID, task.Name)
		}
		if err := checkLocked(*task); err != nil {
			return nil, nil, nil, fmt.Errorf("%s失败：%s", toolLabel, err.Error())
		}
		if len(task.Slots) != 1 {
			return nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 当前包含 %d 段时段，暂不支持该形态", toolLabel, task.StateID, task.Name, len(task.Slots))
		}

		slot := task.Slots[0]
		if err := validateDay(state, slot.Day); err != nil {
			return nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 的时段非法：%s", toolLabel, task.StateID, task.Name, err.Error())
		}
		if err := validateSlotRange(slot.SlotStart, slot.SlotEnd); err != nil {
			return nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 的节次非法：%s", toolLabel, task.StateID, task.Name, err.Error())
		}
		week, dayOfWeek, ok := state.DayToWeekDay(slot.Day)
		if !ok {
			return nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 的 day=%d 无法映射到 week/day_of_week", toolLabel, task.StateID, task.Name, slot.Day)
		}

		contextTag := normalizeMinContextTag(*task)
		beforeByID[task.StateID] = minContextSnapshot{
			StateID:    task.StateID,
			Name:       task.Name,
			ContextTag: contextTag,
			Slot:       slot,
		}
		excludeIDs = append(excludeIDs, task.StateID)
		plannerTasks = append(plannerTasks, refineTaskCandidate{
			TaskID:      task.StateID,
			Week:        week,
			DayOfWeek:   dayOfWeek,
			SectionFrom: slot.SlotStart,
			SectionTo:   slot.SlotEnd,
			Name:        strings.TrimSpace(task.Name),
			ContextTag:  contextTag,
			OriginRank:  rank + 1,
		})
	}

	return plannerTasks, beforeByID, excludeIDs, nil
}

func buildCurrentSlotsFromPlannerTasks(tasks []refineTaskCandidate) []refineSlotCandidate {
	slots := make([]refineSlotCandidate, 0, len(tasks))
	for _, task := range tasks {
		slots = append(slots, refineSlotCandidate{
			Week:        task.Week,
			DayOfWeek:   task.DayOfWeek,
			SectionFrom: task.SectionFrom,
			SectionTo:   task.SectionTo,
		})
	}
	return slots
}

func buildAfterSnapshotsFromPlannedMoves(
	state *ScheduleState,
	beforeByID map[int]minContextSnapshot,
	plannedMoves []refineMovePlanItem,
) (map[int]minContextSnapshot, error) {
	if len(plannedMoves) == 0 {
		return nil, fmt.Errorf("规划结果为空")
	}

	moveByID := make(map[int]refineMovePlanItem, len(plannedMoves))
	for _, move := range plannedMoves {
		if _, exists := moveByID[move.TaskID]; exists {
			return nil, fmt.Errorf("规划结果包含重复任务 id=%d", move.TaskID)
		}
		moveByID[move.TaskID] = move
	}

	afterByID := make(map[int]minContextSnapshot, len(beforeByID))
	for taskID, before := range beforeByID {
		move, ok := moveByID[taskID]
		if !ok {
			return nil, fmt.Errorf("规划结果不完整：缺少任务 id=%d", taskID)
		}
		day, ok := state.WeekDayToDay(move.ToWeek, move.ToDay)
		if !ok {
			return nil, fmt.Errorf("任务 id=%d 目标 week/day 无法映射到 day_index：W%dD%d", taskID, move.ToWeek, move.ToDay)
		}
		afterByID[taskID] = minContextSnapshot{
			StateID:    before.StateID,
			Name:       before.Name,
			ContextTag: before.ContextTag,
			Slot: TaskSlot{
				Day:       day,
				SlotStart: move.ToSectionFrom,
				SlotEnd:   move.ToSectionTo,
			},
		}
	}
	return afterByID, nil
}

func collectSpreadEvenCandidateSlotsBySpan(
	state *ScheduleState,
	args map[string]any,
	spanNeed map[int]int,
) ([]refineSlotCandidate, error) {
	if len(spanNeed) == 0 {
		return nil, fmt.Errorf("未识别到任务跨度需求")
	}

	spans := make([]int, 0, len(spanNeed))
	for span := range spanNeed {
		spans = append(spans, span)
	}
	sort.Ints(spans)

	allSlots := make([]refineSlotCandidate, 0, 16)
	seen := make(map[string]struct{}, 64)
	for _, span := range spans {
		required := spanNeed[span]
		queryArgs := buildSpreadEvenSlotQueryArgs(args, span, required)
		raw := QueryAvailableSlots(state, queryArgs)

		var failed struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal([]byte(raw), &failed)
		if strings.TrimSpace(failed.Error) != "" {
			return nil, fmt.Errorf("查询跨度=%d 的候选坑位失败：%s", span, strings.TrimSpace(failed.Error))
		}

		var payload queryAvailableSlotsResult
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return nil, fmt.Errorf("解析跨度=%d 的候选坑位结果失败：%v", span, err)
		}
		if len(payload.Slots) < required {
			return nil, fmt.Errorf("跨度=%d 可用坑位不足：required=%d, got=%d", span, required, len(payload.Slots))
		}

		for _, slot := range payload.Slots {
			key := fmt.Sprintf("%d-%d-%d-%d", slot.Week, slot.DayOfWeek, slot.SlotStart, slot.SlotEnd)
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			allSlots = append(allSlots, refineSlotCandidate{
				Week:        slot.Week,
				DayOfWeek:   slot.DayOfWeek,
				SectionFrom: slot.SlotStart,
				SectionTo:   slot.SlotEnd,
			})
		}
	}
	return allSlots, nil
}

func buildSpreadEvenSlotQueryArgs(args map[string]any, span int, required int) map[string]any {
	query := make(map[string]any, 16)
	query["span"] = span

	limit := required * 6
	if limit < required {
		limit = required
	}
	if customLimit, ok := readIntAny(args, "limit"); ok && customLimit > limit {
		limit = customLimit
	}
	query["limit"] = limit
	query["allow_embed"] = readBoolAnyWithDefault(args, true, "allow_embed", "allow_embedding")

	for _, key := range []string{"day", "day_start", "day_end", "week", "week_from", "week_to", "day_scope", "after_section", "before_section"} {
		if value, ok := args[key]; ok {
			query[key] = value
		}
	}
	if week, ok := readIntAny(args, "to_week", "target_week", "new_week"); ok {
		query["week"] = week
	}
	if day, ok := readIntAny(args, "to_day", "target_day", "target_day_of_week", "new_day"); ok {
		query["day_of_week"] = []int{day}
	}

	if values := uniquePositiveInts(readIntSliceAny(args, "week_filter", "weeks")); len(values) > 0 {
		query["week_filter"] = values
	}
	if values := uniqueInts(readIntSliceAny(args, "day_of_week", "days", "day_filter")); len(values) > 0 {
		query["day_of_week"] = values
	}
	if values := uniqueInts(readIntSliceAny(args, "exclude_sections", "exclude_section")); len(values) > 0 {
		query["exclude_sections"] = values
	}

	return query
}

func buildSpreadEvenDayLoadBaseline(
	state *ScheduleState,
	excludeTaskIDs []int,
	slots []refineSlotCandidate,
) map[string]int {
	if len(slots) == 0 {
		return nil
	}

	targetDays := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		targetDays[composeDayKey(slot.Week, slot.DayOfWeek)] = struct{}{}
	}
	if len(targetDays) == 0 {
		return nil
	}

	excludeSet := make(map[int]struct{}, len(excludeTaskIDs))
	for _, id := range excludeTaskIDs {
		excludeSet[id] = struct{}{}
	}

	load := make(map[string]int, len(targetDays))
	for _, task := range state.Tasks {
		if !IsSuggestedTask(task) {
			continue
		}
		if _, excluded := excludeSet[task.StateID]; excluded {
			continue
		}
		for _, slot := range task.Slots {
			week, dayOfWeek, ok := state.DayToWeekDay(slot.Day)
			if !ok {
				continue
			}
			key := composeDayKey(week, dayOfWeek)
			if _, inTarget := targetDays[key]; !inTarget {
				continue
			}
			load[key]++
		}
	}
	return load
}

func planEvenSpreadMoves(tasks []refineTaskCandidate, slots []refineSlotCandidate, options refinePlanOptions) ([]refineMovePlanItem, error) {
	normalizedTasks, err := normalizePlannerTasks(tasks)
	if err != nil {
		return nil, err
	}
	normalizedSlots, err := normalizePlannerSlots(slots)
	if err != nil {
		return nil, err
	}
	if len(normalizedSlots) < len(normalizedTasks) {
		return nil, fmt.Errorf("可用坑位不足：tasks=%d, slots=%d", len(normalizedTasks), len(normalizedSlots))
	}

	dayLoad := make(map[string]int, len(options.ExistingDayLoad)+len(normalizedSlots))
	for key, value := range options.ExistingDayLoad {
		if value <= 0 {
			continue
		}
		dayLoad[strings.TrimSpace(key)] = value
	}

	used := make([]bool, len(normalizedSlots))
	moves := make([]refineMovePlanItem, 0, len(normalizedTasks))
	selectedSlots := make([]refineSlotCandidate, 0, len(normalizedTasks))

	for _, task := range normalizedTasks {
		taskSpan := sectionSpan(task.SectionFrom, task.SectionTo)
		bestIdx := -1
		bestScore := int(^uint(0) >> 1)

		for idx, slot := range normalizedSlots {
			if used[idx] {
				continue
			}
			if sectionSpan(slot.SectionFrom, slot.SectionTo) != taskSpan {
				continue
			}
			if slotOverlapsAny(slot, selectedSlots) {
				continue
			}
			dayKey := composeDayKey(slot.Week, slot.DayOfWeek)
			projectedLoad := dayLoad[dayKey] + 1
			score := projectedLoad*10000 + idx
			if score < bestScore {
				bestScore = score
				bestIdx = idx
			}
		}
		if bestIdx < 0 {
			return nil, fmt.Errorf("任务 id=%d 无可用同跨度坑位", task.TaskID)
		}

		chosen := normalizedSlots[bestIdx]
		used[bestIdx] = true
		selectedSlots = append(selectedSlots, chosen)
		dayLoad[composeDayKey(chosen.Week, chosen.DayOfWeek)]++
		moves = append(moves, refineMovePlanItem{
			TaskID:        task.TaskID,
			ToWeek:        chosen.Week,
			ToDay:         chosen.DayOfWeek,
			ToSectionFrom: chosen.SectionFrom,
			ToSectionTo:   chosen.SectionTo,
		})
	}
	return moves, nil
}

func planMinContextSwitchMoves(tasks []refineTaskCandidate, slots []refineSlotCandidate, _ refinePlanOptions) ([]refineMovePlanItem, error) {
	normalizedTasks, err := normalizePlannerTasks(tasks)
	if err != nil {
		return nil, err
	}
	normalizedSlots, err := normalizePlannerSlots(slots)
	if err != nil {
		return nil, err
	}
	if len(normalizedSlots) < len(normalizedTasks) {
		return nil, fmt.Errorf("可用坑位不足：tasks=%d, slots=%d", len(normalizedTasks), len(normalizedSlots))
	}

	type taskGroup struct {
		ContextKey string
		Tasks      []refineTaskCandidate
		MinRank    int
	}

	groupingKeys := buildMinContextGroupingKeys(normalizedTasks)
	groupMap := make(map[string]*taskGroup, len(normalizedTasks))
	groupOrder := make([]string, 0, len(normalizedTasks))
	for _, task := range normalizedTasks {
		key := groupingKeys[task.TaskID]
		group, exists := groupMap[key]
		if !exists {
			group = &taskGroup{
				ContextKey: key,
				MinRank:    normalizedOriginRank(task),
			}
			groupMap[key] = group
			groupOrder = append(groupOrder, key)
		}
		group.Tasks = append(group.Tasks, task)
		if rank := normalizedOriginRank(task); rank < group.MinRank {
			group.MinRank = rank
		}
	}

	groups := make([]taskGroup, 0, len(groupMap))
	for _, key := range groupOrder {
		group := groupMap[key]
		sort.SliceStable(group.Tasks, func(i, j int) bool {
			return compareTaskOrder(group.Tasks[i], group.Tasks[j]) < 0
		})
		groups = append(groups, *group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if len(groups[i].Tasks) != len(groups[j].Tasks) {
			return len(groups[i].Tasks) > len(groups[j].Tasks)
		}
		if groups[i].MinRank != groups[j].MinRank {
			return groups[i].MinRank < groups[j].MinRank
		}
		return groups[i].ContextKey < groups[j].ContextKey
	})

	orderedTasks := make([]refineTaskCandidate, 0, len(normalizedTasks))
	for _, group := range groups {
		orderedTasks = append(orderedTasks, group.Tasks...)
	}

	used := make([]bool, len(normalizedSlots))
	selectedSlots := make([]refineSlotCandidate, 0, len(orderedTasks))
	moves := make([]refineMovePlanItem, 0, len(orderedTasks))
	for _, task := range orderedTasks {
		span := sectionSpan(task.SectionFrom, task.SectionTo)
		chosenIdx := -1
		for idx, slot := range normalizedSlots {
			if used[idx] {
				continue
			}
			if sectionSpan(slot.SectionFrom, slot.SectionTo) != span {
				continue
			}
			if slotOverlapsAny(slot, selectedSlots) {
				continue
			}
			chosenIdx = idx
			break
		}
		if chosenIdx < 0 {
			return nil, fmt.Errorf("任务 id=%d 无可用同跨度坑位", task.TaskID)
		}
		chosen := normalizedSlots[chosenIdx]
		used[chosenIdx] = true
		selectedSlots = append(selectedSlots, chosen)
		moves = append(moves, refineMovePlanItem{
			TaskID:        task.TaskID,
			ToWeek:        chosen.Week,
			ToDay:         chosen.DayOfWeek,
			ToSectionFrom: chosen.SectionFrom,
			ToSectionTo:   chosen.SectionTo,
		})
	}
	return moves, nil
}

func normalizePlannerTasks(tasks []refineTaskCandidate) ([]refineTaskCandidate, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("任务列表为空")
	}
	normalized := make([]refineTaskCandidate, 0, len(tasks))
	seen := make(map[int]struct{}, len(tasks))
	for _, task := range tasks {
		if task.TaskID <= 0 {
			return nil, fmt.Errorf("存在非法 task_id=%d", task.TaskID)
		}
		if _, exists := seen[task.TaskID]; exists {
			return nil, fmt.Errorf("任务 id=%d 重复", task.TaskID)
		}
		if !isValidDay(task.DayOfWeek) {
			return nil, fmt.Errorf("任务 id=%d day_of_week 非法=%d", task.TaskID, task.DayOfWeek)
		}
		if !isValidSection(task.SectionFrom, task.SectionTo) {
			return nil, fmt.Errorf("任务 id=%d 节次区间非法=%d-%d", task.TaskID, task.SectionFrom, task.SectionTo)
		}
		seen[task.TaskID] = struct{}{}
		normalized = append(normalized, task)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		return compareTaskOrder(normalized[i], normalized[j]) < 0
	})
	return normalized, nil
}

func normalizePlannerSlots(slots []refineSlotCandidate) ([]refineSlotCandidate, error) {
	if len(slots) == 0 {
		return nil, fmt.Errorf("可用坑位为空")
	}
	normalized := make([]refineSlotCandidate, 0, len(slots))
	seen := make(map[string]struct{}, len(slots))
	for _, slot := range slots {
		if slot.Week <= 0 {
			return nil, fmt.Errorf("存在非法 week=%d", slot.Week)
		}
		if !isValidDay(slot.DayOfWeek) {
			return nil, fmt.Errorf("存在非法 day_of_week=%d", slot.DayOfWeek)
		}
		if !isValidSection(slot.SectionFrom, slot.SectionTo) {
			return nil, fmt.Errorf("存在非法节次区间=%d-%d", slot.SectionFrom, slot.SectionTo)
		}
		key := fmt.Sprintf("%d-%d-%d-%d", slot.Week, slot.DayOfWeek, slot.SectionFrom, slot.SectionTo)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, slot)
	}
	sort.SliceStable(normalized, func(i, j int) bool {
		if normalized[i].Week != normalized[j].Week {
			return normalized[i].Week < normalized[j].Week
		}
		if normalized[i].DayOfWeek != normalized[j].DayOfWeek {
			return normalized[i].DayOfWeek < normalized[j].DayOfWeek
		}
		if normalized[i].SectionFrom != normalized[j].SectionFrom {
			return normalized[i].SectionFrom < normalized[j].SectionFrom
		}
		return normalized[i].SectionTo < normalized[j].SectionTo
	})
	return normalized, nil
}

func compareTaskOrder(a, b refineTaskCandidate) int {
	rankA := normalizedOriginRank(a)
	rankB := normalizedOriginRank(b)
	if rankA != rankB {
		return rankA - rankB
	}
	if a.Week != b.Week {
		return a.Week - b.Week
	}
	if a.DayOfWeek != b.DayOfWeek {
		return a.DayOfWeek - b.DayOfWeek
	}
	if a.SectionFrom != b.SectionFrom {
		return a.SectionFrom - b.SectionFrom
	}
	if a.SectionTo != b.SectionTo {
		return a.SectionTo - b.SectionTo
	}
	return a.TaskID - b.TaskID
}

func normalizedOriginRank(task refineTaskCandidate) int {
	if task.OriginRank > 0 {
		return task.OriginRank
	}
	return 1_000_000 + task.TaskID
}

func buildMinContextGroupingKeys(tasks []refineTaskCandidate) map[int]string {
	keys := make(map[int]string, len(tasks))
	distinctExplicit := make(map[string]struct{}, len(tasks))
	distinctNonCoarse := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		key := normalizeContextKey(task.ContextTag)
		keys[task.TaskID] = key
		distinctExplicit[key] = struct{}{}
		if !isCoarseContextKey(key) {
			distinctNonCoarse[key] = struct{}{}
		}
	}

	// 1. 显式标签已经足够区分时，直接沿用；
	// 2. 仅在显式标签退化到粗粒度时，才尝试名称兜底。
	if len(distinctNonCoarse) >= 2 {
		return keys
	}
	if len(distinctExplicit) > 1 && len(distinctNonCoarse) > 0 {
		return keys
	}

	inferredKeys := make(map[int]string, len(tasks))
	distinctInferred := make(map[string]struct{}, len(tasks))
	for _, task := range tasks {
		inferred := inferSubjectContextKeyFromTaskName(task.Name)
		if inferred == "" {
			inferred = keys[task.TaskID]
		}
		inferredKeys[task.TaskID] = inferred
		distinctInferred[inferred] = struct{}{}
	}
	if len(distinctInferred) >= 2 {
		return inferredKeys
	}
	return keys
}

func normalizeContextKey(tag string) string {
	text := strings.TrimSpace(tag)
	if text == "" {
		return "General"
	}
	return text
}

func isCoarseContextKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "", "general", "high-logic", "high_logic", "memory", "review":
		return true
	default:
		return false
	}
}

func inferSubjectContextKeyFromTaskName(name string) string {
	text := strings.ToLower(strings.TrimSpace(name))
	if text == "" {
		return ""
	}
	// 1. 这里使用轻量关键词，不追求全学科覆盖；
	// 2. 仅用于“显式标签不足”的兜底场景。
	switch {
	case strings.Contains(text, "概率"), strings.Contains(text, "随机变量"), strings.Contains(text, "贝叶斯"), strings.Contains(text, "分布"):
		return "subject:probability"
	case strings.Contains(text, "数制"), strings.Contains(text, "逻辑代数"), strings.Contains(text, "时序电路"), strings.Contains(text, "状态图"):
		return "subject:digital_logic"
	case strings.Contains(text, "离散"), strings.Contains(text, "图论"), strings.Contains(text, "集合"), strings.Contains(text, "命题逻辑"):
		return "subject:discrete_math"
	default:
		return ""
	}
}

func slotOverlapsAny(candidate refineSlotCandidate, selected []refineSlotCandidate) bool {
	for _, current := range selected {
		if current.Week != candidate.Week || current.DayOfWeek != candidate.DayOfWeek {
			continue
		}
		if current.SectionFrom <= candidate.SectionTo && candidate.SectionFrom <= current.SectionTo {
			return true
		}
	}
	return false
}

func composeDayKey(week, day int) string {
	return fmt.Sprintf("%d-%d", week, day)
}

func sectionSpan(from, to int) int {
	return to - from + 1
}

func isValidDay(day int) bool {
	return day >= 1 && day <= 7
}

func isValidSection(from, to int) bool {
	if from < 1 || to > 12 {
		return false
	}
	return from <= to
}

func uniquePositiveInts(values []int) []int {
	seen := make(map[int]struct{}, len(values))
	result := make([]int, 0, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func normalizeMinContextTag(task ScheduleTask) string {
	if tag := strings.TrimSpace(task.Category); tag != "" {
		return tag
	}
	if tag := strings.TrimSpace(task.Name); tag != "" {
		return tag
	}
	return "General"
}

func sortMinContextSnapshots(snapshotByID map[int]minContextSnapshot) []minContextSnapshot {
	items := make([]minContextSnapshot, 0, len(snapshotByID))
	for _, item := range snapshotByID {
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Slot.Day != items[j].Slot.Day {
			return items[i].Slot.Day < items[j].Slot.Day
		}
		if items[i].Slot.SlotStart != items[j].Slot.SlotStart {
			return items[i].Slot.SlotStart < items[j].Slot.SlotStart
		}
		if items[i].Slot.SlotEnd != items[j].Slot.SlotEnd {
			return items[i].Slot.SlotEnd < items[j].Slot.SlotEnd
		}
		return items[i].StateID < items[j].StateID
	})
	return items
}

func countMinContextSwitches(ordered []minContextSnapshot) int {
	if len(ordered) < 2 {
		return 0
	}
	switches := 0
	prevTag := strings.TrimSpace(ordered[0].ContextTag)
	for i := 1; i < len(ordered); i++ {
		currentTag := strings.TrimSpace(ordered[i].ContextTag)
		if currentTag != prevTag {
			switches++
		}
		prevTag = currentTag
	}
	return switches
}

func sameTaskSlot(a, b TaskSlot) bool {
	return a.Day == b.Day && a.SlotStart == b.SlotStart && a.SlotEnd == b.SlotEnd
}
