package schedule

import "fmt"

// validateLocalOrderForSinglePlacement 校验单个任务落到目标时段后，是否仍满足同任务类内部顺序约束。
//
// 职责边界：
// 1. 只负责“同任务类内部顺序”这一条规则，不负责冲突、锁定、范围合法性；
// 2. 采用“克隆态 + 假设落位”方式校验，避免直接污染真实 state；
// 3. 若任务不属于 task_item / 缺少 task_order / 当前无边界约束，直接放行。
func validateLocalOrderForSinglePlacement(state *ScheduleState, taskID int, targetSlots []TaskSlot) error {
	if len(targetSlots) == 0 {
		return nil
	}
	return validateLocalOrderBatchPlacement(state, map[int][]TaskSlot{
		taskID: cloneScheduleTaskSlots(targetSlots),
	})
}

// validateLocalOrderBatchPlacement 在“多任务同时变更”的假设下做顺序约束校验。
//
// 职责边界：
// 1. 先把所有候选落位一次性写入克隆态，再统一校验，避免 swap/batch/spread_even 出现伪冲突；
// 2. 只校验 proposals 中涉及的任务，因为只要这些任务仍处于各自前驱/后继之间，就不会破坏同类整体顺序；
// 3. 返回首个命中的中文错误，供写工具直接透传给 LLM。
func validateLocalOrderBatchPlacement(state *ScheduleState, proposals map[int][]TaskSlot) error {
	if state == nil || len(proposals) == 0 {
		return nil
	}

	clone := state.Clone()
	for taskID, slots := range proposals {
		task := clone.TaskByStateID(taskID)
		if task == nil {
			return fmt.Errorf("顺序约束校验失败：任务ID %d 不存在", taskID)
		}
		task.Slots = cloneScheduleTaskSlots(slots)
	}

	for taskID := range proposals {
		if err := validateTaskLocalOrderOnState(clone, taskID); err != nil {
			return err
		}
	}
	return nil
}

// validateTaskLocalOrderOnState 判断某个任务在当前假设态下，是否仍处于同任务类前驱/后继之间。
func validateTaskLocalOrderOnState(state *ScheduleState, taskID int) error {
	task := state.TaskByStateID(taskID)
	if task == nil {
		return fmt.Errorf("顺序约束校验失败：任务ID %d 不存在", taskID)
	}
	if !shouldEnforceTaskLocalOrder(*task) || len(task.Slots) == 0 {
		return nil
	}

	prevTask, nextTask := findTaskClassNeighbors(state, *task)
	targetStartDay, targetStartSlot, _ := earliestScheduleTaskSlot(task.Slots)
	targetEndDay, _, targetEndSlot := latestScheduleTaskSlot(task.Slots)

	if prevTask != nil && len(prevTask.Slots) > 0 {
		prevEndDay, _, prevEndSlot := latestScheduleTaskSlot(prevTask.Slots)
		if !isStrictlyAfter(targetStartDay, targetStartSlot, prevEndDay, prevEndSlot) {
			return fmt.Errorf(
				"顺序约束不满足：[%d]%s 不能放到%s。它必须晚于同任务类前一个任务 %s 的结束位置（%s）。",
				task.StateID,
				task.Name,
				formatTaskSlotsBriefWithState(state, task.Slots),
				formatTaskLabel(*prevTask),
				formatTaskSlotsBriefWithState(state, prevTask.Slots),
			)
		}
	}

	if nextTask != nil && len(nextTask.Slots) > 0 {
		nextStartDay, nextStartSlot, _ := earliestScheduleTaskSlot(nextTask.Slots)
		if !isStrictlyBefore(targetEndDay, targetEndSlot, nextStartDay, nextStartSlot) {
			return fmt.Errorf(
				"顺序约束不满足：[%d]%s 不能放到%s。它必须早于同任务类后一个任务 %s 的开始位置（%s）。",
				task.StateID,
				task.Name,
				formatTaskSlotsBriefWithState(state, task.Slots),
				formatTaskLabel(*nextTask),
				formatTaskSlotsBriefWithState(state, nextTask.Slots),
			)
		}
	}

	return nil
}

// shouldEnforceTaskLocalOrder 判断任务是否需要参与“同任务类内部顺序”约束。
func shouldEnforceTaskLocalOrder(task ScheduleTask) bool {
	return task.Source == "task_item" && task.TaskClassID > 0 && task.TaskOrder > 0
}

// findTaskClassNeighbors 查找同任务类中 order 紧邻当前任务的前驱与后继。
func findTaskClassNeighbors(state *ScheduleState, task ScheduleTask) (prevTask *ScheduleTask, nextTask *ScheduleTask) {
	if state == nil || !shouldEnforceTaskLocalOrder(task) {
		return nil, nil
	}

	for i := range state.Tasks {
		candidate := &state.Tasks[i]
		if candidate.StateID == task.StateID {
			continue
		}
		if !shouldEnforceTaskLocalOrder(*candidate) {
			continue
		}
		if candidate.TaskClassID != task.TaskClassID {
			continue
		}

		if candidate.TaskOrder < task.TaskOrder {
			if prevTask == nil || candidate.TaskOrder > prevTask.TaskOrder {
				prevTask = candidate
			}
			continue
		}
		if candidate.TaskOrder > task.TaskOrder {
			if nextTask == nil || candidate.TaskOrder < nextTask.TaskOrder {
				nextTask = candidate
			}
		}
	}
	return prevTask, nextTask
}

func earliestScheduleTaskSlot(slots []TaskSlot) (day int, slotStart int, slotEnd int) {
	if len(slots) == 0 {
		return 0, 0, 0
	}
	best := slots[0]
	for i := 1; i < len(slots); i++ {
		current := slots[i]
		if current.Day < best.Day ||
			(current.Day == best.Day && current.SlotStart < best.SlotStart) ||
			(current.Day == best.Day && current.SlotStart == best.SlotStart && current.SlotEnd < best.SlotEnd) {
			best = current
		}
	}
	return best.Day, best.SlotStart, best.SlotEnd
}

func latestScheduleTaskSlot(slots []TaskSlot) (day int, slotStart int, slotEnd int) {
	if len(slots) == 0 {
		return 0, 0, 0
	}
	best := slots[0]
	for i := 1; i < len(slots); i++ {
		current := slots[i]
		if current.Day > best.Day ||
			(current.Day == best.Day && current.SlotEnd > best.SlotEnd) ||
			(current.Day == best.Day && current.SlotEnd == best.SlotEnd && current.SlotStart > best.SlotStart) {
			best = current
		}
	}
	return best.Day, best.SlotStart, best.SlotEnd
}

func isStrictlyAfter(dayA, slotA, dayB, slotB int) bool {
	if dayA != dayB {
		return dayA > dayB
	}
	return slotA > slotB
}

func isStrictlyBefore(dayA, slotA, dayB, slotB int) bool {
	if dayA != dayB {
		return dayA < dayB
	}
	return slotA < slotB
}

func cloneScheduleTaskSlots(src []TaskSlot) []TaskSlot {
	if len(src) == 0 {
		return nil
	}
	dst := make([]TaskSlot, len(src))
	copy(dst, src)
	return dst
}
