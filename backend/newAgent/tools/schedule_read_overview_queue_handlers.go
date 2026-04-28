package newagenttools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// NewGetOverviewToolHandler 为 get_overview 生成结构化读结果。
func NewGetOverviewToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		if state == nil {
			return buildScheduleReadSimpleFailureResult("get_overview", args, nil, "查看总览失败：日程状态为空，无法读取总览。")
		}

		observation := schedule.GetOverview(state)
		totalSlots := state.Window.TotalDays * 12
		totalOccupied := 0
		taskExistingCount := 0
		taskSuggestedCount := 0
		taskPendingCount := 0
		courseExistingCount := 0

		for i := range state.Tasks {
			task := state.Tasks[i]
			if task.EmbedHost == nil {
				for _, slot := range task.Slots {
					totalOccupied += slot.SlotEnd - slot.SlotStart + 1
				}
			}
			if isCourseScheduleTaskForRead(task) {
				if schedule.IsExistingTask(task) {
					courseExistingCount++
				}
				continue
			}
			switch {
			case schedule.IsPendingTask(task):
				taskPendingCount++
			case schedule.IsSuggestedTask(task):
				taskSuggestedCount++
			default:
				taskExistingCount++
			}
		}

		dailyItems := make([]map[string]any, 0, state.Window.TotalDays)
		for day := 1; day <= state.Window.TotalDays; day++ {
			totalDayOccupied := countScheduleDayOccupiedForRead(state, day)
			taskDayOccupied := countScheduleDayTaskOccupiedForRead(state, day)
			taskEntries := listScheduleTasksOnDayForRead(state, day, false)
			detailLines := make([]string, 0, len(taskEntries))
			for _, entry := range taskEntries {
				detailLines = append(detailLines, fmt.Sprintf(
					"[%d]%s｜%s｜%s",
					entry.Task.StateID,
					fallbackText(entry.Task.Name, "未命名任务"),
					formatScheduleTaskStatusCN(*entry.Task),
					formatScheduleSlotRangeCN(entry.SlotStart, entry.SlotEnd),
				))
			}
			if len(detailLines) == 0 {
				detailLines = append(detailLines, "当天没有任务明细。")
			}
			dailyItems = append(dailyItems, buildScheduleReadItem(
				formatScheduleDayCN(state, day),
				fmt.Sprintf("总占用 %d/12 节，任务占用 %d/12 节", totalDayOccupied, taskDayOccupied),
				[]string{fmt.Sprintf("任务 %d 项", len(taskEntries))},
				detailLines,
				map[string]any{"day": day},
			))
		}

		taskItems := make([]map[string]any, 0, len(state.Tasks))
		for i := range state.Tasks {
			task := state.Tasks[i]
			if isCourseScheduleTaskForRead(task) {
				continue
			}
			detailLines := []string{
				"时段：" + formatScheduleTaskSlotsBriefCN(state, task.Slots),
				"来源：" + formatScheduleTaskSourceCN(task),
			}
			if task.TaskClassID > 0 {
				detailLines = append(detailLines, fmt.Sprintf("任务类 ID：%d", task.TaskClassID))
			}
			taskItems = append(taskItems, buildScheduleReadItem(
				fmt.Sprintf("[%d]%s", task.StateID, fallbackText(task.Name, "未命名任务")),
				fmt.Sprintf("%s｜%s", fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(task)),
				[]string{formatScheduleTaskStatusCN(task)},
				detailLines,
				map[string]any{
					"task_id":       task.StateID,
					"task_class_id": task.TaskClassID,
					"status":        task.Status,
				},
			))
		}
		sort.Slice(taskItems, func(i, j int) bool {
			leftID, _ := toInt(taskItems[i]["meta"].(map[string]any)["task_id"])
			rightID, _ := toInt(taskItems[j]["meta"].(map[string]any)["task_id"])
			return leftID < rightID
		})

		taskClassItems := make([]map[string]any, 0, len(state.TaskClasses))
		for _, meta := range state.TaskClasses {
			detailLines := []string{
				fmt.Sprintf("排程策略：%s", formatTaskClassStrategyCN(meta.Strategy)),
				fmt.Sprintf("总预算：%d 节", meta.TotalSlots),
				fmt.Sprintf("允许嵌入水课：%s", formatBoolLabelCN(meta.AllowFillerCourse)),
			}
			if len(meta.ExcludedSlots) > 0 {
				detailLines = append(detailLines, "排除节次："+formatScheduleSectionListCN(meta.ExcludedSlots))
			}
			if len(meta.ExcludedDaysOfWeek) > 0 {
				detailLines = append(detailLines, "排除星期："+formatWeekdayListCN(meta.ExcludedDaysOfWeek))
			}
			taskClassItems = append(taskClassItems, buildScheduleReadItem(
				fallbackText(meta.Name, "未命名任务类"),
				formatTaskClassStrategyCN(meta.Strategy),
				nil,
				detailLines,
				map[string]any{
					"task_class_id": meta.ID,
					"strategy":      meta.Strategy,
				},
			))
		}

		totalFree := totalSlots - totalOccupied
		if totalFree < 0 {
			totalFree = 0
		}
		sections := []map[string]any{
			buildScheduleReadKVSection("窗口概况", []map[string]any{
				buildScheduleReadKV("规划天数", fmt.Sprintf("%d 天", state.Window.TotalDays)),
				buildScheduleReadKV("总时段", fmt.Sprintf("%d 节", totalSlots)),
				buildScheduleReadKV("已占用", fmt.Sprintf("%d 节", totalOccupied)),
				buildScheduleReadKV("空闲", fmt.Sprintf("%d 节", totalFree)),
				buildScheduleReadKV("课程占位", fmt.Sprintf("%d 项", courseExistingCount)),
				buildScheduleReadKV("已安排任务", fmt.Sprintf("%d 项", taskExistingCount)),
				buildScheduleReadKV("已预排任务", fmt.Sprintf("%d 项", taskSuggestedCount)),
				buildScheduleReadKV("待安排任务", fmt.Sprintf("%d 项", taskPendingCount)),
			}),
			buildScheduleReadItemsSection("每日概况", dailyItems),
			buildScheduleReadItemsSection("任务清单", taskItems),
		}
		if len(taskClassItems) > 0 {
			sections = append(sections, buildScheduleReadItemsSection("任务类约束", taskClassItems))
		}

		return buildScheduleReadResult(
			"get_overview",
			args,
			state,
			observation,
			ToolStatusDone,
			"当前排程总览",
			fmt.Sprintf("%d 天窗口，已占用 %d/%d 节，待安排 %d 项。", state.Window.TotalDays, totalOccupied, totalSlots, taskPendingCount),
			[]map[string]any{
				buildScheduleReadMetric("已占用", fmt.Sprintf("%d 节", totalOccupied)),
				buildScheduleReadMetric("空闲", fmt.Sprintf("%d 节", totalFree)),
				buildScheduleReadMetric("待安排", fmt.Sprintf("%d 项", taskPendingCount)),
				buildScheduleReadMetric("课程占位", fmt.Sprintf("%d 项", courseExistingCount)),
			},
			dailyItems,
			sections,
			map[string]any{
				"total_days":            state.Window.TotalDays,
				"total_slots":           totalSlots,
				"total_occupied":        totalOccupied,
				"task_existing_count":   taskExistingCount,
				"task_suggested_count":  taskSuggestedCount,
				"task_pending_count":    taskPendingCount,
				"course_existing_count": courseExistingCount,
			},
		)
	}
}

// NewQueueStatusToolHandler 为 queue_status 生成结构化读结果。
func NewQueueStatusToolHandler() ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) ToolExecutionResult {
		observation := schedule.QueueStatus(state, args)
		if state == nil {
			return buildScheduleReadSimpleFailureResult("queue_status", args, nil, observation)
		}

		payload, machinePayload := decodeQueueStatusPayload(observation)
		items := make([]map[string]any, 0, 1+len(payload.NextTaskIDs))
		sections := make([]map[string]any, 0, 4)
		if payload.Current != nil {
			currentItem := buildQueueCurrentItem(state, payload.Current, payload.CurrentAttempt)
			items = append(items, currentItem)
			sections = append(sections, buildScheduleReadItemsSection("当前处理", []map[string]any{currentItem}))
		}

		nextItems := make([]map[string]any, 0, len(payload.NextTaskIDs))
		for index, taskID := range payload.NextTaskIDs {
			nextItems = append(nextItems, buildQueuePendingItem(state, taskID, index))
		}
		items = append(items, nextItems...)
		if len(nextItems) > 0 {
			sections = append(sections, buildScheduleReadItemsSection("待处理队列", nextItems))
		}

		sections = append(sections, buildScheduleReadKVSection("运行概况", []map[string]any{
			buildScheduleReadKV("待处理", fmt.Sprintf("%d 项", payload.PendingCount)),
			buildScheduleReadKV("已完成", fmt.Sprintf("%d 项", payload.CompletedCount)),
			buildScheduleReadKV("已跳过", fmt.Sprintf("%d 项", payload.SkippedCount)),
			buildScheduleReadKV("当前任务", resolveTaskQueueLabelByID(state, payload.CurrentTaskID)),
		}))
		if strings.TrimSpace(payload.LastError) != "" {
			sections = append(sections, buildScheduleReadCalloutSection(
				"最近一次失败",
				"队列中保留了上一轮 apply 的失败原因。",
				"warning",
				[]string{strings.TrimSpace(payload.LastError)},
			))
		}

		title := fmt.Sprintf("队列待处理 %d 项", payload.PendingCount)
		if payload.PendingCount == 0 && payload.CurrentTaskID == 0 {
			title = "当前队列为空"
		}
		return buildScheduleReadResult(
			"queue_status",
			args,
			state,
			observation,
			ToolStatusDone,
			title,
			buildQueueStatusSubtitle(state, payload),
			[]map[string]any{
				buildScheduleReadMetric("待处理", fmt.Sprintf("%d 项", payload.PendingCount)),
				buildScheduleReadMetric("已完成", fmt.Sprintf("%d 项", payload.CompletedCount)),
				buildScheduleReadMetric("已跳过", fmt.Sprintf("%d 项", payload.SkippedCount)),
			},
			items,
			sections,
			machinePayload,
		)
	}
}

func decodeQueueStatusPayload(observation string) (scheduleReadQueueStatusPayload, map[string]any) {
	var payload scheduleReadQueueStatusPayload
	_ = json.Unmarshal([]byte(strings.TrimSpace(observation)), &payload)
	raw, _ := parseObservationJSON(strings.TrimSpace(observation))
	return payload, raw
}

func buildQueueStatusSubtitle(state *schedule.ScheduleState, payload scheduleReadQueueStatusPayload) string {
	if payload.Current != nil {
		return fmt.Sprintf(
			"当前处理：[%d]%s，第 %d 次尝试。",
			payload.Current.TaskID,
			fallbackText(payload.Current.Name, "未命名任务"),
			maxInt(payload.CurrentAttempt, 1),
		)
	}
	if payload.PendingCount > 0 {
		return fmt.Sprintf("队列里还有 %d 项待处理，尚未弹出当前任务。", payload.PendingCount)
	}
	return "没有待处理任务，也没有正在处理的任务。"
}

// 这里没有强抽成公共 task builder，因为 queue_status 既要兼容 payload 快照，
// 也要兼容通过 state 按 task_id 兜底，两类输入结构不同，硬抽反而会增加适配噪音。
func buildQueueCurrentItem(state *schedule.ScheduleState, payload *scheduleReadQueueTaskSnapshot, attempt int) map[string]any {
	detailLines := buildQueueCurrentDetailLines(state, payload)
	detailLines = append(detailLines, fmt.Sprintf("当前尝试：第 %d 次", maxInt(attempt, 1)))
	return buildScheduleReadItem(
		fmt.Sprintf("[%d]%s", payload.TaskID, fallbackText(payload.Name, "未命名任务")),
		buildQueueCurrentSubtitle(payload),
		[]string{"当前处理"},
		detailLines,
		map[string]any{
			"task_id":       payload.TaskID,
			"status":        payload.Status,
			"task_class_id": payload.TaskClassID,
		},
	)
}

func buildQueuePendingItem(state *schedule.ScheduleState, taskID int, index int) map[string]any {
	task := state.TaskByStateID(taskID)
	if task == nil {
		return buildScheduleReadItem(
			fmt.Sprintf("[%d]任务", taskID),
			fmt.Sprintf("队列第 %d 位", index+1),
			[]string{"待处理"},
			[]string{"当前状态快照中未找到更多任务详情。"},
			map[string]any{"task_id": taskID, "queue_index": index},
		)
	}
	return buildScheduleReadItem(
		fmt.Sprintf("[%d]%s", task.StateID, fallbackText(task.Name, "未命名任务")),
		buildQueueTaskSubtitle(task),
		buildQueueTaskTags(task, false),
		buildQueueTaskDetailLines(state, task),
		map[string]any{
			"task_id":     task.StateID,
			"queue_index": index,
			"status":      task.Status,
		},
	)
}

func buildQueueTaskSubtitle(task *schedule.ScheduleTask) string {
	if task == nil {
		return "待处理"
	}
	return fmt.Sprintf("%s｜%s", fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(*task))
}

func buildQueueTaskTags(task *schedule.ScheduleTask, isCurrent bool) []string {
	tags := []string{}
	if isCurrent {
		tags = append(tags, "当前处理")
	} else {
		tags = append(tags, "待处理")
	}
	if task != nil && task.Duration > 0 {
		tags = append(tags, fmt.Sprintf("%d 节", task.Duration))
	}
	return tags
}

func buildQueueTaskDetailLines(state *schedule.ScheduleState, task *schedule.ScheduleTask) []string {
	if task == nil {
		return nil
	}
	lines := []string{"时段：" + formatScheduleTaskSlotsBriefCN(state, task.Slots)}
	if task.TaskClassID > 0 {
		lines = append(lines, fmt.Sprintf("任务类 ID：%d", task.TaskClassID))
	}
	return lines
}

func buildQueueCurrentSubtitle(payload *scheduleReadQueueTaskSnapshot) string {
	if payload == nil {
		return "当前处理"
	}
	return fmt.Sprintf("%s｜%s", fallbackText(payload.Category, "未分类"), formatTargetTaskStatusCN(payload.Status))
}

func buildQueueCurrentDetailLines(state *schedule.ScheduleState, payload *scheduleReadQueueTaskSnapshot) []string {
	if payload == nil {
		return nil
	}
	lines := make([]string, 0, 3)
	if len(payload.Slots) > 0 {
		slotParts := make([]string, 0, len(payload.Slots))
		for _, slot := range payload.Slots {
			slotParts = append(slotParts, formatScheduleDaySlotCN(state, slot.Day, slot.SlotStart, slot.SlotEnd))
		}
		lines = append(lines, "时段："+strings.Join(slotParts, "；"))
	} else {
		lines = append(lines, "当前还未落位。")
	}
	if payload.TaskClassID > 0 {
		lines = append(lines, fmt.Sprintf("任务类 ID：%d", payload.TaskClassID))
	}
	if payload.Duration > 0 {
		lines = append(lines, fmt.Sprintf("时长需求：%d 节", payload.Duration))
	}
	return lines
}

func formatTaskClassStrategyCN(strategy string) string {
	switch strings.TrimSpace(strategy) {
	case "steady":
		return "均匀分布"
	case "rapid":
		return "集中突击"
	default:
		return fallbackText(strategy, "默认")
	}
}
