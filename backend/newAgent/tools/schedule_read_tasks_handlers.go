package newagenttools

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// NewQueryTargetTasksToolHandler 为 query_target_tasks 生成结构化读结果。
func NewQueryTargetTasksToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := schedule.QueryTargetTasks(state, args)
		status, _ := resolveToolStatusAndSuccess(observation)
		if status != ToolStatusDone {
			return buildScheduleReadSimpleFailureResult("query_target_tasks", args, state, observation)
		}

		payload, machinePayload := decodeTargetTasksPayload(observation)
		items := make([]map[string]any, 0, len(payload.Items))
		for _, item := range payload.Items {
			items = append(items, buildScheduleReadItem(
				fmt.Sprintf("[%d]%s", item.TaskID, fallbackText(item.Name, "未命名任务")),
				buildTargetTaskSubtitle(item),
				buildTargetTaskTags(item),
				buildTargetTaskDetailLines(state, item),
				map[string]any{
					"task_id":       item.TaskID,
					"category":      item.Category,
					"status":        item.Status,
					"duration":      item.Duration,
					"task_class_id": item.TaskClassID,
				},
			))
		}

		metrics := []map[string]any{
			buildScheduleReadMetric("候选任务", fmt.Sprintf("%d 项", payload.Count)),
			buildScheduleReadMetric("任务池", formatTargetPoolStatusCN(payload.Status)),
		}
		if payload.Enqueue {
			metrics = append(metrics, buildScheduleReadMetric("已入队", fmt.Sprintf("%d 项", payload.Enqueued)))
		}

		sections := []map[string]any{
			buildScheduleReadKVSection("筛选概况", []map[string]any{
				buildScheduleReadKV("任务池", formatTargetPoolStatusCN(payload.Status)),
				buildScheduleReadKV("日期范围", formatDayScopeLabelCN(payload.DayScope)),
				buildScheduleReadKV("星期过滤", formatWeekdayListCN(payload.DayOfWeek)),
				buildScheduleReadKV("周次范围", buildWeekRangeLabelCN(payload.WeekFrom, payload.WeekTo, payload.WeekFilter)),
				buildScheduleReadKV("是否入队", formatBoolLabelCN(payload.Enqueue)),
			}),
		}
		appendSectionIfPresent(&sections, buildScheduleReadArgsSection("筛选条件", LegacyResultWithState("query_target_tasks", args, state, observation).ArgumentView))
		if payload.Queue != nil {
			sections = append(sections, buildScheduleReadKVSection("队列状态", []map[string]any{
				buildScheduleReadKV("待处理", fmt.Sprintf("%d 项", payload.Queue.PendingCount)),
				buildScheduleReadKV("已完成", fmt.Sprintf("%d 项", payload.Queue.CompletedCount)),
				buildScheduleReadKV("已跳过", fmt.Sprintf("%d 项", payload.Queue.SkippedCount)),
				buildScheduleReadKV("当前任务", resolveTaskQueueLabelByID(state, payload.Queue.CurrentTaskID)),
			}))
		}
		if len(items) > 0 {
			sections = append(sections, buildScheduleReadItemsSection("候选任务", items))
		} else {
			sections = append(sections, buildScheduleReadCalloutSection(
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
		return buildScheduleReadResult(
			"query_target_tasks",
			args,
			state,
			observation,
			ToolStatusDone,
			title,
			buildTargetTasksSummarySubtitle(payload),
			metrics,
			items,
			sections,
			machinePayload,
		)
	}
}

// NewGetTaskInfoToolHandler 为 get_task_info 生成结构化读结果。
func NewGetTaskInfoToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		taskID, ok := schedule.ArgsInt(args, "task_id")
		if !ok {
			return buildScheduleReadSimpleFailureResult("get_task_info", args, state, "查询失败：缺少必填参数 task_id。")
		}
		if state == nil {
			return buildScheduleReadSimpleFailureResult("get_task_info", args, nil, "查询失败：日程状态为空，无法读取任务详情。")
		}

		observation := schedule.GetTaskInfo(state, taskID)
		task := state.TaskByStateID(taskID)
		if task == nil {
			return buildScheduleReadSimpleFailureResult("get_task_info", args, state, observation)
		}

		slotItems := make([]map[string]any, 0, len(task.Slots))
		for _, slot := range cloneAndSortTaskSlots(task.Slots) {
			slotItems = append(slotItems, buildScheduleReadItem(
				formatScheduleDaySlotCN(state, slot.Day, slot.SlotStart, slot.SlotEnd),
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

		fields := []map[string]any{
			buildScheduleReadKV("类别", fallbackText(task.Category, "未分类")),
			buildScheduleReadKV("状态", formatScheduleTaskStatusCN(*task)),
			buildScheduleReadKV("来源", formatScheduleTaskSourceCN(*task)),
			buildScheduleReadKV("落位情况", buildTaskPlacementLabel(task)),
			buildScheduleReadKV("时长需求", buildTaskDurationLabel(task)),
		}
		if task.TaskClassID > 0 {
			fields = append(fields, buildScheduleReadKV("任务类 ID", fmt.Sprintf("%d", task.TaskClassID)))
		}
		if task.CanEmbed {
			fields = append(fields, buildScheduleReadKV("可作为宿主", "是"))
		}

		sections := []map[string]any{
			buildScheduleReadKVSection("基本信息", fields),
		}
		if len(slotItems) > 0 {
			sections = append(sections, buildScheduleReadItemsSection("占用时段", slotItems))
		}
		if relationLines := buildTaskRelationLines(state, task); len(relationLines) > 0 {
			sections = append(sections, buildScheduleReadCalloutSection("嵌入关系", "当前任务存在宿主/客体关系。", "info", relationLines))
		}
		appendSectionIfPresent(&sections, buildScheduleReadArgsSection("查询条件", LegacyResultWithState("get_task_info", args, state, observation).ArgumentView))

		return buildScheduleReadResult(
			"get_task_info",
			args,
			state,
			observation,
			ToolStatusDone,
			fmt.Sprintf("[%d]%s", task.StateID, fallbackText(task.Name, "未命名任务")),
			fmt.Sprintf("%s｜%s", fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(*task)),
			[]map[string]any{
				buildScheduleReadMetric("状态", formatScheduleTaskStatusCN(*task)),
				buildScheduleReadMetric("时长", buildTaskDurationLabel(task)),
				buildScheduleReadMetric("落位", buildTaskPlacementLabel(task)),
			},
			slotItems,
			sections,
			map[string]any{
				"task_id":       task.StateID,
				"source":        task.Source,
				"status":        task.Status,
				"task_class_id": task.TaskClassID,
				"can_embed":     task.CanEmbed,
				"embedded_by":   task.EmbeddedBy,
				"embed_host":    task.EmbedHost,
			},
		)
	}
}

func decodeTargetTasksPayload(observation string) (scheduleReadTargetTasksPayload, map[string]any) {
	var payload scheduleReadTargetTasksPayload
	_ = json.Unmarshal([]byte(strings.TrimSpace(observation)), &payload)
	raw, _ := parseObservationJSON(strings.TrimSpace(observation))
	return payload, raw
}

func buildTargetTasksSummarySubtitle(payload scheduleReadTargetTasksPayload) string {
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

func buildTargetTaskSubtitle(item scheduleReadTargetTaskRecord) string {
	return fmt.Sprintf("%s｜%s", fallbackText(item.Category, "未分类"), formatTargetTaskStatusCN(item.Status))
}

func buildTargetTaskTags(item scheduleReadTargetTaskRecord) []string {
	tags := []string{formatTargetTaskStatusCN(item.Status)}
	if item.Duration > 0 {
		tags = append(tags, fmt.Sprintf("%d 节", item.Duration))
	}
	if item.TaskClassID > 0 {
		tags = append(tags, fmt.Sprintf("任务类 %d", item.TaskClassID))
	}
	return tags
}

func buildTargetTaskDetailLines(state *schedule.ScheduleState, item scheduleReadTargetTaskRecord) []string {
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
