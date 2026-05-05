package schedule

import (
	"encoding/json"
	"fmt"
	"strings"
)

type queueTaskSlot struct {
	Day       int `json:"day"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	SlotStart int `json:"slot_start"`
	SlotEnd   int `json:"slot_end"`
}

type queueTaskItem struct {
	TaskID      int             `json:"task_id"`
	Name        string          `json:"name"`
	Category    string          `json:"category,omitempty"`
	Status      string          `json:"status"`
	Duration    int             `json:"duration,omitempty"`
	TaskClassID int             `json:"task_class_id,omitempty"`
	Slots       []queueTaskSlot `json:"slots,omitempty"`
}

type queuePopHeadResult struct {
	Tool           string         `json:"tool"`
	HasHead        bool           `json:"has_head"`
	PendingCount   int            `json:"pending_count"`
	CompletedCount int            `json:"completed_count"`
	SkippedCount   int            `json:"skipped_count"`
	Current        *queueTaskItem `json:"current,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
}

type queueApplyHeadMoveResult struct {
	Tool           string `json:"tool"`
	Success        bool   `json:"success"`
	TaskID         int    `json:"task_id,omitempty"`
	CurrentAttempt int    `json:"current_attempt,omitempty"`
	PendingCount   int    `json:"pending_count"`
	CompletedCount int    `json:"completed_count"`
	SkippedCount   int    `json:"skipped_count"`
	Result         string `json:"result"`
}

type queueSkipHeadResult struct {
	Tool          string `json:"tool"`
	Success       bool   `json:"success"`
	SkippedTaskID int    `json:"skipped_task_id,omitempty"`
	PendingCount  int    `json:"pending_count"`
	SkippedCount  int    `json:"skipped_count"`
	Reason        string `json:"reason,omitempty"`
}

type queueStatusResult struct {
	Tool           string         `json:"tool"`
	PendingCount   int            `json:"pending_count"`
	CompletedCount int            `json:"completed_count"`
	SkippedCount   int            `json:"skipped_count"`
	CurrentTaskID  int            `json:"current_task_id,omitempty"`
	CurrentAttempt int            `json:"current_attempt,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	NextTaskIDs    []int          `json:"next_task_ids,omitempty"`
	Current        *queueTaskItem `json:"current,omitempty"`
}

// QueuePopHead 从队列弹出队首任务（若已有 current 则复用），并返回当前处理对象。
//
// 步骤化说明：
// 1. 先保证队列容器存在，避免空指针；
// 2. 若 current 已存在，直接复用，确保 apply/skip 前不会切换处理对象；
// 3. 若 current 为空则从 pending 弹出队首；
// 4. 若没有可处理任务，返回 has_head=false，由 LLM 收口或重筛选。
func QueuePopHead(state *ScheduleState, _ map[string]any) string {
	if state == nil {
		return `{"tool":"queue_pop_head","has_head":false,"error":"state is nil"}`
	}
	queue := ensureTaskProcessingQueue(state)
	taskID := popOrGetCurrentTaskID(state)

	result := queuePopHeadResult{
		Tool:           "queue_pop_head",
		HasHead:        taskID > 0,
		PendingCount:   len(queue.PendingTaskIDs),
		CompletedCount: len(queue.CompletedTaskIDs),
		SkippedCount:   len(queue.SkippedTaskIDs),
		LastError:      strings.TrimSpace(queue.LastError),
	}
	if taskID > 0 {
		result.Current = buildQueueTaskItem(state, taskID)
	}
	return mustJSON(result, "queue_pop_head")
}

// QueueApplyHeadMove 将当前队首任务移动到指定位置，成功后自动完成并出队。
//
// 步骤化说明：
// 1. 只能处理 current 任务，禁止越级指定 task_id，避免 LLM 绕过队列直接乱改；
// 2. 成功时标记 completed 并清空 current；
// 3. 失败时保留 current 并累加 attempt，让 LLM 继续换坑位重试或 skip。
func QueueApplyHeadMove(state *ScheduleState, args map[string]any) string {
	if state == nil {
		return `{"tool":"queue_apply_head_move","success":false,"result":"state is nil"}`
	}
	queue := ensureTaskProcessingQueue(state)
	currentID := queue.CurrentTaskID
	if currentID <= 0 {
		return mustJSON(queueApplyHeadMoveResult{
			Tool:           "queue_apply_head_move",
			Success:        false,
			PendingCount:   len(queue.PendingTaskIDs),
			CompletedCount: len(queue.CompletedTaskIDs),
			SkippedCount:   len(queue.SkippedTaskIDs),
			Result:         "队列中没有正在处理的任务。请先调用 queue_pop_head。",
		}, "queue_apply_head_move")
	}

	newDay, ok := ArgsInt(args, "new_day")
	if !ok {
		return mustJSON(queueApplyHeadMoveResult{
			Tool:           "queue_apply_head_move",
			Success:        false,
			TaskID:         currentID,
			CurrentAttempt: queue.CurrentAttempts,
			PendingCount:   len(queue.PendingTaskIDs),
			CompletedCount: len(queue.CompletedTaskIDs),
			SkippedCount:   len(queue.SkippedTaskIDs),
			Result:         "缺少必填参数 new_day。",
		}, "queue_apply_head_move")
	}
	newSlotStart, ok := ArgsInt(args, "new_slot_start")
	if !ok {
		return mustJSON(queueApplyHeadMoveResult{
			Tool:           "queue_apply_head_move",
			Success:        false,
			TaskID:         currentID,
			CurrentAttempt: queue.CurrentAttempts,
			PendingCount:   len(queue.PendingTaskIDs),
			CompletedCount: len(queue.CompletedTaskIDs),
			SkippedCount:   len(queue.SkippedTaskIDs),
			Result:         "缺少必填参数 new_slot_start。",
		}, "queue_apply_head_move")
	}

	// 1. 真正执行仍复用既有 move 校验链路，避免重复实现一套冲突判断。
	// 2. 失败时仅更新队列 attempt，不改 current，确保同一任务可继续重试。
	resultText := Move(state, currentID, newDay, newSlotStart)
	success := !strings.Contains(resultText, "移动失败")
	if success {
		markCurrentTaskCompleted(state)
	} else {
		bumpCurrentTaskAttempt(state, resultText)
	}

	queue = ensureTaskProcessingQueue(state)
	return mustJSON(queueApplyHeadMoveResult{
		Tool:           "queue_apply_head_move",
		Success:        success,
		TaskID:         currentID,
		CurrentAttempt: queue.CurrentAttempts,
		PendingCount:   len(queue.PendingTaskIDs),
		CompletedCount: len(queue.CompletedTaskIDs),
		SkippedCount:   len(queue.SkippedTaskIDs),
		Result:         strings.TrimSpace(resultText),
	}, "queue_apply_head_move")
}

// QueueSkipHead 跳过当前队首任务。
//
// 职责边界：
// 1. 只修改队列运行态，不改排程结果；
// 2. current 必须存在，否则返回失败提示；
// 3. 跳过后由下一轮 queue_pop_head 继续取下一项。
func QueueSkipHead(state *ScheduleState, args map[string]any) string {
	if state == nil {
		return `{"tool":"queue_skip_head","success":false,"reason":"state is nil"}`
	}
	queue := ensureTaskProcessingQueue(state)
	currentID := queue.CurrentTaskID
	if currentID <= 0 {
		return mustJSON(queueSkipHeadResult{
			Tool:         "queue_skip_head",
			Success:      false,
			PendingCount: len(queue.PendingTaskIDs),
			SkippedCount: len(queue.SkippedTaskIDs),
			Reason:       "没有可跳过的 current 任务，请先 queue_pop_head。",
		}, "queue_skip_head")
	}

	reason := ""
	if raw, ok := ArgsString(args, "reason"); ok {
		reason = strings.TrimSpace(raw)
	}
	markCurrentTaskSkipped(state)
	queue = ensureTaskProcessingQueue(state)
	return mustJSON(queueSkipHeadResult{
		Tool:          "queue_skip_head",
		Success:       true,
		SkippedTaskID: currentID,
		PendingCount:  len(queue.PendingTaskIDs),
		SkippedCount:  len(queue.SkippedTaskIDs),
		Reason:        reason,
	}, "queue_skip_head")
}

// QueueStatus 查询当前队列状态。
func QueueStatus(state *ScheduleState, _ map[string]any) string {
	if state == nil {
		return `{"tool":"queue_status","pending_count":0,"completed_count":0,"skipped_count":0,"last_error":"state is nil"}`
	}
	queue := ensureTaskProcessingQueue(state)
	nextIDs := queue.PendingTaskIDs
	if len(nextIDs) > 5 {
		nextIDs = nextIDs[:5]
	}

	result := queueStatusResult{
		Tool:           "queue_status",
		PendingCount:   len(queue.PendingTaskIDs),
		CompletedCount: len(queue.CompletedTaskIDs),
		SkippedCount:   len(queue.SkippedTaskIDs),
		CurrentTaskID:  queue.CurrentTaskID,
		CurrentAttempt: queue.CurrentAttempts,
		LastError:      strings.TrimSpace(queue.LastError),
		NextTaskIDs:    append([]int(nil), nextIDs...),
	}
	if queue.CurrentTaskID > 0 {
		result.Current = buildQueueTaskItem(state, queue.CurrentTaskID)
	}
	return mustJSON(result, "queue_status")
}

// buildQueueTaskItem 构造队列任务快照，供 pop/status 返回。
func buildQueueTaskItem(state *ScheduleState, taskID int) *queueTaskItem {
	task := state.TaskByStateID(taskID)
	if task == nil {
		return nil
	}
	item := &queueTaskItem{
		TaskID:      task.StateID,
		Name:        strings.TrimSpace(task.Name),
		Category:    strings.TrimSpace(task.Category),
		Status:      buildTaskStatusLabel(*task),
		Duration:    task.Duration,
		TaskClassID: task.TaskClassID,
		Slots:       make([]queueTaskSlot, 0, len(task.Slots)),
	}
	for _, slot := range task.Slots {
		week, dayOfWeek, ok := state.DayToWeekDay(slot.Day)
		if !ok {
			continue
		}
		item.Slots = append(item.Slots, queueTaskSlot{
			Day:       slot.Day,
			Week:      week,
			DayOfWeek: dayOfWeek,
			SlotStart: slot.SlotStart,
			SlotEnd:   slot.SlotEnd,
		})
	}
	return item
}

func mustJSON(v any, toolName string) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf(`{"tool":"%s","success":false,"error":"json encode failed"}`, toolName)
	}
	return string(raw)
}
