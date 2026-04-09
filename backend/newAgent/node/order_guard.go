package newagentnode

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

const (
	orderGuardStageName   = "order_guard"
	orderGuardStatusBlock = "order_guard.status"
)

type suggestedOrderItem struct {
	StateID   int
	Day       int
	SlotStart int
	SlotEnd   int
	Slots     []newagenttools.TaskSlot
}

type orderRestoreResult struct {
	Restored bool
	Changed  int
	Detail   string
}

// RunOrderGuardNode 负责在收口前校验 suggested 任务相对顺序是否被打乱。
//
// 职责边界：
// 1. 只做“相对顺序守卫”这一件事，不负责执行调度工具，也不负责写库；
// 2. 仅当 AllowReorder=false 时生效，用户明确授权可打乱顺序时直接放行；
// 3. 校验失败时优先“自动复原相对顺序”，由 Deliver 节点继续交付，不再直接终止。
func RunOrderGuardNode(ctx context.Context, st *newagentmodel.AgentGraphState) error {
	if st == nil {
		return fmt.Errorf("order_guard node: state is nil")
	}

	flowState := st.EnsureFlowState()
	if flowState == nil {
		return fmt.Errorf("order_guard node: flow state is nil")
	}
	// 1. 用户明确授权可打乱顺序时，顺序守卫节点直接放行。
	if flowState.AllowReorder {
		return nil
	}

	// 2. 读取当前 ScheduleState，提取 suggested 任务的“时间顺序快照”。
	scheduleState, err := st.EnsureScheduleState(ctx)
	if err != nil {
		return fmt.Errorf("order_guard node: load schedule state failed: %w", err)
	}
	if scheduleState == nil {
		return nil
	}
	currentOrder := buildSuggestedOrderSnapshot(scheduleState)

	// 3. 基线为空时，仅初始化基线并放行，避免第一次进入守卫就误判。
	if len(flowState.SuggestedOrderBaseline) == 0 {
		flowState.SuggestedOrderBaseline = append([]int(nil), currentOrder...)
		_ = st.EnsureChunkEmitter().EmitStatus(
			orderGuardStatusBlock,
			orderGuardStageName,
			"order_guard_initialized",
			"已记录本轮建议任务顺序基线，继续交付当前结果。",
			false,
		)
		return nil
	}

	// 4. 基线存在时做逆序检测；发现逆序后优先自动复原，而不是直接中止。
	violated, detail := detectRelativeOrderViolation(flowState.SuggestedOrderBaseline, currentOrder)
	if !violated {
		_ = st.EnsureChunkEmitter().EmitStatus(
			orderGuardStatusBlock,
			orderGuardStageName,
			"order_guard_passed",
			"顺序守卫校验通过，保持原有相对顺序。",
			false,
		)
		return nil
	}

	// 4.1 违序后进入自动复原：
	// 1) 复用“当前坑位集合”，按 baseline 相对顺序回填任务；
	// 2) 成功则继续 completed 路径，保证预览可写入；
	// 3) 若复原条件不满足，保守放行并输出诊断，避免再次把整轮流程打成 aborted。
	restore := restoreSuggestedOrderByBaseline(scheduleState, flowState.SuggestedOrderBaseline)
	if restore.Restored {
		_ = st.EnsureChunkEmitter().EmitStatus(
			orderGuardStatusBlock,
			orderGuardStageName,
			"order_guard_restored",
			fmt.Sprintf("检测到建议任务顺序被打乱，已自动复原（调整 %d 个任务）。", restore.Changed),
			false,
		)
		return nil
	}

	_ = st.EnsureChunkEmitter().EmitStatus(
		orderGuardStatusBlock,
		orderGuardStageName,
		"order_guard_restore_skipped",
		"检测到顺序异常，但本次未执行自动复原，已继续交付当前结果。详情见日志。",
		false,
	)
	log.Printf(
		"[WARN] order_guard restore skipped chat=%s baseline=%v current=%v detail=%s restore_detail=%s",
		flowState.ConversationID,
		flowState.SuggestedOrderBaseline,
		currentOrder,
		detail,
		restore.Detail,
	)
	return nil
}

// buildSuggestedOrderSnapshot 生成 suggested 任务的相对顺序快照（按时间坐标排序）。
//
// 说明：
// 1. 这里只关心 suggested 任务，因为顺序守卫目标是约束“本轮建议层”的相对次序；
// 2. 多 slot 任务取“最早 slot”作为排序锚点，保证排序键稳定；
// 3. 返回值是 state_id 列表，便于写入 CommonState 做跨节点持久化。
func buildSuggestedOrderSnapshot(state *newagenttools.ScheduleState) []int {
	items := buildSuggestedOrderItems(state)
	order := make([]int, 0, len(items))
	for _, item := range items {
		order = append(order, item.StateID)
	}
	return order
}

// buildSuggestedOrderItems 生成 suggested 任务的排序明细。
//
// 职责边界：
// 1. 统一封装顺序守卫和自动复原都需要的排序素材，避免两处逻辑口径漂移；
// 2. 排序键保持与历史实现一致：day -> slot_start -> slot_end -> state_id；
// 3. 每项附带完整 slots 快照，供“坑位复用式复原”直接使用。
func buildSuggestedOrderItems(state *newagenttools.ScheduleState) []suggestedOrderItem {
	if state == nil || len(state.Tasks) == 0 {
		return nil
	}

	items := make([]suggestedOrderItem, 0, len(state.Tasks))
	for i := range state.Tasks {
		task := state.Tasks[i]
		if !newagenttools.IsSuggestedTask(task) || len(task.Slots) == 0 {
			continue
		}
		day, slotStart, slotEnd := earliestTaskSlot(task.Slots)
		items = append(items, suggestedOrderItem{
			StateID:   task.StateID,
			Day:       day,
			SlotStart: slotStart,
			SlotEnd:   slotEnd,
			Slots:     cloneTaskSlots(task.Slots),
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Day != items[j].Day {
			return items[i].Day < items[j].Day
		}
		if items[i].SlotStart != items[j].SlotStart {
			return items[i].SlotStart < items[j].SlotStart
		}
		if items[i].SlotEnd != items[j].SlotEnd {
			return items[i].SlotEnd < items[j].SlotEnd
		}
		return items[i].StateID < items[j].StateID
	})

	return items
}

func earliestTaskSlot(slots []newagenttools.TaskSlot) (day int, slotStart int, slotEnd int) {
	if len(slots) == 0 {
		return 0, 0, 0
	}
	best := slots[0]
	for i := 1; i < len(slots); i++ {
		current := slots[i]
		if current.Day < best.Day {
			best = current
			continue
		}
		if current.Day == best.Day && current.SlotStart < best.SlotStart {
			best = current
			continue
		}
		if current.Day == best.Day && current.SlotStart == best.SlotStart && current.SlotEnd < best.SlotEnd {
			best = current
		}
	}
	return best.Day, best.SlotStart, best.SlotEnd
}

// detectRelativeOrderViolation 检查 current 是否破坏 baseline 的相对顺序。
//
// 规则：
// 1. 仅比较 baseline 与 current 的交集任务，避免新增/删除任务引发误报；
// 2. 一旦出现 rank 逆序即判定为 violation；
// 3. detail 只用于内部排查，不直接给用户。
func detectRelativeOrderViolation(baseline []int, current []int) (bool, string) {
	if len(baseline) == 0 || len(current) == 0 {
		return false, ""
	}

	rankByID := make(map[int]int, len(baseline))
	for idx, id := range baseline {
		rankByID[id] = idx
	}

	filtered := make([]int, 0, len(current))
	for _, id := range current {
		if _, ok := rankByID[id]; ok {
			filtered = append(filtered, id)
		}
	}
	if len(filtered) < 2 {
		return false, ""
	}

	prevID := filtered[0]
	prevRank := rankByID[prevID]
	for i := 1; i < len(filtered); i++ {
		id := filtered[i]
		rank := rankByID[id]
		if rank < prevRank {
			return true, strings.TrimSpace(fmt.Sprintf(
				"reverse pair detected: prev_id=%d prev_rank=%d current_id=%d current_rank=%d",
				prevID, prevRank, id, rank,
			))
		}
		prevID = id
		prevRank = rank
	}
	return false, ""
}

// restoreSuggestedOrderByBaseline 在“默认不允许打乱顺序”场景下自动复原 suggested 相对顺序。
//
// 步骤化说明：
// 1. 先提取 baseline 与 current 的交集任务，确保只修复本轮可比对对象；
// 2. 复用 current 的“坑位序列”（时段集合），按 baseline 顺序重新回填任务；
// 3. 回填前校验时长兼容，避免把长任务塞进短坑位；
// 4. 回填后再次校验顺序；若失败则回滚，保证状态不会半成功。
func restoreSuggestedOrderByBaseline(state *newagenttools.ScheduleState, baseline []int) orderRestoreResult {
	if state == nil {
		return orderRestoreResult{Restored: false, Detail: "schedule_state=nil"}
	}
	if len(baseline) == 0 {
		return orderRestoreResult{Restored: true}
	}

	items := buildSuggestedOrderItems(state)
	if len(items) < 2 {
		return orderRestoreResult{Restored: true}
	}

	itemByID := make(map[int]suggestedOrderItem, len(items))
	currentInScope := make([]int, 0, len(items))
	for _, item := range items {
		itemByID[item.StateID] = item
	}
	for _, item := range items {
		if _, ok := itemByID[item.StateID]; ok {
			currentInScope = append(currentInScope, item.StateID)
		}
	}

	baselineInScope := make([]int, 0, len(baseline))
	for _, id := range baseline {
		if _, ok := itemByID[id]; ok {
			baselineInScope = append(baselineInScope, id)
		}
	}
	if len(baselineInScope) < 2 {
		return orderRestoreResult{Restored: true}
	}

	// currentInScope 只保留 baseline 交集，保证两边长度一致且语义可比。
	baselineSet := make(map[int]struct{}, len(baselineInScope))
	for _, id := range baselineInScope {
		baselineSet[id] = struct{}{}
	}
	filteredCurrent := make([]int, 0, len(currentInScope))
	for _, id := range currentInScope {
		if _, ok := baselineSet[id]; ok {
			filteredCurrent = append(filteredCurrent, id)
		}
	}
	if sameIDOrder(filteredCurrent, baselineInScope) {
		return orderRestoreResult{Restored: true}
	}
	if len(filteredCurrent) != len(baselineInScope) {
		return orderRestoreResult{
			Restored: false,
			Detail:   fmt.Sprintf("size_mismatch baseline=%d current=%d", len(baselineInScope), len(filteredCurrent)),
		}
	}

	// 1. 先构建“当前坑位序列”。
	slotPool := make([][]newagenttools.TaskSlot, 0, len(filteredCurrent))
	for _, currentID := range filteredCurrent {
		item, ok := itemByID[currentID]
		if !ok {
			return orderRestoreResult{
				Restored: false,
				Detail:   fmt.Sprintf("current_id_missing id=%d", currentID),
			}
		}
		slotPool = append(slotPool, cloneTaskSlots(item.Slots))
	}

	// 2. 回填前做兼容性校验：默认要求“目标任务时长 == 坑位时长”。
	for i, targetID := range baselineInScope {
		targetTask := state.TaskByStateID(targetID)
		if targetTask == nil {
			return orderRestoreResult{
				Restored: false,
				Detail:   fmt.Sprintf("target_task_missing id=%d", targetID),
			}
		}
		if !isSlotsCompatibleWithTask(*targetTask, slotPool[i]) {
			return orderRestoreResult{
				Restored: false,
				Detail: fmt.Sprintf(
					"slot_incompatible target=%d expected_duration=%d slot_duration=%d expected_segments=%d slot_segments=%d",
					targetID,
					expectedTaskDuration(*targetTask),
					totalSlotDuration(slotPool[i]),
					len(targetTask.Slots),
					len(slotPool[i]),
				),
			}
		}
	}

	// 3. 执行回填，并在失败时支持回滚。
	beforeSlots := make(map[int][]newagenttools.TaskSlot, len(baselineInScope))
	changed := 0
	for i, targetID := range baselineInScope {
		task := state.TaskByStateID(targetID)
		if task == nil {
			continue
		}
		beforeSlots[targetID] = cloneTaskSlots(task.Slots)
		targetSlots := cloneTaskSlots(slotPool[i])
		if !equalTaskSlots(task.Slots, targetSlots) {
			task.Slots = targetSlots
			changed++
		}
	}

	afterOrder := buildSuggestedOrderSnapshot(state)
	afterFiltered := make([]int, 0, len(afterOrder))
	for _, id := range afterOrder {
		if _, ok := baselineSet[id]; ok {
			afterFiltered = append(afterFiltered, id)
		}
	}
	if !sameIDOrder(afterFiltered, baselineInScope) {
		// 回滚，避免保留半成功状态。
		for _, targetID := range baselineInScope {
			task := state.TaskByStateID(targetID)
			if task == nil {
				continue
			}
			task.Slots = cloneTaskSlots(beforeSlots[targetID])
		}
		return orderRestoreResult{
			Restored: false,
			Detail: fmt.Sprintf(
				"restore_verify_failed expected=%v actual=%v",
				baselineInScope, afterFiltered,
			),
		}
	}

	return orderRestoreResult{
		Restored: true,
		Changed:  changed,
	}
}

func sameIDOrder(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func cloneTaskSlots(slots []newagenttools.TaskSlot) []newagenttools.TaskSlot {
	if len(slots) == 0 {
		return nil
	}
	copied := make([]newagenttools.TaskSlot, len(slots))
	copy(copied, slots)
	return copied
}

func equalTaskSlots(left, right []newagenttools.TaskSlot) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i].Day != right[i].Day {
			return false
		}
		if left[i].SlotStart != right[i].SlotStart {
			return false
		}
		if left[i].SlotEnd != right[i].SlotEnd {
			return false
		}
	}
	return true
}

func expectedTaskDuration(task newagenttools.ScheduleTask) int {
	if task.Duration > 0 {
		return task.Duration
	}
	if len(task.Slots) > 0 {
		return totalSlotDuration(task.Slots)
	}
	return 0
}

func totalSlotDuration(slots []newagenttools.TaskSlot) int {
	total := 0
	for _, slot := range slots {
		total += slot.SlotEnd - slot.SlotStart + 1
	}
	return total
}

func isSlotsCompatibleWithTask(task newagenttools.ScheduleTask, slots []newagenttools.TaskSlot) bool {
	if len(slots) == 0 {
		return false
	}
	expectedDuration := expectedTaskDuration(task)
	if expectedDuration > 0 && expectedDuration != totalSlotDuration(slots) {
		return false
	}
	// 兼容策略：当前任务已有多段落位时，要求目标坑位段数一致，避免跨段语义被破坏。
	if len(task.Slots) > 0 && len(task.Slots) != len(slots) {
		return false
	}
	return true
}
