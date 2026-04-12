package schedule

// TaskProcessingQueue 表示 execute 阶段的“逐项处理队列”运行态。
//
// 职责边界：
// 1. PendingTaskIDs：尚未开始处理的候选任务；
// 2. CurrentTaskID：当前正在处理的队首任务（0 表示暂无）；
// 3. CompletedTaskIDs / SkippedTaskIDs：本轮处理结果归档；
// 4. LastError：最近一次 apply 失败的原因，供 LLM 下一轮决策参考。
type TaskProcessingQueue struct {
	PendingTaskIDs   []int  `json:"pending_task_ids,omitempty"`
	CurrentTaskID    int    `json:"current_task_id,omitempty"`
	CurrentAttempts  int    `json:"current_attempts,omitempty"`
	CompletedTaskIDs []int  `json:"completed_task_ids,omitempty"`
	SkippedTaskIDs   []int  `json:"skipped_task_ids,omitempty"`
	LastError        string `json:"last_error,omitempty"`
}

// ensureTaskProcessingQueue 确保 state 上有可用队列容器。
func ensureTaskProcessingQueue(state *ScheduleState) *TaskProcessingQueue {
	if state == nil {
		return nil
	}
	if state.RuntimeQueue == nil {
		state.RuntimeQueue = &TaskProcessingQueue{}
	}
	return state.RuntimeQueue
}

// ResetTaskProcessingQueue 清空本轮临时队列，供“新一轮执行开始”时调用。
func ResetTaskProcessingQueue(state *ScheduleState) {
	if state == nil {
		return
	}
	state.RuntimeQueue = nil
}

// ReplaceTaskProcessingQueue 用新的任务 ID 列表覆盖队列。
//
// 步骤化说明：
// 1. 先重置队列，避免上一次处理结果残留；
// 2. 对输入任务 ID 去重，防止 LLM 重复筛选造成同任务重复入队；
// 3. 不自动弹出当前任务，保持“显式 queue_pop_head 才开始处理”的流程约束。
func ReplaceTaskProcessingQueue(state *ScheduleState, taskIDs []int) int {
	queue := ensureTaskProcessingQueue(state)
	if queue == nil {
		return 0
	}
	queue.PendingTaskIDs = nil
	queue.CurrentTaskID = 0
	queue.CurrentAttempts = 0
	queue.CompletedTaskIDs = nil
	queue.SkippedTaskIDs = nil
	queue.LastError = ""
	return appendTaskIDsToQueue(state, taskIDs)
}

// appendTaskIDsToQueue 将任务追加到队列尾部并做去重，返回本次实际入队数量。
//
// 去重规则：
// 1. 与当前正在处理的任务去重；
// 2. 与 pending / completed / skipped 去重；
// 3. task_id<=0 直接忽略，避免无效数据污染队列。
func appendTaskIDsToQueue(state *ScheduleState, taskIDs []int) int {
	queue := ensureTaskProcessingQueue(state)
	if queue == nil || len(taskIDs) == 0 {
		return 0
	}

	exists := make(map[int]struct{}, len(queue.PendingTaskIDs)+len(queue.CompletedTaskIDs)+len(queue.SkippedTaskIDs)+1)
	if queue.CurrentTaskID > 0 {
		exists[queue.CurrentTaskID] = struct{}{}
	}
	for _, id := range queue.PendingTaskIDs {
		exists[id] = struct{}{}
	}
	for _, id := range queue.CompletedTaskIDs {
		exists[id] = struct{}{}
	}
	for _, id := range queue.SkippedTaskIDs {
		exists[id] = struct{}{}
	}

	added := 0
	for _, id := range taskIDs {
		if id <= 0 {
			continue
		}
		if _, ok := exists[id]; ok {
			continue
		}
		queue.PendingTaskIDs = append(queue.PendingTaskIDs, id)
		exists[id] = struct{}{}
		added++
	}
	return added
}

// popOrGetCurrentTaskID 返回当前可处理任务。
//
// 规则：
// 1. 若已有 CurrentTaskID，直接复用（保证 apply/skip 前不切换对象）；
// 2. 若 current 为空且 pending 非空，则弹出队首并设为 current；
// 3. 若队列为空，返回 0。
func popOrGetCurrentTaskID(state *ScheduleState) int {
	queue := ensureTaskProcessingQueue(state)
	if queue == nil {
		return 0
	}
	if queue.CurrentTaskID > 0 {
		return queue.CurrentTaskID
	}
	if len(queue.PendingTaskIDs) == 0 {
		return 0
	}
	queue.CurrentTaskID = queue.PendingTaskIDs[0]
	queue.PendingTaskIDs = queue.PendingTaskIDs[1:]
	queue.CurrentAttempts = 0
	queue.LastError = ""
	return queue.CurrentTaskID
}

// markCurrentTaskCompleted 将 current 任务标记为完成并清空 current。
func markCurrentTaskCompleted(state *ScheduleState) {
	queue := ensureTaskProcessingQueue(state)
	if queue == nil || queue.CurrentTaskID <= 0 {
		return
	}
	queue.CompletedTaskIDs = append(queue.CompletedTaskIDs, queue.CurrentTaskID)
	queue.CurrentTaskID = 0
	queue.CurrentAttempts = 0
	queue.LastError = ""
}

// markCurrentTaskSkipped 将 current 任务标记为跳过并清空 current。
func markCurrentTaskSkipped(state *ScheduleState) {
	queue := ensureTaskProcessingQueue(state)
	if queue == nil || queue.CurrentTaskID <= 0 {
		return
	}
	queue.SkippedTaskIDs = append(queue.SkippedTaskIDs, queue.CurrentTaskID)
	queue.CurrentTaskID = 0
	queue.CurrentAttempts = 0
	queue.LastError = ""
}

// bumpCurrentTaskAttempt 记录 current 任务一次失败尝试。
func bumpCurrentTaskAttempt(state *ScheduleState, errText string) {
	queue := ensureTaskProcessingQueue(state)
	if queue == nil || queue.CurrentTaskID <= 0 {
		return
	}
	queue.CurrentAttempts++
	queue.LastError = errText
}

// cloneTaskProcessingQueue 深拷贝 RuntimeQueue。
func cloneTaskProcessingQueue(src *TaskProcessingQueue) *TaskProcessingQueue {
	if src == nil {
		return nil
	}
	dst := &TaskProcessingQueue{
		CurrentTaskID:   src.CurrentTaskID,
		CurrentAttempts: src.CurrentAttempts,
		LastError:       src.LastError,
	}
	if len(src.PendingTaskIDs) > 0 {
		dst.PendingTaskIDs = append([]int(nil), src.PendingTaskIDs...)
	}
	if len(src.CompletedTaskIDs) > 0 {
		dst.CompletedTaskIDs = append([]int(nil), src.CompletedTaskIDs...)
	}
	if len(src.SkippedTaskIDs) > 0 {
		dst.SkippedTaskIDs = append([]int(nil), src.SkippedTaskIDs...)
	}
	return dst
}
