package schedule

import "slices"

// 任务状态常量。
//
// 说明：
// 1. existing 表示“数据库里已经存在的已安排事实”，例如课程表事件、已持久化任务块；
// 2. suggested 表示“当前轮内存态里的建议落位”，来源可能是粗排结果，也可能是用户确认后的工具预排；
// 3. pending 表示“仍未落位的真实待安排任务”。
const (
	TaskStatusExisting  = "existing"
	TaskStatusSuggested = "suggested"
	TaskStatusPending   = "pending"
)

// IsPendingTask 判断任务是否属于“真实待安排”状态。
//
// 并行迁移说明：
// 1. 只有 pending 且没有 Slots，才视为真正未落位；
// 2. 旧快照里可能存在“pending 但已有 Slots”的粗排遗留形态，这类任务不应继续算作待安排；
// 3. 这样可以在不强制清洗旧快照的前提下，先把新旧语义统一到“pending=无落位”。
func IsPendingTask(task ScheduleTask) bool {
	return task.Status == TaskStatusPending && len(task.Slots) == 0
}

// IsSuggestedTask 判断任务是否属于“建议落位 / 可优化”状态。
//
// 并行迁移说明：
// 1. 新语义使用显式 suggested 状态；
// 2. 兼容旧 rough_build 快照：pending + Slots 视为 suggested；
// 3. 兼容旧 place 快照：existing + source=task_item + Duration>0 + Slots 视为 suggested。
func IsSuggestedTask(task ScheduleTask) bool {
	if len(task.Slots) == 0 {
		return false
	}
	if task.Status == TaskStatusSuggested {
		return true
	}
	if task.Status == TaskStatusPending {
		return true
	}
	if task.Status == TaskStatusExisting && task.Source == "task_item" && task.Duration > 0 {
		return true
	}
	return false
}

// IsExistingTask 判断任务是否属于“已确定事实层”。
//
// 说明：
// 1. 这里会主动排除 suggested 兼容形态，避免旧快照里的 existing+Duration>0 被误当成已确定任务；
// 2. 这样 get_overview 等工具才能稳定区分”事实层 existing”和”建议层 suggested”。
func IsExistingTask(task ScheduleTask) bool {
	return task.Status == TaskStatusExisting && !IsSuggestedTask(task)
}

// IsPlacedTask 判断任务当前是否已经拥有可操作的落位。
//
// 说明：
// 1. existing 和 suggested 都属于“已落位”；
// 2. pending 只有在并行迁移兼容形态（pending + Slots）下，才会被 IsSuggestedTask 吸收进来。
func IsPlacedTask(task ScheduleTask) bool {
	return IsExistingTask(task) || IsSuggestedTask(task)
}

// IsTaskInRequestedClassScope 判断 task_item 是否属于“本轮请求涉及的任务类范围”。
//
// 说明：
//  1. task_class_ids 为空时，视为不做范围裁剪，统一返回 true；
//  2. 仅 source=task_item 才有 task_class_id 语义，event 不参与该判断；
//  3. 迁移期若 task_item 缺失 TaskClassID，则在有显式 scope 时按“不在范围内”处理，
//     避免把域外 pending 误混进本轮粗排/微调。
func IsTaskInRequestedClassScope(task ScheduleTask, taskClassIDs []int) bool {
	if len(taskClassIDs) == 0 {
		return true
	}
	if task.Source != "task_item" {
		return false
	}
	return task.TaskClassID > 0 && slices.Contains(taskClassIDs, task.TaskClassID)
}

// FilterScheduleStateForTaskClassScope 按“本轮请求的任务类范围”裁剪工具态里的域外 pending。
//
// 步骤说明：
// 1. existing / suggested 一律保留，因为它们已经是事实层或建议层落位，会参与冲突判断；
// 2. 仅移除“域外真实 pending”，避免粗排校验和读工具把别的任务类误算进来；
// 3. TaskClasses 元数据也同步按 scope 裁剪，避免 prompt/工具读到无关约束；
// 4. 这里做就地裁剪，调用方无需再维护第二份 scoped state。
func FilterScheduleStateForTaskClassScope(state *ScheduleState, taskClassIDs []int) {
	if state == nil || len(taskClassIDs) == 0 {
		return
	}

	filteredTasks := make([]ScheduleTask, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		if !IsPendingTask(task) {
			filteredTasks = append(filteredTasks, task)
			continue
		}
		if IsTaskInRequestedClassScope(task, taskClassIDs) {
			filteredTasks = append(filteredTasks, task)
		}
	}
	state.Tasks = filteredTasks

	if len(state.TaskClasses) == 0 {
		return
	}
	filteredMetas := make([]TaskClassMeta, 0, len(state.TaskClasses))
	for _, meta := range state.TaskClasses {
		if slices.Contains(taskClassIDs, meta.ID) {
			filteredMetas = append(filteredMetas, meta)
		}
	}
	state.TaskClasses = filteredMetas
}
