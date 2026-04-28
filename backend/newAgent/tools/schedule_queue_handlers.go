package newagenttools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	scheduleread "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule_read"
)

type queueTaskSlotSnapshot struct {
	Day       int `json:"day"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	SlotStart int `json:"slot_start"`
	SlotEnd   int `json:"slot_end"`
}

type queueTaskSnapshotPayload struct {
	TaskID      int                     `json:"task_id"`
	Name        string                  `json:"name"`
	Category    string                  `json:"category,omitempty"`
	Status      string                  `json:"status"`
	Duration    int                     `json:"duration,omitempty"`
	TaskClassID int                     `json:"task_class_id,omitempty"`
	Slots       []queueTaskSlotSnapshot `json:"slots,omitempty"`
}

type queuePopHeadPayload struct {
	Tool           string                    `json:"tool"`
	HasHead        bool                      `json:"has_head"`
	PendingCount   int                       `json:"pending_count"`
	CompletedCount int                       `json:"completed_count"`
	SkippedCount   int                       `json:"skipped_count"`
	Current        *queueTaskSnapshotPayload `json:"current,omitempty"`
	LastError      string                    `json:"last_error,omitempty"`
	Error          string                    `json:"error,omitempty"`
}

type queueSkipHeadPayload struct {
	Tool          string `json:"tool"`
	Success       bool   `json:"success"`
	SkippedTaskID int    `json:"skipped_task_id,omitempty"`
	PendingCount  int    `json:"pending_count"`
	SkippedCount  int    `json:"skipped_count"`
	Reason        string `json:"reason,omitempty"`
	Error         string `json:"error,omitempty"`
}

// NewQueuePopHeadToolHandler 返回 queue_pop_head 的结构化读卡片。
//
// 设计说明：
// 1. 这个工具本质是“读取当前队首处理对象”，因此继续走 schedule.read_result；
// 2. 不修改 schedule_read 子包，只在父包做一个轻量 adapter，复用既有 read 卡片协议；
// 3. 原始 ObservationText 继续保留 JSON 字符串，供 execute/timeline/模型链路复用。
func NewQueuePopHeadToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := schedule.QueuePopHead(state, args)
		legacy := LegacyResultWithState("queue_pop_head", args, state, observation)
		argFields := extractScheduleReadArgumentFields(legacy.ArgumentView)

		payload, machinePayload, ok := decodeQueuePopHeadPayload(observation)
		if !ok || normalizeToolStatus(legacy.Status) != ToolStatusDone {
			view := scheduleread.BuildFailureView(scheduleread.BuildFailureViewInput{
				ToolName:    "queue_pop_head",
				Status:      legacy.Status,
				Observation: observation,
				ArgFields:   argFields,
			})
			return buildScheduleReadExecutionResult(legacy, args, view)
		}

		view := buildQueuePopHeadReadView(state, observation, payload, machinePayload, argFields)
		return buildScheduleReadExecutionResult(legacy, args, view)
	}
}

// NewQueueSkipHeadToolHandler 返回 queue_skip_head 的结构化操作卡片。
//
// 设计说明：
// 1. 这个工具会改变 RuntimeQueue，因此继续落在 schedule.operation_result 语义下；
// 2. 但它不涉及日程位移，所以这里不强行复用 task change 列表，只展示队列前后快照；
// 3. 这样能去掉 legacy wrapper，同时避免把 queue 小尾巴抽成新的大协议。
func NewQueueSkipHeadToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		beforeState := cloneScheduleStateOrNil(state)
		beforeQueue := snapshotQueue(beforeState)
		currentTaskID := beforeQueue.CurrentTaskID
		currentTask := snapshotTask(beforeState, currentTaskID)

		observation := schedule.QueueSkipHead(state, args)

		afterState := cloneScheduleStateOrNil(state)
		afterQueue := snapshotQueue(afterState)
		legacy := LegacyResultWithState("queue_skip_head", args, afterState, observation)

		payload, machinePayload, ok := decodeQueueSkipHeadPayload(observation)
		success := false
		if ok {
			success = payload.Success
		}
		if !success {
			success = currentTaskID > 0 &&
				(afterQueue.SkippedCount > beforeQueue.SkippedCount) &&
				(afterQueue.CurrentTaskID != currentTaskID)
		}

		return buildQueueSkipHeadExecutionResult(
			legacy,
			args,
			observation,
			success,
			beforeQueue,
			afterQueue,
			currentTask,
			payload,
			machinePayload,
		)
	}
}

func buildQueuePopHeadReadView(
	state *schedule.ScheduleState,
	observation string,
	payload queuePopHeadPayload,
	machinePayload map[string]any,
	argFields []scheduleread.KVField,
) scheduleread.ReadResultView {
	items := make([]scheduleread.ItemView, 0, 1)
	sections := make([]map[string]any, 0, 4)

	if payload.Current != nil {
		currentItem := buildQueuePopHeadCurrentItem(state, payload.Current)
		items = append(items, currentItem)
		sections = append(sections, scheduleread.BuildItemsSection("当前处理", []scheduleread.ItemView{currentItem}))
	}

	sections = append(sections, scheduleread.BuildKVSection("队列快照", []scheduleread.KVField{
		scheduleread.BuildKVField("待处理", fmt.Sprintf("%d 项", payload.PendingCount)),
		scheduleread.BuildKVField("已完成", fmt.Sprintf("%d 项", payload.CompletedCount)),
		scheduleread.BuildKVField("已跳过", fmt.Sprintf("%d 项", payload.SkippedCount)),
		scheduleread.BuildKVField("当前队首", buildQueuePopHeadCurrentLabel(payload.Current)),
	}))

	if payload.HasHead {
		sections = append(sections, buildQueueReadCalloutSection(
			"队首任务已就位",
			"可以继续调用 queue_apply_head_move 或 queue_skip_head。",
			"info",
			buildQueuePopHeadHintLines(payload),
		))
	} else {
		sections = append(sections, buildQueueReadCalloutSection(
			"当前没有可处理任务",
			"队列里没有 pending/current 任务，可以结束队列链路或重新 enqueue。",
			"warning",
			buildQueuePopHeadHintLines(payload),
		))
	}

	if strings.TrimSpace(payload.LastError) != "" {
		sections = append(sections, buildQueueReadCalloutSection(
			"最近一次失败原因",
			strings.TrimSpace(payload.LastError),
			"warning",
			[]string{strings.TrimSpace(payload.LastError)},
		))
	}

	if argsSection := scheduleread.BuildArgsSection("查询条件", argFields); argsSection != nil {
		sections = append(sections, argsSection)
	}

	return scheduleread.BuildResultView(scheduleread.BuildResultViewInput{
		Status:         scheduleread.StatusDone,
		Title:          buildQueuePopHeadTitle(payload),
		Subtitle:       buildQueuePopHeadSubtitle(payload),
		Metrics:        buildQueuePopHeadMetrics(payload),
		Items:          items,
		Sections:       sections,
		Observation:    observation,
		MachinePayload: machinePayload,
	})
}

func buildQueueSkipHeadExecutionResult(
	legacy ToolExecutionResult,
	args map[string]any,
	observation string,
	success bool,
	beforeQueue scheduleQueueSnapshot,
	afterQueue scheduleQueueSnapshot,
	currentTask scheduleTaskSnapshot,
	payload queueSkipHeadPayload,
	machinePayload map[string]any,
) ToolExecutionResult {
	result := legacy
	status := ToolStatusFailed
	if success {
		status = ToolStatusDone
	}

	taskLabel := resolveChangeTaskLabel(currentTask, currentTask)
	queueSnapshot := buildQueueSnapshotWithLabels(beforeQueue, afterQueue)
	if len(queueSnapshot) == 0 {
		queueSnapshot = make(map[string]any)
	}
	queueSnapshot["summary_label"] = buildQueueSkipSnapshotTitle(success, taskLabel)
	if strings.TrimSpace(payload.Reason) != "" {
		queueSnapshot["skip_reason"] = strings.TrimSpace(payload.Reason)
	}
	if strings.TrimSpace(taskLabel) != "" {
		queueSnapshot["skipped_task_label"] = strings.TrimSpace(taskLabel)
	}

	title := buildQueueSkipHeadTitle(success)
	subtitle := buildQueueSkipHeadSubtitle(success, taskLabel, payload.Reason)
	collapsed := map[string]any{
		"title":           title,
		"subtitle":        subtitle,
		"status":          status,
		"status_label":    resolveToolStatusLabelCN(status),
		"operation":       "queue_skip_head",
		"operation_label": resolveToolLabelCN("queue_skip_head"),
		"metrics": []map[string]any{
			{"label": "待处理", "value": fmt.Sprintf("%d 项", afterQueue.PendingCount)},
			{"label": "已跳过", "value": fmt.Sprintf("%d 项", afterQueue.SkippedCount)},
			{"label": "当前队首", "value": buildQueueCurrentMetricValue(afterQueue.CurrentTaskID)},
		},
	}
	expanded := map[string]any{
		"operation":       "queue_skip_head",
		"operation_label": resolveToolLabelCN("queue_skip_head"),
		"queue_snapshot":  queueSnapshot,
		"raw_text":        observation,
	}
	if len(machinePayload) > 0 {
		expanded["machine_payload"] = machinePayload
	}
	if !success {
		expanded["failure_reason"] = strings.TrimSpace(pickFailureReason(observation, false))
	}

	result.Status = status
	result.Success = success
	result.Summary = title
	result.ResultView = &ToolDisplayView{
		ViewType:  "schedule.operation_result",
		Version:   1,
		Collapsed: collapsed,
		Expanded:  expanded,
	}
	if !success {
		errorCode, errorMessage := extractToolErrorInfo(observation, status)
		if strings.TrimSpace(result.ErrorCode) == "" {
			result.ErrorCode = strings.TrimSpace(errorCode)
		}
		if strings.TrimSpace(result.ErrorMessage) == "" {
			result.ErrorMessage = strings.TrimSpace(errorMessage)
		}
	}
	return EnsureToolResultDefaults(result, args)
}

func decodeQueuePopHeadPayload(observation string) (queuePopHeadPayload, map[string]any, bool) {
	var payload queuePopHeadPayload
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" {
		return payload, nil, false
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return payload, nil, false
	}
	raw, ok := parseObservationJSON(trimmed)
	return payload, raw, ok
}

func decodeQueueSkipHeadPayload(observation string) (queueSkipHeadPayload, map[string]any, bool) {
	var payload queueSkipHeadPayload
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" {
		return payload, nil, false
	}
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return payload, nil, false
	}
	raw, ok := parseObservationJSON(trimmed)
	return payload, raw, ok
}

func buildQueuePopHeadTitle(payload queuePopHeadPayload) string {
	if payload.HasHead {
		return "已获取队首任务"
	}
	return "当前队列无可处理任务"
}

func buildQueuePopHeadSubtitle(payload queuePopHeadPayload) string {
	if payload.Current != nil {
		return fmt.Sprintf("%s，待处理 %d 项。", buildQueueTaskLabel(payload.Current), payload.PendingCount)
	}
	if strings.TrimSpace(payload.LastError) != "" {
		return "当前没有队首任务，最近一次失败原因已保留。"
	}
	return "没有 pending/current 任务，可结束队列链路或重新入队。"
}

func buildQueuePopHeadMetrics(payload queuePopHeadPayload) []scheduleread.MetricField {
	return []scheduleread.MetricField{
		scheduleread.BuildMetric("待处理", fmt.Sprintf("%d 项", payload.PendingCount)),
		scheduleread.BuildMetric("已完成", fmt.Sprintf("%d 项", payload.CompletedCount)),
		scheduleread.BuildMetric("已跳过", fmt.Sprintf("%d 项", payload.SkippedCount)),
	}
}

func buildQueuePopHeadCurrentItem(state *schedule.ScheduleState, payload *queueTaskSnapshotPayload) scheduleread.ItemView {
	if payload == nil {
		return scheduleread.BuildItem("当前无队首任务", "", nil, nil, nil)
	}
	tags := []string{"当前处理", resolveTaskStatusLabelCN(payload.Status)}
	if payload.Duration > 0 {
		tags = append(tags, fmt.Sprintf("%d 节", payload.Duration))
	}
	return scheduleread.BuildItem(
		buildQueueTaskLabel(payload),
		buildQueueTaskSubtitle(payload),
		tags,
		buildQueueTaskDetailLines(state, payload),
		map[string]any{
			"task_id":       payload.TaskID,
			"task_class_id": payload.TaskClassID,
			"status":        payload.Status,
		},
	)
}

func buildQueuePopHeadCurrentLabel(payload *queueTaskSnapshotPayload) string {
	if payload == nil {
		return "无"
	}
	return buildQueueTaskLabel(payload)
}

func buildQueuePopHeadHintLines(payload queuePopHeadPayload) []string {
	lines := []string{
		fmt.Sprintf("待处理：%d 项", payload.PendingCount),
		fmt.Sprintf("已完成：%d 项", payload.CompletedCount),
		fmt.Sprintf("已跳过：%d 项", payload.SkippedCount),
	}
	if payload.Current != nil {
		lines = append(lines, fmt.Sprintf("当前队首：%s", buildQueueTaskLabel(payload.Current)))
	}
	return lines
}

func buildQueueSkipHeadTitle(success bool) string {
	if success {
		return "已跳过队首任务"
	}
	return "跳过队首任务失败"
}

func buildQueueSkipHeadSubtitle(success bool, taskLabel string, reason string) string {
	if success {
		if strings.TrimSpace(taskLabel) != "" {
			return fmt.Sprintf("已将 %s 标记为 skipped，可继续 queue_pop_head。", strings.TrimSpace(taskLabel))
		}
		return "已跳过当前队首任务，可继续 queue_pop_head。"
	}
	if strings.TrimSpace(reason) != "" {
		return strings.TrimSpace(reason)
	}
	return "当前没有可跳过的队首任务。"
}

func buildQueueSkipSnapshotTitle(success bool, taskLabel string) string {
	if success && strings.TrimSpace(taskLabel) != "" {
		return fmt.Sprintf("已跳过 %s", strings.TrimSpace(taskLabel))
	}
	if success {
		return "队列已跳过当前队首"
	}
	return "队列状态未变更"
}

func buildQueueCurrentMetricValue(taskID int) string {
	if taskID <= 0 {
		return "无"
	}
	return fmt.Sprintf("%d", taskID)
}

func buildQueueTaskLabel(payload *queueTaskSnapshotPayload) string {
	if payload == nil {
		return "任务"
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return fmt.Sprintf("[%d]任务", payload.TaskID)
	}
	return fmt.Sprintf("[%d]%s", payload.TaskID, name)
}

func buildQueueTaskSubtitle(payload *queueTaskSnapshotPayload) string {
	if payload == nil {
		return ""
	}
	category := strings.TrimSpace(payload.Category)
	status := resolveTaskStatusLabelCN(payload.Status)
	if category == "" {
		return status
	}
	return fmt.Sprintf("%s，%s", category, status)
}

func buildQueueTaskDetailLines(state *schedule.ScheduleState, payload *queueTaskSnapshotPayload) []string {
	if payload == nil {
		return nil
	}
	lines := make([]string, 0, 3)
	if len(payload.Slots) > 0 {
		slotParts := make([]string, 0, len(payload.Slots))
		for _, slot := range payload.Slots {
			slotParts = append(slotParts, buildQueueSlotLabel(state, slot))
		}
		lines = append(lines, "时段："+strings.Join(slotParts, "；"))
	} else {
		lines = append(lines, "时段：当前还未落位")
	}
	if payload.TaskClassID > 0 {
		lines = append(lines, fmt.Sprintf("任务类 ID：%d", payload.TaskClassID))
	}
	if payload.Duration > 0 {
		lines = append(lines, fmt.Sprintf("时长需求：%d 节", payload.Duration))
	}
	return lines
}

func buildQueueSlotLabel(state *schedule.ScheduleState, slot queueTaskSlotSnapshot) string {
	dayLabel := formatDayLabelCN(slot.Day)
	if state != nil {
		if week, dayOfWeek, ok := state.DayToWeekDay(slot.Day); ok {
			dayLabel = fmt.Sprintf("%s（第%d周 周%d）", formatDayLabelCN(slot.Day), week, dayOfWeek)
		}
	}
	if slot.Week > 0 && slot.DayOfWeek > 0 {
		dayLabel = fmt.Sprintf("%s（第%d周 周%d）", formatDayLabelCN(slot.Day), slot.Week, slot.DayOfWeek)
	}
	return fmt.Sprintf("%s %s", dayLabel, formatSlotRangeCN(slot.SlotStart, slot.SlotEnd))
}

func buildQueueReadCalloutSection(title string, summary string, tone string, detailLines []string) map[string]any {
	return map[string]any{
		"type":         "callout",
		"title":        strings.TrimSpace(title),
		"summary":      strings.TrimSpace(summary),
		"tone":         strings.TrimSpace(tone),
		"detail_lines": normalizeQueueDetailLines(detailLines),
	}
}

func normalizeQueueDetailLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneScheduleStateOrNil(state *schedule.ScheduleState) *schedule.ScheduleState {
	if state == nil {
		return nil
	}
	return state.Clone()
}
