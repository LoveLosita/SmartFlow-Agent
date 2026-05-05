package planning

import (
	"testing"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

// newTestGrid 创建仅用于单测的最小 grid。
//
// 职责边界：
// 1. 只负责初始化时间窗口与 data 容器；
// 2. 不负责填充节次状态（由各测试用例自行设置）。
func newTestGrid(startWeek, startDay, endWeek, endDay int) *grid {
	return &grid{
		data:      make(map[int]map[int][13]slotNode),
		startWeek: startWeek,
		startDay:  startDay,
		endWeek:   endWeek,
		endDay:    endDay,
	}
}

// setDayStatus 批量设置某一天 1~12 节的状态。
func setDayStatus(g *grid, week, day int, status slotStatus) {
	for s := 1; s <= 12; s++ {
		g.setNode(week, day, s, slotNode{Status: status})
	}
}

// setSectionStatus 设置单个节次状态。
func setSectionStatus(g *grid, week, day, section int, status slotStatus) {
	g.setNode(week, day, section, slotNode{Status: status})
}

// TestComputeAllocation_SkipIsolatedOneSlot 验证“孤立 1 节”不会被错误写成任务。
//
// 用例意图：
// 1. 第一天只放一个孤立可用节次（10 节），后继 11 节被屏蔽；
// 2. 第二天提供一个合法的连续 2 节（1-2 节）；
// 3. 期望算法跳过第一天孤立节次，把任务落到第二天 1-2 节。
func TestComputeAllocation_SkipIsolatedOneSlot(t *testing.T) {
	g := newTestGrid(1, 1, 1, 2)

	// 1. 先全部置为 Blocked，避免默认 Free 干扰本用例。
	setDayStatus(g, 1, 1, Blocked)
	setDayStatus(g, 1, 2, Blocked)

	// 2. 构造“孤立 1 节 + 合法 2 节”场景。
	setSectionStatus(g, 1, 1, 10, Free) // 第一天仅 10 节可用，11/12 仍然 Blocked。
	setSectionStatus(g, 1, 2, 1, Free)
	setSectionStatus(g, 1, 2, 2, Free)

	items := []model.TaskClassItem{{ID: 1}}
	got, err := computeAllocation(g, items, "rapid")
	if err != nil {
		t.Fatalf("期望分配成功，实际报错: %v", err)
	}
	if len(got) != 1 || got[0].EmbeddedTime == nil {
		t.Fatalf("期望回填 1 条 EmbeddedTime，实际: %+v", got)
	}

	tt := got[0].EmbeddedTime
	if tt.Week != 1 || tt.DayOfWeek != 2 || tt.SectionFrom != 1 || tt.SectionTo != 2 {
		t.Fatalf("期望落位到 W1D2 1-2 节，实际: week=%d day=%d from=%d to=%d",
			tt.Week, tt.DayOfWeek, tt.SectionFrom, tt.SectionTo)
	}
}

// TestComputeAllocation_RejectAllIsolatedSlots 验证“全是孤立 1 节”时应返回时间不足。
//
// 用例意图：
// 1. 虽然总可用节次数量达到 2，但它们分散成两个孤立 1 节；
// 2. 业务要求普通任务默认必须 2 连续节，因此应整体失败而不是偷偷降级为 1 节。
func TestComputeAllocation_RejectAllIsolatedSlots(t *testing.T) {
	g := newTestGrid(1, 1, 1, 2)

	// 1. 先全部置为 Blocked。
	setDayStatus(g, 1, 1, Blocked)
	setDayStatus(g, 1, 2, Blocked)

	// 2. 仅放两个彼此分离的孤立可用节次。
	setSectionStatus(g, 1, 1, 10, Free)
	setSectionStatus(g, 1, 2, 10, Free)

	items := []model.TaskClassItem{{ID: 1}}
	_, err := computeAllocation(g, items, "rapid")
	if err == nil {
		t.Fatalf("期望返回时间不足错误，实际为 nil")
	}
	if err.Error() != respond.TimeNotEnoughForAutoScheduling.Error() {
		t.Fatalf("期望错误=%s，实际=%s", respond.TimeNotEnoughForAutoScheduling.Error(), err.Error())
	}
}

// TestComputeAllocation_RejectEvenStartPair 验证偶数起点双节（如 8-9）不允许作为粗排结果。
//
// 用例意图：
// 1. 构造一个看似连续的 8-9 空位；
// 2. 同时给出一个合法的 11-12 对齐空位；
// 3. 期望算法跳过 8-9，选择 11-12。
func TestComputeAllocation_RejectEvenStartPair(t *testing.T) {
	g := newTestGrid(1, 1, 1, 1)

	// 1. 全部先置为 Blocked，避免默认 Free 干扰判断。
	setDayStatus(g, 1, 1, Blocked)

	// 2. 构造“偶数起点双节 + 合法奇数起点双节”。
	setSectionStatus(g, 1, 1, 8, Free)
	setSectionStatus(g, 1, 1, 9, Free)
	setSectionStatus(g, 1, 1, 11, Free)
	setSectionStatus(g, 1, 1, 12, Free)

	items := []model.TaskClassItem{{ID: 1}}
	got, err := computeAllocation(g, items, "rapid")
	if err != nil {
		t.Fatalf("期望分配成功，实际报错: %v", err)
	}
	if got[0].EmbeddedTime == nil {
		t.Fatalf("期望回填 EmbeddedTime，实际为 nil")
	}

	tt := got[0].EmbeddedTime
	if tt.SectionFrom != 11 || tt.SectionTo != 12 {
		t.Fatalf("期望落位到 11-12，实际落位到 %d-%d", tt.SectionFrom, tt.SectionTo)
	}
}

// TestComputeAllocation_FillerNeedOddEvenPair 验证 Filler 课程块也必须满足奇数起点双节对齐。
//
// 用例意图：
// 1. 仅提供一个 Filler 课程块 8-9（偶数起点）；
// 2. 即使总可用节数为 2，也不能被当作合法落位；
// 3. 期望返回时间不足错误。
func TestComputeAllocation_FillerNeedOddEvenPair(t *testing.T) {
	g := newTestGrid(1, 1, 1, 1)

	// 1. 全部先置为 Blocked。
	setDayStatus(g, 1, 1, Blocked)

	// 2. 课程块 8-9 标记为 Filler，但其起点为偶数，不满足对齐规则。
	g.setNode(1, 1, 8, slotNode{Status: Filler, EventID: 1001})
	g.setNode(1, 1, 9, slotNode{Status: Filler, EventID: 1001})

	items := []model.TaskClassItem{{ID: 1}}
	_, err := computeAllocation(g, items, "rapid")
	if err == nil {
		t.Fatalf("期望返回时间不足错误，实际为 nil")
	}
	if err.Error() != respond.TimeNotEnoughForAutoScheduling.Error() {
		t.Fatalf("期望错误=%s，实际=%s", respond.TimeNotEnoughForAutoScheduling.Error(), err.Error())
	}
}
