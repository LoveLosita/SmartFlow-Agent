package schedule_read

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
)

// BuildTargetTasksView 构造 query_target_tasks 的纯展示视图。
func BuildTargetTasksView(input TargetTasksViewInput) ReadResultView {
	payload, machinePayload, ok := DecodeTargetTasksPayload(input.Observation)
	if !ok || !payload.Success {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "query_target_tasks",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	items := make([]ItemView, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, BuildItem(
			fmt.Sprintf("[%d]%s", item.TaskID, fallbackText(item.Name, "未命名任务")),
			buildTargetTaskSubtitle(item),
			buildTargetTaskTags(item),
			buildTargetTaskDetailLines(input.State, item),
			map[string]any{
				"task_id":       item.TaskID,
				"category":      item.Category,
				"status":        item.Status,
				"duration":      item.Duration,
				"task_class_id": item.TaskClassID,
			},
		))
	}

	metrics := []MetricField{
		BuildMetric("候选任务", fmt.Sprintf("%d 项", payload.Count)),
		BuildMetric("任务池", formatTargetPoolStatusCN(payload.Status)),
	}
	if payload.Enqueue {
		metrics = append(metrics, BuildMetric("已入队", fmt.Sprintf("%d 项", payload.Enqueued)))
	}

	sections := []map[string]any{
		BuildKVSection("筛选概况", []KVField{
			BuildKVField("任务池", formatTargetPoolStatusCN(payload.Status)),
			BuildKVField("日期范围", formatDayScopeLabelCN(payload.DayScope)),
			BuildKVField("星期过滤", formatWeekdayListCN(payload.DayOfWeek)),
			BuildKVField("周次范围", buildWeekRangeLabelCN(payload.WeekFrom, payload.WeekTo, payload.WeekFilter)),
			BuildKVField("是否入队", formatBoolLabelCN(payload.Enqueue)),
		}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("筛选条件", input.ArgFields))
	if payload.Queue != nil {
		sections = append(sections, BuildKVSection("队列状态", []KVField{
			BuildKVField("待处理", fmt.Sprintf("%d 项", payload.Queue.PendingCount)),
			BuildKVField("已完成", fmt.Sprintf("%d 项", payload.Queue.CompletedCount)),
			BuildKVField("已跳过", fmt.Sprintf("%d 项", payload.Queue.SkippedCount)),
			BuildKVField("当前任务", resolveTaskQueueLabelByID(input.State, payload.Queue.CurrentTaskID)),
		}))
	}
	if len(items) > 0 {
		sections = append(sections, BuildItemsSection("候选任务", items))
	} else {
		sections = append(sections, BuildCalloutSection(
			"没有命中任务",
			"当前筛选条件下没有找到候选任务。",
			"info",
			[]string{"可以放宽状态、日期或任务 ID 过滤条件后再试。"},
		))
	}

	title := fmt.Sprintf("找到 %d 个候选任务", payload.Count)
	if payload.Count == 0 {
		title = "未找到候选任务"
	}
	return BuildResultView(BuildResultViewInput{
		Status:         StatusDone,
		Title:          title,
		Subtitle:       buildTargetTasksSummarySubtitle(payload),
		Metrics:        metrics,
		Items:          items,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: machinePayload,
	})
}

// BuildTaskInfoView 构造 get_task_info 的纯展示视图。
func BuildTaskInfoView(input TaskInfoViewInput) ReadResultView {
	if input.State == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "get_task_info",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}
	task := input.State.TaskByStateID(input.TaskID)
	if task == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "get_task_info",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	slotItems := make([]ItemView, 0, len(task.Slots))
	for _, slot := range cloneAndSortTaskSlots(task.Slots) {
		slotItems = append(slotItems, BuildItem(
			formatScheduleDaySlotCN(input.State, slot.Day, slot.SlotStart, slot.SlotEnd),
			formatScheduleTaskStatusCN(*task),
			[]string{fallbackText(task.Category, "未分类")},
			[]string{
				fmt.Sprintf("来源：%s", formatScheduleTaskSourceCN(*task)),
				fmt.Sprintf("时长：%d 节", slot.SlotEnd-slot.SlotStart+1),
			},
			map[string]any{
				"day":        slot.Day,
				"slot_start": slot.SlotStart,
				"slot_end":   slot.SlotEnd,
			},
		))
	}

	fields := []KVField{
		BuildKVField("类别", fallbackText(task.Category, "未分类")),
		BuildKVField("状态", formatScheduleTaskStatusCN(*task)),
		BuildKVField("来源", formatScheduleTaskSourceCN(*task)),
		BuildKVField("落位情况", buildTaskPlacementLabel(task)),
		BuildKVField("时长需求", buildTaskDurationLabel(task)),
	}
	if task.TaskClassID > 0 {
		fields = append(fields, BuildKVField("任务类 ID", fmt.Sprintf("%d", task.TaskClassID)))
	}
	if task.CanEmbed {
		fields = append(fields, BuildKVField("可作为宿主", "是"))
	}

	sections := []map[string]any{
		BuildKVSection("基本信息", fields),
	}
	if len(slotItems) > 0 {
		sections = append(sections, BuildItemsSection("占用时段", slotItems))
	}
	if relationLines := buildTaskRelationLines(input.State, task); len(relationLines) > 0 {
		sections = append(sections, BuildCalloutSection("嵌入关系", "当前任务存在宿主或宿体关系。", "info", relationLines))
	}
	appendSectionIfPresent(&sections, BuildArgsSection("查询条件", input.ArgFields))

	return BuildResultView(BuildResultViewInput{
		Status:   StatusDone,
		Title:    fmt.Sprintf("[%d]%s", task.StateID, fallbackText(task.Name, "未命名任务")),
		Subtitle: fmt.Sprintf("%s，%s", fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(*task)),
		Metrics: []MetricField{
			BuildMetric("状态", formatScheduleTaskStatusCN(*task)),
			BuildMetric("时长", buildTaskDurationLabel(task)),
			BuildMetric("落位", buildTaskPlacementLabel(task)),
		},
		Items:       slotItems,
		Sections:    sections,
		Observation: input.Observation,
		MachinePayload: map[string]any{
			"task_id":       task.StateID,
			"source":        task.Source,
			"status":        task.Status,
			"task_class_id": task.TaskClassID,
			"can_embed":     task.CanEmbed,
			"embedded_by":   optionalIntValue(task.EmbeddedBy),
			"embed_host":    optionalIntValue(task.EmbedHost),
		},
	})
}

// DecodeTargetTasksPayload 解析 query_target_tasks 的 JSON observation。
func DecodeTargetTasksPayload(observation string) (TargetTasksPayload, map[string]any, bool) {
	var payload TargetTasksPayload
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

func buildTargetTasksSummarySubtitle(payload TargetTasksPayload) string {
	parts := []string{
		formatTargetPoolStatusCN(payload.Status),
		formatDayScopeLabelCN(payload.DayScope),
		buildWeekRangeLabelCN(payload.WeekFrom, payload.WeekTo, payload.WeekFilter),
	}
	if len(payload.DayOfWeek) > 0 {
		parts = append(parts, formatWeekdayListCN(payload.DayOfWeek))
	}
	if payload.Enqueue {
		parts = append(parts, fmt.Sprintf("已入队 %d 项", payload.Enqueued))
	}
	return strings.Join(parts, "，")
}

func buildTargetTaskSubtitle(item TargetTaskRecord) string {
	return fmt.Sprintf("%s，%s", fallbackText(item.Category, "未分类"), formatTargetTaskStatusCN(item.Status))
}

func buildTargetTaskTags(item TargetTaskRecord) []string {
	tags := []string{formatTargetTaskStatusCN(item.Status)}
	if item.Duration > 0 {
		tags = append(tags, fmt.Sprintf("%d 节", item.Duration))
	}
	if item.TaskClassID > 0 {
		tags = append(tags, fmt.Sprintf("任务类 %d", item.TaskClassID))
	}
	return tags
}

func buildTargetTaskDetailLines(state *schedule.ScheduleState, item TargetTaskRecord) []string {
	lines := make([]string, 0, 3)
	if len(item.Slots) == 0 {
		lines = append(lines, fmt.Sprintf("当前未落位，仍需要 %s。", buildTaskDurationText(item.Duration)))
	} else {
		slotParts := make([]string, 0, len(item.Slots))
		for _, slot := range item.Slots {
			slotParts = append(slotParts, formatScheduleDaySlotCN(state, slot.Day, slot.SlotStart, slot.SlotEnd))
		}
		lines = append(lines, "时段："+strings.Join(slotParts, "；"))
	}
	if item.TaskClassID > 0 {
		lines = append(lines, fmt.Sprintf("任务类 ID：%d", item.TaskClassID))
	}
	return lines
}

func resolveTaskQueueLabelByID(state *schedule.ScheduleState, taskID int) string {
	if taskID <= 0 {
		return "无"
	}
	if state == nil {
		return fmt.Sprintf("[%d]任务", taskID)
	}
	task := state.TaskByStateID(taskID)
	if task == nil {
		return fmt.Sprintf("[%d]任务", taskID)
	}
	return fmt.Sprintf("[%d]%s", task.StateID, fallbackText(task.Name, "未命名任务"))
}

func buildTaskDurationLabel(task *schedule.ScheduleTask) string {
	if task == nil {
		return "未标注"
	}
	if task.Duration > 0 {
		return fmt.Sprintf("%d 节", task.Duration)
	}
	total := 0
	for _, slot := range task.Slots {
		total += slot.SlotEnd - slot.SlotStart + 1
	}
	if total <= 0 {
		return "未标注"
	}
	return fmt.Sprintf("%d 节", total)
}

func buildTaskDurationText(duration int) string {
	if duration <= 0 {
		return "未标注时长"
	}
	return fmt.Sprintf("%d 节连续时段", duration)
}

func buildTaskPlacementLabel(task *schedule.ScheduleTask) string {
	if task == nil || len(task.Slots) == 0 {
		return "尚未落位"
	}
	if len(task.Slots) == 1 {
		slot := task.Slots[0]
		return fmt.Sprintf("1 段（第%d天 第%d-%d节）", slot.Day, slot.SlotStart, slot.SlotEnd)
	}
	return fmt.Sprintf("%d 段", len(task.Slots))
}

func buildTaskRelationLines(state *schedule.ScheduleState, task *schedule.ScheduleTask) []string {
	if task == nil {
		return nil
	}
	lines := make([]string, 0, 2)
	if task.EmbeddedBy != nil {
		lines = append(lines, "当前已嵌入任务："+resolveTaskQueueLabelByID(state, *task.EmbeddedBy))
	} else if task.CanEmbed {
		lines = append(lines, "当前没有嵌入其他任务。")
	}
	if task.EmbedHost != nil {
		lines = append(lines, "嵌入宿主："+resolveTaskQueueLabelByID(state, *task.EmbedHost))
	}
	return lines
}
