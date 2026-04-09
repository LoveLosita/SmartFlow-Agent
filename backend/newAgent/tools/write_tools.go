package newagenttools

import (
	"fmt"
	"strings"
)

// ==================== 写工具：LLM 通过这些函数修改日程状态 ====================
// 所有写工具：
//   - 只修改内存中的 ScheduleState，不直接写库
//   - 先校验后修改，校验失败则 state 不变，返回错误信息
//   - 返回自然语言描述变更结果 + 涉及天的占用摘要

// MoveRequest 是 BatchMove 的单条移动请求。
type MoveRequest struct {
	TaskID       int `json:"task_id"`
	NewDay       int `json:"new_day"`
	NewSlotStart int `json:"new_slot_start"`
}

const (
	// maxBatchMoveSize 是 batch_move 的安全上限。
	//
	// 设计说明：
	// 1. 旧链路中 batch_move 容易因组合冲突导致“整批回滚 + 连续重试”；
	// 2. 先把批量规模限制在 2，作为止血策略，降低一次决策的冲突面；
	// 3. 更大规模的调整应优先走队列化逐项处理（queue_pop_head + queue_apply_head_move）。
	maxBatchMoveSize = 2
)

// ==================== Place ====================

// Place 将一个待安排任务预排到指定位置。
// taskID 必须是真实 pending（无 Slots）状态的任务。
// 如果目标位置有可嵌入宿主（can_embed=true 且未被嵌入），自动走嵌入逻辑。
func Place(state *ScheduleState, taskID, day, slotStart int) string {
	// 1. 查找任务。
	task := state.TaskByStateID(taskID)
	if task == nil {
		return fmt.Sprintf("放置失败：任务ID %d 不存在。", taskID)
	}

	// 2. 校验状态。
	// 2.1 只有“真实 pending”才允许 place；
	// 2.2 suggested / existing 都说明任务已经有落位，继续 place 会破坏当前方案语义；
	// 2.3 旧快照里的 pending+Slots 也会被 IsPendingTask 排除，避免重复补排。
	if !IsPendingTask(*task) {
		return fmt.Sprintf("放置失败：[%d]%s 不是待安排任务，无法放置。", task.StateID, task.Name)
	}

	// 3. 计算目标范围并校验。
	slotEnd := slotStart + task.Duration - 1
	if err := validateDay(state, day); err != nil {
		return fmt.Sprintf("放置失败：%s", err.Error())
	}
	if err := validateSlotRange(slotStart, slotEnd); err != nil {
		return fmt.Sprintf("放置失败：%s", err.Error())
	}

	// 4. 冲突检测。
	conflict := findConflict(state, day, slotStart, slotEnd)
	if conflict != nil {
		// 锁定任务的冲突给出特殊提示。
		if conflict.Locked {
			return fmt.Sprintf("放置失败：%s已被 [%d]%s（固定）占用。\n%s\n%s",
				formatDaySlotLabel(state, day, slotStart, slotEnd), conflict.StateID, conflict.Name,
				formatDayOccupancy(state, day), formatFreeHint(state, day))
		}
		return fmt.Sprintf("放置失败：%s已被 [%d]%s 占用。\n%s\n%s",
			formatDaySlotLabel(state, day, slotStart, slotEnd), conflict.StateID, conflict.Name,
			formatDayOccupancy(state, day), formatFreeHint(state, day))
	}

	// 5. 检查是否有可嵌入宿主。
	host := findEmbedHost(state, day, slotStart, slotEnd)

	// 6. 执行变更。
	if host != nil {
		// 嵌入路径：设置双向嵌入关系，并把任务提升为 suggested。
		guestID := task.StateID
		hostID := host.StateID
		task.EmbedHost = &hostID
		host.EmbeddedBy = &guestID
		task.Slots = []TaskSlot{{Day: day, SlotStart: slotStart, SlotEnd: slotEnd}}
		task.Status = TaskStatusSuggested

		return fmt.Sprintf("已将 [%d]%s 预排并嵌入到%s（宿主：[%d]%s）。\n%s\n待安排任务剩余：%d个。",
			task.StateID, task.Name, formatDaySlotLabel(state, day, slotStart, slotEnd),
			host.StateID, host.Name,
			formatDayOccupancy(state, day), countPending(state))
	}

	// 普通路径：直接放置，并标记为 suggested。
	task.Slots = []TaskSlot{{Day: day, SlotStart: slotStart, SlotEnd: slotEnd}}
	task.Status = TaskStatusSuggested

	return fmt.Sprintf("已将 [%d]%s 预排到%s。\n%s\n待安排任务剩余：%d个。",
		task.StateID, task.Name, formatDaySlotLabel(state, day, slotStart, slotEnd),
		formatDayOccupancy(state, day), countPending(state))
}

// ==================== Move ====================

// Move 将一个已落位任务移动到新位置。
// taskID 仅允许 suggested；existing/pending 都不允许移动。
func Move(state *ScheduleState, taskID, newDay, newSlotStart int) string {
	// 1. 查找任务。
	task := state.TaskByStateID(taskID)
	if task == nil {
		return fmt.Sprintf("移动失败：任务ID %d 不存在。", taskID)
	}

	// 2. 校验状态。
	if !IsSuggestedTask(*task) {
		// 2.1 pending 任务尚未落位，应通过 place 安排；
		// 2.2 existing 任务属于已安排事实层，不允许在 execute 微调里直接 move；
		// 2.3 仅 suggested 属于“本轮可微调建议落位”。
		if IsPendingTask(*task) {
			return fmt.Sprintf("移动失败：[%d]%s 当前为待安排状态，请使用 place 放置。", task.StateID, task.Name)
		}
		return fmt.Sprintf("移动失败：[%d]%s 当前为已安排（existing）任务，不允许 move；仅 suggested 任务可移动。", task.StateID, task.Name)
	}

	// 3. 校验锁定。
	if err := checkLocked(*task); err != nil {
		return fmt.Sprintf("移动失败：%s", err.Error())
	}

	// 4. 计算新范围。
	duration := taskDuration(*task)
	newSlotEnd := newSlotStart + duration - 1

	if err := validateDay(state, newDay); err != nil {
		return fmt.Sprintf("移动失败：%s", err.Error())
	}
	if err := validateSlotRange(newSlotStart, newSlotEnd); err != nil {
		return fmt.Sprintf("移动失败：%s", err.Error())
	}

	// 5. 冲突检测（排除自身）。
	conflict := findConflict(state, newDay, newSlotStart, newSlotEnd, taskID)
	if conflict != nil {
		return fmt.Sprintf("移动失败：%s已被 [%d]%s 占用。\n%s\n%s",
			formatDaySlotLabel(state, newDay, newSlotStart, newSlotEnd), conflict.StateID, conflict.Name,
			formatDayOccupancy(state, newDay), formatFreeHint(state, newDay))
	}

	// 6. 记录旧位置。
	oldSlots := make([]TaskSlot, len(task.Slots))
	copy(oldSlots, task.Slots)
	oldDesc := formatTaskSlotsBriefWithState(state, oldSlots)

	// 7. 执行变更。
	task.Slots = []TaskSlot{{Day: newDay, SlotStart: newSlotStart, SlotEnd: newSlotEnd}}

	// 8. 收集涉及的天（去重）。
	affectedDays := collectAffectedDays(oldSlots, task.Slots)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("已将 [%d]%s 从%s移至%s。\n",
		task.StateID, task.Name, oldDesc, formatDaySlotLabel(state, newDay, newSlotStart, newSlotEnd)))
	for _, d := range affectedDays {
		sb.WriteString(formatDayOccupancy(state, d) + "\n")
	}
	return sb.String()
}

// ==================== Swap ====================

// Swap 交换两个已落位任务的位置。
// 两个任务都必须是 suggested / existing、非锁定、总时长相同。
func Swap(state *ScheduleState, taskAID, taskBID int) string {
	// 1. 查找两个任务。
	taskA := state.TaskByStateID(taskAID)
	if taskA == nil {
		return fmt.Sprintf("交换失败：任务ID %d 不存在。", taskAID)
	}
	taskB := state.TaskByStateID(taskBID)
	if taskB == nil {
		return fmt.Sprintf("交换失败：任务ID %d 不存在。", taskBID)
	}

	if taskAID == taskBID {
		return "交换失败：不能与自己交换。"
	}

	// 2. 校验状态。
	if !IsPlacedTask(*taskA) {
		return fmt.Sprintf("交换失败：[%d]%s 不是已落位任务。", taskA.StateID, taskA.Name)
	}
	if !IsPlacedTask(*taskB) {
		return fmt.Sprintf("交换失败：[%d]%s 不是已落位任务。", taskB.StateID, taskB.Name)
	}

	// 3. 校验锁定。
	if err := checkLocked(*taskA); err != nil {
		return fmt.Sprintf("交换失败：%s", err.Error())
	}
	if err := checkLocked(*taskB); err != nil {
		return fmt.Sprintf("交换失败：%s", err.Error())
	}

	// 4. 校验时长。
	durA := taskDuration(*taskA)
	durB := taskDuration(*taskB)
	if durA != durB {
		return fmt.Sprintf("交换失败：[%d]%s 占%d个时段，[%d]%s 占%d个时段，时长不同无法直接交换。",
			taskA.StateID, taskA.Name, durA, taskB.StateID, taskB.Name, durB)
	}

	// 5. 记录旧位置。
	oldSlotsA := make([]TaskSlot, len(taskA.Slots))
	copy(oldSlotsA, taskA.Slots)
	oldSlotsB := make([]TaskSlot, len(taskB.Slots))
	copy(oldSlotsB, taskB.Slots)

	// 6. 交换 Slots。
	taskA.Slots, taskB.Slots = taskB.Slots, taskA.Slots

	// 7. 交换后冲突检测：A 的新位置（原 B 的位置）是否有第三方冲突。
	// 需要排除 B（因为 B 现在在 A 的旧位置，已经被 swap 了）。
	for _, slot := range taskA.Slots {
		conflict := findConflict(state, slot.Day, slot.SlotStart, slot.SlotEnd, taskAID, taskBID)
		if conflict != nil {
			// 回滚
			taskA.Slots = oldSlotsA
			taskB.Slots = oldSlotsB
			return fmt.Sprintf("交换失败：[%d]%s 的新位置%s与 [%d]%s 冲突。",
				taskA.StateID, taskA.Name, formatDaySlotLabel(state, slot.Day, slot.SlotStart, slot.SlotEnd),
				conflict.StateID, conflict.Name)
		}
	}
	for _, slot := range taskB.Slots {
		conflict := findConflict(state, slot.Day, slot.SlotStart, slot.SlotEnd, taskAID, taskBID)
		if conflict != nil {
			// 回滚
			taskA.Slots = oldSlotsA
			taskB.Slots = oldSlotsB
			return fmt.Sprintf("交换失败：[%d]%s 的新位置%s与 [%d]%s 冲突。",
				taskB.StateID, taskB.Name, formatDaySlotLabel(state, slot.Day, slot.SlotStart, slot.SlotEnd),
				conflict.StateID, conflict.Name)
		}
	}

	// 8. 成功输出。
	affectedDays := collectAffectedDays(oldSlotsA, taskA.Slots)
	affectedDays = append(affectedDays, collectAffectedDays(oldSlotsB, taskB.Slots)...)
	affectedDays = uniqueSorted(affectedDays)

	var sb strings.Builder
	sb.WriteString("交换完成：\n")
	sb.WriteString(fmt.Sprintf("  [%d]%s：%s → %s\n",
		taskA.StateID, taskA.Name,
		formatTaskSlotsBriefWithState(state, oldSlotsA), formatTaskSlotsBriefWithState(state, taskA.Slots)))
	sb.WriteString(fmt.Sprintf("  [%d]%s：%s → %s\n",
		taskB.StateID, taskB.Name,
		formatTaskSlotsBriefWithState(state, oldSlotsB), formatTaskSlotsBriefWithState(state, taskB.Slots)))
	for _, d := range affectedDays {
		sb.WriteString(formatDayOccupancy(state, d) + "\n")
	}
	return sb.String()
}

// ==================== BatchMove ====================

// BatchMove 原子性地批量移动多个任务。
// moves 中每个 task_id 都必须是 suggested；existing/pending 任一命中都会整批失败。
// 全部成功才生效，任一失败则完全回滚。
func BatchMove(state *ScheduleState, moves []MoveRequest) string {
	if len(moves) == 0 {
		return "批量移动失败：移动列表为空。"
	}
	if len(moves) > maxBatchMoveSize {
		return fmt.Sprintf("批量移动失败：当前最多支持 %d 条移动请求。请改用队列化逐项处理（queue_pop_head + queue_apply_head_move）。", maxBatchMoveSize)
	}

	// 1. 全量校验阶段（不改 state）。
	for i, m := range moves {
		task := state.TaskByStateID(m.TaskID)
		if task == nil {
			return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n任务ID %d 不存在（第%d条移动请求）。", m.TaskID, i+1)
		}
		if !IsSuggestedTask(*task) {
			// 1.1 保持与 Move 一致：批量移动仅允许 suggested；
			// 1.2 pending / existing 任一命中都应整批失败并回滚。
			if IsPendingTask(*task) {
				return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n[%d]%s 当前为待安排状态，请使用 place（第%d条移动请求）。",
					task.StateID, task.Name, i+1)
			}
			return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n[%d]%s 当前为已安排（existing）任务，不允许 move；仅 suggested 任务可移动（第%d条移动请求）。",
				task.StateID, task.Name, i+1)
		}
		if err := checkLocked(*task); err != nil {
			return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n%s（第%d条移动请求）", err.Error(), i+1)
		}

		duration := taskDuration(*task)
		newSlotEnd := m.NewSlotStart + duration - 1
		if err := validateDay(state, m.NewDay); err != nil {
			return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n%s（第%d条移动请求）", err.Error(), i+1)
		}
		if err := validateSlotRange(m.NewSlotStart, newSlotEnd); err != nil {
			return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n%s（第%d条移动请求）", err.Error(), i+1)
		}
	}

	// 2. 克隆 state，在克隆上执行。
	clone := state.Clone()

	// 收集涉及的天。
	affectedDays := make(map[int]bool)

	// 3. 逐个应用 + 冲突检测。
	for _, m := range moves {
		task := clone.TaskByStateID(m.TaskID)
		duration := taskDuration(*task)
		newSlotEnd := m.NewSlotStart + duration - 1

		// 记录旧位置涉及的天。
		for _, slot := range task.Slots {
			affectedDays[slot.Day] = true
		}

		// 冲突检测（在 clone 的中间状态上，排除自身）。
		conflict := findConflict(clone, m.NewDay, m.NewSlotStart, newSlotEnd, m.TaskID)
		if conflict != nil {
			return fmt.Sprintf("批量移动失败，全部回滚，无任何变更。\n冲突：[%d]%s → %s，该位置已被 [%d]%s 占用。",
				task.StateID, task.Name, formatDaySlotLabel(state, m.NewDay, m.NewSlotStart, newSlotEnd),
				conflict.StateID, conflict.Name)
		}

		// 应用移动。
		task.Slots = []TaskSlot{{Day: m.NewDay, SlotStart: m.NewSlotStart, SlotEnd: newSlotEnd}}
		affectedDays[m.NewDay] = true
	}

	// 4. 全部成功，将 clone 的数据写回原 state。
	state.Tasks = clone.Tasks

	// 5. 输出结果。
	days := sortedKeys(affectedDays)
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("批量移动完成，%d个任务全部成功：\n", len(moves)))
	for _, m := range moves {
		task := state.TaskByStateID(m.TaskID)
		duration := taskDuration(*task)
		sb.WriteString(fmt.Sprintf("  [%d]%s → %s\n",
			task.StateID, task.Name,
			formatDaySlotLabel(state, m.NewDay, m.NewSlotStart, m.NewSlotStart+duration-1)))
	}
	for _, d := range days {
		sb.WriteString(formatDayOccupancy(state, d) + "\n")
	}
	return sb.String()
}

// ==================== Unplace ====================

// Unplace 将一个已落位任务移除，恢复为待安排状态。
// taskID 允许是 suggested / existing，但不能是真实 pending。
// 如果任务有嵌入关系，会自动清理双向指针。
func Unplace(state *ScheduleState, taskID int) string {
	// 1. 查找任务。
	task := state.TaskByStateID(taskID)
	if task == nil {
		return fmt.Sprintf("移除失败：任务ID %d 不存在。", taskID)
	}

	// 2. 校验状态。
	if IsPendingTask(*task) {
		return fmt.Sprintf("移除失败：[%d]%s 已经是待安排状态。", task.StateID, task.Name)
	}

	// 3. 校验锁定。
	if err := checkLocked(*task); err != nil {
		return fmt.Sprintf("移除失败：%s", err.Error())
	}

	// 4. 记录旧位置。
	oldSlots := make([]TaskSlot, len(task.Slots))
	copy(oldSlots, task.Slots)
	oldDesc := formatTaskSlotsBriefWithState(state, oldSlots)

	// 5. 清理嵌入关系。
	// 如果该任务嵌入到了某个宿主上，清除宿主的 EmbeddedBy。
	if task.EmbedHost != nil {
		host := state.TaskByStateID(*task.EmbedHost)
		if host != nil {
			host.EmbeddedBy = nil
		}
		task.EmbedHost = nil
	}
	// 如果该任务是一个宿主且有嵌入客人，将客人也恢复为 pending。
	if task.EmbeddedBy != nil {
		guest := state.TaskByStateID(*task.EmbeddedBy)
		if guest != nil {
			// 先从嵌入时设置的 Slots 推算 Duration，再清空。
			// Place 嵌入时 guest.Slots 被设置为实际占用范围，这里从中恢复时长。
			if len(guest.Slots) > 0 {
				guest.Duration = taskDuration(*guest)
			}
			guest.EmbedHost = nil
			guest.Slots = nil
			guest.Status = TaskStatusPending
		}
		task.EmbeddedBy = nil
	}

	// 6. 执行变更。
	task.Slots = nil
	task.Status = TaskStatusPending

	// 7. 收集涉及的天。
	affectedDays := collectAffectedDaysFromSlots(oldSlots)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("已将 [%d]%s 从%s移除，恢复为待安排状态。\n",
		task.StateID, task.Name, oldDesc))
	for _, d := range affectedDays {
		sb.WriteString(formatDayOccupancy(state, d) + "\n")
	}
	sb.WriteString(fmt.Sprintf("待安排任务剩余：%d个。", countPending(state)))
	return sb.String()
}
