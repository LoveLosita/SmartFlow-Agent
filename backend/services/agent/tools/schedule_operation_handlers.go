package agenttools

import (
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
)

type scheduleTaskSnapshot struct {
	Exists  bool
	TaskID  int
	Name    string
	Status  string
	Slots   []schedule.TaskSlot
	DayInfo map[int]schedule.DayMapping
}

type scheduleQueueSnapshot struct {
	PendingCount   int
	CompletedCount int
	SkippedCount   int
	CurrentTaskID  int
	CurrentAttempt int
	LastError      string
}

// NewPlaceToolHandler 返回 place 的结构化结果 handler（第一轮真实 result_view）。
func NewPlaceToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		taskID, ok := schedule.ArgsInt(args, "task_id")
		if !ok {
			return buildScheduleArgErrorResult("place", args, "缺少必填参数 task_id。", state)
		}
		day, ok := schedule.ArgsInt(args, "day")
		if !ok {
			return buildScheduleArgErrorResult("place", args, "缺少必填参数 day。", state)
		}
		slotStart, ok := schedule.ArgsInt(args, "slot_start")
		if !ok {
			return buildScheduleArgErrorResult("place", args, "缺少必填参数 slot_start。", state)
		}
		if state == nil {
			return buildScheduleArgErrorResult("place", args, "日程状态为空，无法执行预排。", nil)
		}

		beforeState := state.Clone()
		observation := schedule.Place(state, taskID, day, slotStart)
		afterState := state.Clone()

		before := snapshotTask(beforeState, taskID)
		after := snapshotTask(afterState, taskID)
		success := after.Exists && taskHasSlotAt(after, day, slotStart) && !sameSlots(before.Slots, after.Slots)

		changes := []map[string]any{
			buildTaskChange("place", before, after),
		}
		affectedDays := collectAffectedDays(changes)
		return buildScheduleOperationResult("place", args, afterState, observation, success, affectedDays, changes, nil, "")
	}
}

// NewMoveToolHandler 返回 move 的结构化结果 handler（第一轮真实 result_view）。
func NewMoveToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		taskID, ok := schedule.ArgsInt(args, "task_id")
		if !ok {
			return buildScheduleArgErrorResult("move", args, "缺少必填参数 task_id。", state)
		}
		newDay, ok := schedule.ArgsInt(args, "new_day")
		if !ok {
			return buildScheduleArgErrorResult("move", args, "缺少必填参数 new_day。", state)
		}
		newSlotStart, ok := schedule.ArgsInt(args, "new_slot_start")
		if !ok {
			return buildScheduleArgErrorResult("move", args, "缺少必填参数 new_slot_start。", state)
		}
		if state == nil {
			return buildScheduleArgErrorResult("move", args, "日程状态为空，无法执行移动。", nil)
		}

		beforeState := state.Clone()
		observation := schedule.Move(state, taskID, newDay, newSlotStart)
		afterState := state.Clone()

		before := snapshotTask(beforeState, taskID)
		after := snapshotTask(afterState, taskID)
		success := before.Exists && after.Exists && taskHasSlotAt(after, newDay, newSlotStart) && !sameSlots(before.Slots, after.Slots)

		changes := []map[string]any{
			buildTaskChange("move", before, after),
		}
		affectedDays := collectAffectedDays(changes)
		return buildScheduleOperationResult("move", args, afterState, observation, success, affectedDays, changes, nil, "")
	}
}

// NewSwapToolHandler 返回 swap 的结构化结果 handler（第一轮真实 result_view）。
func NewSwapToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		taskA, ok := schedule.ArgsInt(args, "task_a")
		if !ok {
			return buildScheduleArgErrorResult("swap", args, "缺少必填参数 task_a。", state)
		}
		taskB, ok := schedule.ArgsInt(args, "task_b")
		if !ok {
			return buildScheduleArgErrorResult("swap", args, "缺少必填参数 task_b。", state)
		}
		if state == nil {
			return buildScheduleArgErrorResult("swap", args, "日程状态为空，无法执行交换。", nil)
		}

		beforeState := state.Clone()
		observation := schedule.Swap(state, taskA, taskB)
		afterState := state.Clone()

		beforeA := snapshotTask(beforeState, taskA)
		afterA := snapshotTask(afterState, taskA)
		beforeB := snapshotTask(beforeState, taskB)
		afterB := snapshotTask(afterState, taskB)

		success := beforeA.Exists &&
			beforeB.Exists &&
			afterA.Exists &&
			afterB.Exists &&
			sameSlots(beforeA.Slots, afterB.Slots) &&
			sameSlots(beforeB.Slots, afterA.Slots)

		changes := []map[string]any{
			buildTaskChange("swap", beforeA, afterA),
			buildTaskChange("swap", beforeB, afterB),
		}
		affectedDays := collectAffectedDays(changes)
		return buildScheduleOperationResult("swap", args, afterState, observation, success, affectedDays, changes, nil, "")
	}
}

// NewBatchMoveToolHandler 返回 batch_move 的结构化结果 handler（第一轮真实 result_view）。
func NewBatchMoveToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		if state == nil {
			return buildScheduleArgErrorResult("batch_move", args, "日程状态为空，无法执行批量移动。", nil)
		}
		moves, err := schedule.ArgsMoveList(args)
		if err != nil {
			return buildScheduleArgErrorResult("batch_move", args, err.Error(), state)
		}

		beforeState := state.Clone()
		observation := schedule.BatchMove(state, moves)
		afterState := state.Clone()

		changes := make([]map[string]any, 0, len(moves))
		success := len(moves) > 0
		for _, move := range moves {
			before := snapshotTask(beforeState, move.TaskID)
			after := snapshotTask(afterState, move.TaskID)
			changes = append(changes, buildTaskChange("batch_move", before, after))
			if !after.Exists || !taskHasSlotAt(after, move.NewDay, move.NewSlotStart) {
				success = false
			}
		}

		affectedDays := collectAffectedDays(changes)
		return buildScheduleOperationResult("batch_move", args, afterState, observation, success, affectedDays, changes, nil, "")
	}
}

// NewUnplaceToolHandler 返回 unplace 的结构化结果 handler（第一轮真实 result_view）。
func NewUnplaceToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		taskID, ok := schedule.ArgsInt(args, "task_id")
		if !ok {
			return buildScheduleArgErrorResult("unplace", args, "缺少必填参数 task_id。", state)
		}
		if state == nil {
			return buildScheduleArgErrorResult("unplace", args, "日程状态为空，无法执行移出。", nil)
		}

		beforeState := state.Clone()
		observation := schedule.Unplace(state, taskID)
		afterState := state.Clone()

		before := snapshotTask(beforeState, taskID)
		after := snapshotTask(afterState, taskID)
		success := before.Exists && len(before.Slots) > 0 && len(after.Slots) == 0 && strings.EqualFold(strings.TrimSpace(after.Status), schedule.TaskStatusPending)

		changes := []map[string]any{
			buildTaskChange("unplace", before, after),
		}
		affectedDays := collectAffectedDays(changes)
		return buildScheduleOperationResult("unplace", args, afterState, observation, success, affectedDays, changes, nil, "")
	}
}

// NewQueueApplyHeadMoveToolHandler 返回 queue_apply_head_move 的结构化结果 handler（第一轮真实 result_view）。
func NewQueueApplyHeadMoveToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		newDay, dayOK := schedule.ArgsInt(args, "new_day")
		newSlotStart, slotOK := schedule.ArgsInt(args, "new_slot_start")
		if state == nil {
			return buildScheduleArgErrorResult("queue_apply_head_move", args, "日程状态为空，无法执行队首任务应用。", nil)
		}

		// 1. 执行前先记录 current 任务与队列快照，保证成功/失败都可构造稳定结构化视图。
		// 2. 再执行工具并抓取执行后快照，基于 before/after 计算差异，不依赖自然语言解析。
		// 3. 如果快照构造异常，外层仍会回退 LegacyResult，保证工具主链路不被展示层影响。
		beforeState := state.Clone()
		beforeQueue := snapshotQueue(beforeState)
		currentTaskID := 0
		if beforeState != nil && beforeState.RuntimeQueue != nil {
			currentTaskID = beforeState.RuntimeQueue.CurrentTaskID
		}
		beforeTask := snapshotTask(beforeState, currentTaskID)

		observation := schedule.QueueApplyHeadMove(state, args)

		afterState := state.Clone()
		afterQueue := snapshotQueue(afterState)
		afterTask := snapshotTask(afterState, currentTaskID)

		success := false
		if payload, ok := parseObservationJSON(strings.TrimSpace(observation)); ok {
			if parsedSuccess, exists := payload["success"].(bool); exists {
				success = parsedSuccess
			}
		}
		if !success {
			success = currentTaskID > 0 &&
				(afterQueue.CompletedCount > beforeQueue.CompletedCount) &&
				(afterQueue.CurrentTaskID != currentTaskID)
			if dayOK && slotOK && success {
				success = taskHasSlotAt(afterTask, newDay, newSlotStart)
			}
		}

		changes := []map[string]any{
			buildTaskChange("queue_apply_head_move", beforeTask, afterTask),
		}
		affectedDays := collectAffectedDays(changes)
		queueSnapshot := buildQueueSnapshotWithLabels(beforeQueue, afterQueue)
		return buildScheduleOperationResult(
			"queue_apply_head_move",
			args,
			afterState,
			observation,
			success,
			affectedDays,
			changes,
			queueSnapshot,
			pickFailureReason(observation, success),
		)
	}
}

func buildScheduleArgErrorResult(toolName string, args map[string]any, reason string, state *schedule.ScheduleState) ToolExecutionResult {
	observation := fmt.Sprintf("%s失败：%s", scheduleOperationFailurePrefix(toolName), strings.TrimSpace(reason))
	return buildScheduleOperationResult(
		toolName,
		args,
		state,
		observation,
		false,
		nil,
		nil,
		nil,
		strings.TrimSpace(reason),
	)
}

func buildScheduleOperationResult(
	toolName string,
	args map[string]any,
	displayState *schedule.ScheduleState,
	observation string,
	success bool,
	affectedDays []int,
	changes []map[string]any,
	queueSnapshot map[string]any,
	failureReason string,
) ToolExecutionResult {
	result := LegacyResultWithState(toolName, args, displayState, observation)

	status := ToolStatusFailed
	if success {
		status = ToolStatusDone
	}
	operationLabel := resolveOperationLabelCN(toolName)
	title := fmt.Sprintf("%s%s", operationLabel, resolveResultTitleSuffix(status))
	subtitle := buildScheduleSubtitle(toolName, changes, success)
	metrics := []map[string]any{
		{"label": "任务数量", "value": fmt.Sprintf("%d个", maxInt(len(changes), countMovesFromArgs(args)))},
		{"label": "影响天数", "value": fmt.Sprintf("%d天", len(affectedDays))},
	}

	collapsed := map[string]any{
		"title":               title,
		"subtitle":            subtitle,
		"status":              status,
		"status_label":        resolveToolStatusLabelCN(status),
		"operation":           strings.TrimSpace(toolName),
		"operation_label":     operationLabel,
		"task_count":          maxInt(len(changes), countMovesFromArgs(args)),
		"affected_days_count": len(affectedDays),
		"metrics":             metrics,
	}

	expanded := map[string]any{
		"operation":           strings.TrimSpace(toolName),
		"operation_label":     operationLabel,
		"changes":             changes,
		"affected_days":       affectedDays,
		"affected_days_label": formatAffectedDaysLabel(affectedDays),
		"raw_text":            observation,
	}
	if queueSnapshot != nil {
		expanded["queue_snapshot"] = queueSnapshot
	}
	if !success {
		expanded["failure_reason"] = strings.TrimSpace(pickFailureReason(failureReason, false))
	}

	result.Status = status
	result.Success = success
	result.Summary = title
	result.ArgumentsPreview = readArgumentSummary(result.ArgumentView)
	result.ResultView = &ToolDisplayView{
		ViewType:  "schedule.operation_result",
		Version:   1,
		Collapsed: collapsed,
		Expanded:  expanded,
	}
	if !success {
		result.ErrorCode = "schedule_operation_failed"
		if strings.TrimSpace(result.ErrorMessage) == "" {
			result.ErrorMessage = strings.TrimSpace(pickFailureReason(failureReason, false))
		}
	}
	return EnsureToolResultDefaults(result, args)
}

func buildScheduleSubtitle(operation string, changes []map[string]any, success bool) string {
	if len(changes) == 0 {
		if success {
			return fmt.Sprintf("%s已完成", resolveOperationLabelCN(operation))
		}
		return fmt.Sprintf("%s执行失败", resolveOperationLabelCN(operation))
	}

	firstTask := readStringMap(changes[0], "task_label")
	firstBefore := readStringMap(changes[0], "before_label")
	firstAfter := readStringMap(changes[0], "after_label")

	switch strings.TrimSpace(operation) {
	case "move":
		return fmt.Sprintf("%s：从%s移动到%s", firstTask, firstBefore, firstAfter)
	case "place":
		return fmt.Sprintf("%s：预排到%s", firstTask, firstAfter)
	case "unplace":
		return fmt.Sprintf("%s：已从%s移出", firstTask, firstBefore)
	case "swap":
		if len(changes) >= 2 {
			secondTask := readStringMap(changes[1], "task_label")
			return fmt.Sprintf("%s 与 %s 已交换位置", firstTask, secondTask)
		}
		return fmt.Sprintf("%s已交换位置", firstTask)
	case "batch_move":
		return fmt.Sprintf("批量移动 %d 个任务", len(changes))
	case "queue_apply_head_move":
		if success {
			return fmt.Sprintf("队首任务已移动到%s", firstAfter)
		}
		return fmt.Sprintf("队首任务移动失败：%s", firstTask)
	default:
		return fmt.Sprintf("%s：%s", resolveOperationLabelCN(operation), firstTask)
	}
}

func resolveResultTitleSuffix(status string) string {
	switch normalizeToolStatus(status) {
	case ToolStatusDone:
		return "成功"
	case ToolStatusBlocked:
		return "已阻断"
	default:
		return "失败"
	}
}

func pickFailureReason(raw string, success bool) string {
	if success {
		return ""
	}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "操作失败，请查看原始结果。"
	}
	if payload, ok := parseObservationJSON(trimmed); ok {
		if text, ok := readStringFromMap(payload, "result", "error", "reason", "message", "err"); ok {
			return strings.TrimSpace(text)
		}
	}
	return trimmed
}

func snapshotTask(state *schedule.ScheduleState, taskID int) scheduleTaskSnapshot {
	if state == nil || taskID <= 0 {
		return scheduleTaskSnapshot{
			Exists:  false,
			TaskID:  taskID,
			DayInfo: buildDayInfo(state),
		}
	}
	task := state.TaskByStateID(taskID)
	if task == nil {
		return scheduleTaskSnapshot{
			Exists:  false,
			TaskID:  taskID,
			DayInfo: buildDayInfo(state),
		}
	}
	return scheduleTaskSnapshot{
		Exists:  true,
		TaskID:  task.StateID,
		Name:    strings.TrimSpace(task.Name),
		Status:  strings.TrimSpace(task.Status),
		Slots:   cloneSlots(task.Slots),
		DayInfo: buildDayInfo(state),
	}
}

func snapshotQueue(state *schedule.ScheduleState) scheduleQueueSnapshot {
	if state == nil || state.RuntimeQueue == nil {
		return scheduleQueueSnapshot{}
	}
	return scheduleQueueSnapshot{
		PendingCount:   len(state.RuntimeQueue.PendingTaskIDs),
		CompletedCount: len(state.RuntimeQueue.CompletedTaskIDs),
		SkippedCount:   len(state.RuntimeQueue.SkippedTaskIDs),
		CurrentTaskID:  state.RuntimeQueue.CurrentTaskID,
		CurrentAttempt: state.RuntimeQueue.CurrentAttempts,
		LastError:      strings.TrimSpace(state.RuntimeQueue.LastError),
	}
}

func buildQueueSnapshotWithLabels(before scheduleQueueSnapshot, after scheduleQueueSnapshot) map[string]any {
	return map[string]any{
		"before":           queueSnapshotToMap(before),
		"after":            queueSnapshotToMap(after),
		"before_label":     queueSummaryLabel(before),
		"after_label":      queueSummaryLabel(after),
		"summary_label":    queueSummaryLabel(after),
		"last_error_label": strings.TrimSpace(after.LastError),
	}
}

func queueSnapshotToMap(s scheduleQueueSnapshot) map[string]any {
	return map[string]any{
		"pending_count":   s.PendingCount,
		"completed_count": s.CompletedCount,
		"skipped_count":   s.SkippedCount,
		"current_task_id": s.CurrentTaskID,
		"current_attempt": s.CurrentAttempt,
		"last_error":      s.LastError,
	}
}

func queueSummaryLabel(s scheduleQueueSnapshot) string {
	return fmt.Sprintf(
		"待处理%d个，已完成%d个，已跳过%d个，当前任务%d，尝试%d次",
		s.PendingCount,
		s.CompletedCount,
		s.SkippedCount,
		s.CurrentTaskID,
		s.CurrentAttempt,
	)
}

func buildTaskChange(operation string, before scheduleTaskSnapshot, after scheduleTaskSnapshot) map[string]any {
	taskLabel := resolveChangeTaskLabel(before, after)
	beforeStatusLabel := resolveTaskStatusLabelCN(before.Status)
	afterStatusLabel := resolveTaskStatusLabelCN(after.Status)
	beforeLabel := formatPlacementLabel(operation, before.Slots, before.Status, false, false)
	afterLabel := formatPlacementLabel(operation, after.Slots, after.Status, true, len(before.Slots) > 0)

	change := map[string]any{
		"task_id": before.TaskID,
		"name":    firstNonEmpty(before.Name, after.Name),
		"status": map[string]any{
			"before": before.Status,
			"after":  after.Status,
		},
		"before_slots":  slotsToView(before.Slots, before.DayInfo),
		"after_slots":   slotsToView(after.Slots, after.DayInfo),
		"task_label":    taskLabel,
		"before_label":  beforeLabel,
		"after_label":   afterLabel,
		"status_label":  fmt.Sprintf("%s -> %s", beforeStatusLabel, afterStatusLabel),
		"operation_key": operation,
	}
	return change
}

func resolveChangeTaskLabel(before scheduleTaskSnapshot, after scheduleTaskSnapshot) string {
	name := firstNonEmpty(before.Name, after.Name)
	if name == "" {
		if before.TaskID > 0 {
			return fmt.Sprintf("[%d]任务", before.TaskID)
		}
		return "任务"
	}
	if before.TaskID > 0 {
		return fmt.Sprintf("[%d]%s", before.TaskID, name)
	}
	return name
}

func resolveTaskStatusLabelCN(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case schedule.TaskStatusPending:
		return "待安排"
	case schedule.TaskStatusSuggested:
		return "已预排"
	case schedule.TaskStatusExisting:
		return "已安排"
	default:
		return "未知状态"
	}
}

func formatPlacementLabel(operation string, slots []schedule.TaskSlot, status string, isAfter bool, hadBefore bool) string {
	if len(slots) > 0 {
		return formatSlotsLabelCN(slots)
	}
	if isAfter && strings.TrimSpace(operation) == "unplace" && hadBefore {
		return "已移出"
	}
	if strings.EqualFold(strings.TrimSpace(status), schedule.TaskStatusPending) {
		return "未安排"
	}
	return "未安排"
}

func formatSlotsLabelCN(slots []schedule.TaskSlot) string {
	if len(slots) == 0 {
		return "未安排"
	}
	parts := make([]string, 0, len(slots))
	for _, slot := range slots {
		parts = append(parts, fmt.Sprintf("%s %s", formatDayLabelCN(slot.Day), formatSlotRangeCN(slot.SlotStart, slot.SlotEnd)))
	}
	return strings.Join(parts, "、")
}

func formatAffectedDaysLabel(affectedDays []int) string {
	if len(affectedDays) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(affectedDays))
	for _, day := range affectedDays {
		parts = append(parts, formatDayLabelCN(day))
	}
	return strings.Join(parts, "、")
}

func slotsToView(slots []schedule.TaskSlot, dayInfo map[int]schedule.DayMapping) []map[string]any {
	if len(slots) == 0 {
		return make([]map[string]any, 0)
	}
	result := make([]map[string]any, 0, len(slots))
	for _, slot := range slots {
		entry := map[string]any{
			"day":        slot.Day,
			"slot_start": slot.SlotStart,
			"slot_end":   slot.SlotEnd,
		}
		if info, ok := dayInfo[slot.Day]; ok {
			entry["week"] = info.Week
			entry["day_of_week"] = info.DayOfWeek
		}
		result = append(result, entry)
	}
	return result
}

func collectAffectedDays(changes []map[string]any) []int {
	if len(changes) == 0 {
		return make([]int, 0)
	}
	set := make(map[int]struct{})
	for _, change := range changes {
		collectDaysFromSlotView(set, change["before_slots"])
		collectDaysFromSlotView(set, change["after_slots"])
	}
	days := make([]int, 0, len(set))
	for day := range set {
		days = append(days, day)
	}
	sort.Ints(days)
	return days
}

func collectDaysFromSlotView(target map[int]struct{}, raw any) {
	list, ok := raw.([]map[string]any)
	if ok {
		for _, item := range list {
			day, ok := item["day"].(int)
			if ok {
				target[day] = struct{}{}
			}
		}
		return
	}

	anyList, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range anyList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch day := itemMap["day"].(type) {
		case int:
			target[day] = struct{}{}
		case float64:
			target[int(day)] = struct{}{}
		}
	}
}

func buildDayInfo(state *schedule.ScheduleState) map[int]schedule.DayMapping {
	if state == nil || len(state.Window.DayMapping) == 0 {
		return map[int]schedule.DayMapping{}
	}
	info := make(map[int]schedule.DayMapping, len(state.Window.DayMapping))
	for _, item := range state.Window.DayMapping {
		info[item.DayIndex] = item
	}
	return info
}

func cloneSlots(slots []schedule.TaskSlot) []schedule.TaskSlot {
	if len(slots) == 0 {
		return nil
	}
	out := make([]schedule.TaskSlot, len(slots))
	copy(out, slots)
	return out
}

func sameSlots(a []schedule.TaskSlot, b []schedule.TaskSlot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Day != b[i].Day || a[i].SlotStart != b[i].SlotStart || a[i].SlotEnd != b[i].SlotEnd {
			return false
		}
	}
	return true
}

func taskHasSlotAt(snapshot scheduleTaskSnapshot, day int, slotStart int) bool {
	for _, slot := range snapshot.Slots {
		if slot.Day == day && slot.SlotStart == slotStart {
			return true
		}
	}
	return false
}

func readStringMap(input map[string]any, key string) string {
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func countMovesFromArgs(args map[string]any) int {
	moves, ok := args["moves"].([]any)
	if !ok {
		return 0
	}
	return len(moves)
}

func maxInt(values ...int) int {
	if len(values) == 0 {
		return 0
	}
	maxValue := values[0]
	for _, value := range values[1:] {
		if value > maxValue {
			maxValue = value
		}
	}
	return maxValue
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func scheduleOperationFailurePrefix(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "place":
		return "放置"
	case "move", "queue_apply_head_move":
		return "移动"
	case "swap":
		return "交换"
	case "batch_move":
		return "批量移动"
	case "unplace":
		return "移除"
	default:
		return strings.TrimSpace(toolName)
	}
}
