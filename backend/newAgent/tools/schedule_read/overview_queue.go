package schedule_read

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// BuildOverviewView 构造 get_overview 的纯展示视图。
func BuildOverviewView(input OverviewViewInput) ReadResultView {
	if input.State == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "get_overview",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	totalSlots := input.State.Window.TotalDays * 12
	totalOccupied := 0
	taskExistingCount := 0
	taskSuggestedCount := 0
	taskPendingCount := 0
	courseExistingCount := 0

	for i := range input.State.Tasks {
		task := input.State.Tasks[i]
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

	dailyItems := make([]ItemView, 0, input.State.Window.TotalDays)
	for day := 1; day <= input.State.Window.TotalDays; day++ {
		totalDayOccupied := countScheduleDayOccupiedForRead(input.State, day)
		taskDayOccupied := countScheduleDayTaskOccupiedForRead(input.State, day)
		taskEntries := listScheduleTasksOnDayForRead(input.State, day, false)
		detailLines := make([]string, 0, len(taskEntries))
		for _, entry := range taskEntries {
			detailLines = append(detailLines, fmt.Sprintf(
				"[%d]%s，%s，%s",
				entry.Task.StateID,
				fallbackText(entry.Task.Name, "未命名任务"),
				formatScheduleTaskStatusCN(*entry.Task),
				formatScheduleSlotRangeCN(entry.SlotStart, entry.SlotEnd),
			))
		}
		if len(detailLines) == 0 {
			detailLines = append(detailLines, "当天没有任务明细。")
		}
		dailyItems = append(dailyItems, BuildItem(
			formatScheduleDayCN(input.State, day),
			fmt.Sprintf("总占用 %d/12 节，任务占用 %d/12 节", totalDayOccupied, taskDayOccupied),
			[]string{fmt.Sprintf("任务 %d 项", len(taskEntries))},
			detailLines,
			map[string]any{"day": day},
		))
	}

	taskItems := make([]ItemView, 0, len(input.State.Tasks))
	for i := range input.State.Tasks {
		task := input.State.Tasks[i]
		if isCourseScheduleTaskForRead(task) {
			continue
		}
		detailLines := []string{
			"时段：" + formatScheduleTaskSlotsBriefCN(input.State, task.Slots),
			"来源：" + formatScheduleTaskSourceCN(task),
		}
		if task.TaskClassID > 0 {
			detailLines = append(detailLines, fmt.Sprintf("任务类 ID：%d", task.TaskClassID))
		}
		taskItems = append(taskItems, BuildItem(
			fmt.Sprintf("[%d]%s", task.StateID, fallbackText(task.Name, "未命名任务")),
			fmt.Sprintf("%s，%s", fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(task)),
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
		leftID, _ := toInt(taskItems[i].Meta["task_id"])
		rightID, _ := toInt(taskItems[j].Meta["task_id"])
		return leftID < rightID
	})

	taskClassItems := make([]ItemView, 0, len(input.State.TaskClasses))
	for _, meta := range input.State.TaskClasses {
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
		taskClassItems = append(taskClassItems, BuildItem(
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
		BuildKVSection("窗口概况", []KVField{
			BuildKVField("规划天数", fmt.Sprintf("%d 天", input.State.Window.TotalDays)),
			BuildKVField("总时段", fmt.Sprintf("%d 节", totalSlots)),
			BuildKVField("已占用", fmt.Sprintf("%d 节", totalOccupied)),
			BuildKVField("空闲", fmt.Sprintf("%d 节", totalFree)),
			BuildKVField("课程占位", fmt.Sprintf("%d 项", courseExistingCount)),
			BuildKVField("已安排任务", fmt.Sprintf("%d 项", taskExistingCount)),
			BuildKVField("已预排任务", fmt.Sprintf("%d 项", taskSuggestedCount)),
			BuildKVField("待安排任务", fmt.Sprintf("%d 项", taskPendingCount)),
		}),
		BuildItemsSection("每日概况", dailyItems),
		BuildItemsSection("任务清单", taskItems),
	}
	if len(taskClassItems) > 0 {
		sections = append(sections, BuildItemsSection("任务类约束", taskClassItems))
	}
	appendSectionIfPresent(&sections, BuildArgsSection("查询条件", input.ArgFields))

	return BuildResultView(BuildResultViewInput{
		Status:   StatusDone,
		Title:    "当前排程总览",
		Subtitle: fmt.Sprintf("%d 天窗口，已占用 %d/%d 节，待安排 %d 项。", input.State.Window.TotalDays, totalOccupied, totalSlots, taskPendingCount),
		Metrics: []MetricField{
			BuildMetric("已占用", fmt.Sprintf("%d 节", totalOccupied)),
			BuildMetric("空闲", fmt.Sprintf("%d 节", totalFree)),
			BuildMetric("待安排", fmt.Sprintf("%d 项", taskPendingCount)),
			BuildMetric("课程占位", fmt.Sprintf("%d 项", courseExistingCount)),
		},
		Items:       dailyItems,
		Sections:    sections,
		Observation: input.Observation,
		MachinePayload: map[string]any{
			"total_days":            input.State.Window.TotalDays,
			"total_slots":           totalSlots,
			"total_occupied":        totalOccupied,
			"task_existing_count":   taskExistingCount,
			"task_suggested_count":  taskSuggestedCount,
			"task_pending_count":    taskPendingCount,
			"course_existing_count": courseExistingCount,
		},
	})
}

// BuildQueueStatusView 构造 queue_status 的纯展示视图。
func BuildQueueStatusView(input QueueStatusViewInput) ReadResultView {
	if input.State == nil {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "queue_status",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	payload, machinePayload, ok := DecodeQueueStatusPayload(input.Observation)
	if !ok {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "queue_status",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	items := make([]ItemView, 0, 1+len(payload.NextTaskIDs))
	sections := make([]map[string]any, 0, 4)
	if payload.Current != nil {
		currentItem := buildQueueCurrentItem(input.State, payload.Current, payload.CurrentAttempt)
		items = append(items, currentItem)
		sections = append(sections, BuildItemsSection("当前处理", []ItemView{currentItem}))
	}

	nextItems := make([]ItemView, 0, len(payload.NextTaskIDs))
	for index, taskID := range payload.NextTaskIDs {
		nextItems = append(nextItems, buildQueuePendingItem(input.State, taskID, index))
	}
	items = append(items, nextItems...)
	if len(nextItems) > 0 {
		sections = append(sections, BuildItemsSection("待处理队列", nextItems))
	}

	sections = append(sections, BuildKVSection("运行概况", []KVField{
		BuildKVField("待处理", fmt.Sprintf("%d 项", payload.PendingCount)),
		BuildKVField("已完成", fmt.Sprintf("%d 项", payload.CompletedCount)),
		BuildKVField("已跳过", fmt.Sprintf("%d 项", payload.SkippedCount)),
		BuildKVField("当前任务", resolveTaskQueueLabelByID(input.State, payload.CurrentTaskID)),
	}))
	if strings.TrimSpace(payload.LastError) != "" {
		sections = append(sections, BuildCalloutSection(
			"最近一次失败",
			"队列中保留了上一轮 apply 的失败原因。",
			"warning",
			[]string{strings.TrimSpace(payload.LastError)},
		))
	}
	appendSectionIfPresent(&sections, BuildArgsSection("查询条件", input.ArgFields))

	title := fmt.Sprintf("队列待处理 %d 项", payload.PendingCount)
	if payload.PendingCount == 0 && payload.CurrentTaskID == 0 {
		title = "当前队列为空"
	}
	return BuildResultView(BuildResultViewInput{
		Status:         StatusDone,
		Title:          title,
		Subtitle:       buildQueueStatusSubtitle(payload),
		Metrics:        []MetricField{BuildMetric("待处理", fmt.Sprintf("%d 项", payload.PendingCount)), BuildMetric("已完成", fmt.Sprintf("%d 项", payload.CompletedCount)), BuildMetric("已跳过", fmt.Sprintf("%d 项", payload.SkippedCount))},
		Items:          items,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: machinePayload,
	})
}

// DecodeQueueStatusPayload 解析 queue_status 的 JSON observation。
func DecodeQueueStatusPayload(observation string) (QueueStatusPayload, map[string]any, bool) {
	var payload QueueStatusPayload
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

func buildQueueStatusSubtitle(payload QueueStatusPayload) string {
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

// 1. 这里没有强抽成通用 task builder，因为 queue_status 既要兼容 payload 快照，
// 2. 也要兼容通过 state 按 task_id 兜底，两类输入结构不同，硬抽反而会增加适配噪音。
func buildQueueCurrentItem(state *schedule.ScheduleState, payload *QueueTaskSnapshot, attempt int) ItemView {
	detailLines := buildQueueCurrentDetailLines(state, payload)
	detailLines = append(detailLines, fmt.Sprintf("当前尝试：第 %d 次", maxInt(attempt, 1)))
	return BuildItem(
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

func buildQueuePendingItem(state *schedule.ScheduleState, taskID int, index int) ItemView {
	task := state.TaskByStateID(taskID)
	if task == nil {
		return BuildItem(
			fmt.Sprintf("[%d]任务", taskID),
			fmt.Sprintf("队列第 %d 位", index+1),
			[]string{"待处理"},
			[]string{"当前状态快照中未找到更多任务详情。"},
			map[string]any{"task_id": taskID, "queue_index": index},
		)
	}
	return BuildItem(
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
	return fmt.Sprintf("%s，%s", fallbackText(task.Category, "未分类"), formatScheduleTaskStatusCN(*task))
}

func buildQueueTaskTags(task *schedule.ScheduleTask, isCurrent bool) []string {
	tags := make([]string, 0, 2)
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

func buildQueueCurrentSubtitle(payload *QueueTaskSnapshot) string {
	if payload == nil {
		return "当前处理"
	}
	return fmt.Sprintf("%s，%s", fallbackText(payload.Category, "未分类"), formatTargetTaskStatusCN(payload.Status))
}

func buildQueueCurrentDetailLines(state *schedule.ScheduleState, payload *QueueTaskSnapshot) []string {
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
