package schedule

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	compositelogic "github.com/LoveLosita/smartflow/backend/logic"
)

var spreadEvenAllowedArgs = []string{
	"task_ids",
	"task_id",
	"limit",
	"allow_embed",
	"day",
	"day_start",
	"day_end",
	"day_scope",
	"day_of_week",
	"week",
	"week_filter",
	"week_from",
	"week_to",
	"slot_type",
	"slot_types",
	"exclude_sections",
	"after_section",
	"before_section",
}

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

// compositeIDMapper 负责维护 state_id 与 logic 规划入参 ID 的双向映射。
//
// 说明：
// 1. 当前阶段使用等值映射（logicID=stateID），保证行为不变；
// 2. 保留独立适配层，后续若切到真实 task_item_id，只需改这里；
// 3. 通过双向映射保证“入参转换 + 结果回填”一致。
type compositeIDMapper struct {
	stateToLogic map[int]int
	logicToState map[int]int
}

// buildCompositeIDMapper 构建并校验本轮复合工具的 ID 映射。
func buildCompositeIDMapper(stateIDs []int) (*compositeIDMapper, error) {
	mapper := &compositeIDMapper{
		stateToLogic: make(map[int]int, len(stateIDs)),
		logicToState: make(map[int]int, len(stateIDs)),
	}
	for _, stateID := range stateIDs {
		if stateID <= 0 {
			return nil, fmt.Errorf("存在非法 state_id=%d", stateID)
		}
		if _, exists := mapper.stateToLogic[stateID]; exists {
			return nil, fmt.Errorf("state_id=%d 重复", stateID)
		}
		// 当前迁移阶段采用等值映射，先把“映射机制”跑通。
		logicID := stateID
		mapper.stateToLogic[stateID] = logicID
		mapper.logicToState[logicID] = stateID
	}
	return mapper, nil
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
	plannerTasks, beforeByID, excludeIDs, idMapper, err := collectCompositePlannerTasks(state, taskIDs, "减少上下文切换")
	if err != nil {
		return err.Error()
	}
	logicTasks, err := toLogicPlannerTasks(plannerTasks, idMapper)
	if err != nil {
		return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
	}

	// 2. 该工具固定在“当前任务已占坑位集合”内重排，不向外扩张候选位。
	currentSlots := buildCurrentSlotsFromPlannerTasks(logicTasks)
	plannedMoves, err := compositelogic.PlanMinContextSwitchMoves(logicTasks, currentSlots, compositelogic.RefineCompositePlanOptions{})
	if err != nil {
		return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
	}

	// 3. 映射回工具态坐标并在提交前做完整校验。
	afterByID, err := buildAfterSnapshotsFromPlannedMoves(state, beforeByID, plannedMoves, idMapper)
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
	minContextProposals := make(map[int][]TaskSlot, len(afterByID))
	for taskID, after := range afterByID {
		minContextProposals[taskID] = []TaskSlot{after.Slot}
	}
	if err := validateLocalOrderBatchPlacement(state, minContextProposals); err != nil {
		return fmt.Sprintf("减少上下文切换失败：%s。", err.Error())
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
	// 0. 参数白名单校验：未知字段直接失败，避免静默忽略导致候选范围漂移。
	if err := validateToolArgsStrict(args, spreadEvenAllowedArgs); err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}

	// 1. 先做任务侧校验，避免后续规划在脏输入上执行。
	plannerTasks, beforeByID, excludeIDs, idMapper, err := collectCompositePlannerTasks(state, taskIDs, "均匀化调整")
	if err != nil {
		return err.Error()
	}
	logicTasks, err := toLogicPlannerTasks(plannerTasks, idMapper)
	if err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}

	// 2. 按跨度需求收集候选坑位，确保每类跨度都有可用池。
	spanNeed := make(map[int]int, len(logicTasks))
	for _, task := range logicTasks {
		spanNeed[task.SectionTo-task.SectionFrom+1]++
	}
	candidateSlots, err := collectSpreadEvenCandidateSlotsBySpan(state, args, spanNeed)
	if err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}

	// 3. 用“范围内既有负载”作为打分基线，让结果更接近均匀分布。
	dayLoadBaseline := buildSpreadEvenDayLoadBaseline(state, excludeIDs, candidateSlots)
	plannedMoves, err := compositelogic.PlanEvenSpreadMoves(logicTasks, candidateSlots, compositelogic.RefineCompositePlanOptions{
		ExistingDayLoad: dayLoadBaseline,
	})
	if err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
	}

	// 4. 回填 + 校验 + 原子提交。
	afterByID, err := buildAfterSnapshotsFromPlannedMoves(state, beforeByID, plannedMoves, idMapper)
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
	spreadEvenProposals := make(map[int][]TaskSlot, len(afterByID))
	for taskID, after := range afterByID {
		spreadEvenProposals[taskID] = []TaskSlot{after.Slot}
	}
	if err := validateLocalOrderBatchPlacement(state, spreadEvenProposals); err != nil {
		return fmt.Sprintf("均匀化调整失败：%s。", err.Error())
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

func ParseMinContextSwitchTaskIDs(args map[string]any) ([]int, error) {
	return ParseCompositeTaskIDs(args)
}

func ParseSpreadEvenTaskIDs(args map[string]any) ([]int, error) {
	return ParseCompositeTaskIDs(args)
}

func ParseCompositeTaskIDs(args map[string]any) ([]int, error) {
	if ids, ok := ArgsIntSlice(args, "task_ids"); ok && len(ids) > 0 {
		return ids, nil
	}
	if id, ok := ArgsInt(args, "task_id"); ok {
		return []int{id}, nil
	}
	return nil, fmt.Errorf("缺少必填参数 task_ids（兼容单值 task_id）")
}

// collectCompositePlannerTasks 统一收集复合工具输入任务，并做“可移动 suggested”校验。
func collectCompositePlannerTasks(
	state *ScheduleState,
	taskIDs []int,
	toolLabel string,
) ([]refineTaskCandidate, map[int]minContextSnapshot, []int, *compositeIDMapper, error) {
	normalizedIDs := uniquePositiveInts(taskIDs)
	if len(normalizedIDs) < 2 {
		return nil, nil, nil, nil, fmt.Errorf("%s失败：task_ids 至少需要 2 个有效任务 ID", toolLabel)
	}

	idMapper, err := buildCompositeIDMapper(normalizedIDs)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("%s失败：ID 映射构建失败：%s", toolLabel, err.Error())
	}

	plannerTasks := make([]refineTaskCandidate, 0, len(normalizedIDs))
	beforeByID := make(map[int]minContextSnapshot, len(normalizedIDs))
	excludeIDs := make([]int, 0, len(normalizedIDs))

	for rank, taskID := range normalizedIDs {
		task := state.TaskByStateID(taskID)
		if task == nil {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：任务ID %d 不存在", toolLabel, taskID)
		}
		if !IsSuggestedTask(*task) {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 不是 suggested 任务，仅 suggested 可参与该工具", toolLabel, task.StateID, task.Name)
		}
		if err := checkLocked(*task); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：%s", toolLabel, err.Error())
		}
		if len(task.Slots) != 1 {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 当前包含 %d 段时段，暂不支持该形态", toolLabel, task.StateID, task.Name, len(task.Slots))
		}

		slot := task.Slots[0]
		if err := validateDay(state, slot.Day); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 的时段非法：%s", toolLabel, task.StateID, task.Name, err.Error())
		}
		if err := validateSlotRange(slot.SlotStart, slot.SlotEnd); err != nil {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 的节次非法：%s", toolLabel, task.StateID, task.Name, err.Error())
		}
		week, dayOfWeek, ok := state.DayToWeekDay(slot.Day)
		if !ok {
			return nil, nil, nil, nil, fmt.Errorf("%s失败：[%d]%s 的 day=%d 无法映射到 week/day_of_week", toolLabel, task.StateID, task.Name, slot.Day)
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

	return plannerTasks, beforeByID, excludeIDs, idMapper, nil
}

// toLogicPlannerTasks 将工具层任务结构映射为 logic 规划器输入。
func toLogicPlannerTasks(tasks []refineTaskCandidate, idMapper *compositeIDMapper) ([]compositelogic.RefineTaskCandidate, error) {
	if len(tasks) == 0 {
		return nil, fmt.Errorf("任务列表为空")
	}
	if idMapper == nil {
		return nil, fmt.Errorf("ID 映射为空")
	}
	result := make([]compositelogic.RefineTaskCandidate, 0, len(tasks))
	for _, task := range tasks {
		logicID, ok := idMapper.stateToLogic[task.TaskID]
		if !ok {
			return nil, fmt.Errorf("任务 state_id=%d 缺少 logic 映射", task.TaskID)
		}
		result = append(result, compositelogic.RefineTaskCandidate{
			TaskItemID:  logicID,
			Week:        task.Week,
			DayOfWeek:   task.DayOfWeek,
			SectionFrom: task.SectionFrom,
			SectionTo:   task.SectionTo,
			Name:        task.Name,
			ContextTag:  task.ContextTag,
			OriginRank:  task.OriginRank,
		})
	}
	return result, nil
}

func buildCurrentSlotsFromPlannerTasks(tasks []compositelogic.RefineTaskCandidate) []compositelogic.RefineSlotCandidate {
	slots := make([]compositelogic.RefineSlotCandidate, 0, len(tasks))
	for _, task := range tasks {
		slots = append(slots, compositelogic.RefineSlotCandidate{
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
	plannedMoves []compositelogic.RefineMovePlanItem,
	idMapper *compositeIDMapper,
) (map[int]minContextSnapshot, error) {
	if len(plannedMoves) == 0 {
		return nil, fmt.Errorf("规划结果为空")
	}
	if idMapper == nil {
		return nil, fmt.Errorf("ID 映射为空")
	}

	moveByID := make(map[int]compositelogic.RefineMovePlanItem, len(plannedMoves))
	for _, move := range plannedMoves {
		stateID, ok := idMapper.logicToState[move.TaskItemID]
		if !ok {
			return nil, fmt.Errorf("规划结果包含未知 logic 任务 id=%d", move.TaskItemID)
		}
		if _, exists := moveByID[stateID]; exists {
			return nil, fmt.Errorf("规划结果包含重复任务 id=%d", stateID)
		}
		moveByID[stateID] = move
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
) ([]compositelogic.RefineSlotCandidate, error) {
	if len(spanNeed) == 0 {
		return nil, fmt.Errorf("未识别到任务跨度需求")
	}

	spans := make([]int, 0, len(spanNeed))
	for span := range spanNeed {
		spans = append(spans, span)
	}
	sort.Ints(spans)

	allSlots := make([]compositelogic.RefineSlotCandidate, 0, 16)
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
			allSlots = append(allSlots, compositelogic.RefineSlotCandidate{
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
	slots []compositelogic.RefineSlotCandidate,
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

func composeDayKey(week, day int) string {
	return fmt.Sprintf("%d-%d", week, day)
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
