package newagentnode

import (
	"context"
	"fmt"
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
}

// RunOrderGuardNode 负责在收口前校验 suggested 任务相对顺序是否被打乱。
//
// 职责边界：
// 1. 只做“相对顺序守卫”这一件事，不负责执行调度工具，也不负责写库；
// 2. 仅当 AllowReorder=false 时生效，用户明确授权可打乱顺序时直接放行；
// 3. 校验失败只写入统一终止结果（Abort），由 Deliver 节点统一收口文案。
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

	// 4. 基线存在时做逆序检测；一旦发现逆序，立即终止本轮自动微调。
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

	userMessage := "检测到当前方案打乱了原有建议任务顺序，本轮先停止自动微调。若你确认可以打乱顺序，请明确说明“允许打乱顺序”。"
	flowState.Abort(
		orderGuardStageName,
		"relative_order_violation",
		userMessage,
		fmt.Sprintf("baseline=%v current=%v detail=%s", flowState.SuggestedOrderBaseline, currentOrder, detail),
	)
	_ = st.EnsureChunkEmitter().EmitStatus(
		orderGuardStatusBlock,
		orderGuardStageName,
		"order_guard_failed",
		userMessage,
		true,
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

	order := make([]int, 0, len(items))
	for _, item := range items {
		order = append(order, item.StateID)
	}
	return order
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
