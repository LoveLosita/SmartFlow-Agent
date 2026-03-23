package logic

import (
	"sort"
	"testing"
)

func TestPlanEvenSpreadMovesPrefersLowerLoadDay(t *testing.T) {
	tasks := []RefineTaskCandidate{
		{TaskItemID: 101, Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, OriginRank: 1},
		{TaskItemID: 102, Week: 16, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, OriginRank: 2},
	}
	slots := []RefineSlotCandidate{
		{Week: 12, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2},
		{Week: 12, DayOfWeek: 2, SectionFrom: 1, SectionTo: 2},
		{Week: 12, DayOfWeek: 3, SectionFrom: 1, SectionTo: 2},
	}
	moves, err := PlanEvenSpreadMoves(tasks, slots, RefineCompositePlanOptions{
		ExistingDayLoad: map[string]int{
			composeDayKey(12, 1): 5,
			composeDayKey(12, 2): 1,
			composeDayKey(12, 3): 0,
		},
	})
	if err != nil {
		t.Fatalf("PlanEvenSpreadMoves 返回错误: %v", err)
	}
	if len(moves) != 2 {
		t.Fatalf("期望移动 2 条，实际=%d", len(moves))
	}

	// 1. 低负载日（周三）应优先被填充；
	// 2. 第二条应落在次低负载日（周二），而不是高负载日（周一）。
	weekDayByID := make(map[int][2]int, len(moves))
	for _, move := range moves {
		weekDayByID[move.TaskItemID] = [2]int{move.ToWeek, move.ToDay}
	}
	if got := weekDayByID[101]; got != [2]int{12, 3} {
		t.Fatalf("任务101应优先落到 W12D3，实际=%v", got)
	}
	if got := weekDayByID[102]; got != [2]int{12, 2} {
		t.Fatalf("任务102应落到 W12D2，实际=%v", got)
	}
}

func TestPlanMinContextSwitchMovesGroupsSameContext(t *testing.T) {
	tasks := []RefineTaskCandidate{
		{TaskItemID: 201, Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, ContextTag: "数学", OriginRank: 1},
		{TaskItemID: 202, Week: 16, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, ContextTag: "算法", OriginRank: 2},
		{TaskItemID: 203, Week: 16, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6, ContextTag: "数学", OriginRank: 3},
	}
	slots := []RefineSlotCandidate{
		{Week: 12, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2},
		{Week: 12, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4},
		{Week: 12, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6},
	}
	moves, err := PlanMinContextSwitchMoves(tasks, slots, RefineCompositePlanOptions{})
	if err != nil {
		t.Fatalf("PlanMinContextSwitchMoves 返回错误: %v", err)
	}
	if len(moves) != 3 {
		t.Fatalf("期望移动 3 条，实际=%d", len(moves))
	}

	// 1. “数学”有 2 条，分组后应先连续落在最早两个坑位；
	// 2. 因此 201 与 203 对应的目标节次应是 1-2 与 3-4（顺序由 origin_rank 决定）。
	sort.SliceStable(moves, func(i, j int) bool {
		if moves[i].ToWeek != moves[j].ToWeek {
			return moves[i].ToWeek < moves[j].ToWeek
		}
		if moves[i].ToDay != moves[j].ToDay {
			return moves[i].ToDay < moves[j].ToDay
		}
		return moves[i].ToSectionFrom < moves[j].ToSectionFrom
	})
	if moves[0].TaskItemID != 201 || moves[1].TaskItemID != 203 {
		t.Fatalf("期望前两个坑位由同上下文任务占据，实际=%+v", moves)
	}
	if moves[2].TaskItemID != 202 {
		t.Fatalf("期望最后一个坑位为算法任务，实际=%+v", moves[2])
	}
}

func TestPlanEvenSpreadMovesReturnsErrorWhenSpanNotMatched(t *testing.T) {
	tasks := []RefineTaskCandidate{
		{TaskItemID: 301, Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 3, OriginRank: 1}, // span=3
	}
	slots := []RefineSlotCandidate{
		{Week: 12, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2}, // span=2
	}
	_, err := PlanEvenSpreadMoves(tasks, slots, RefineCompositePlanOptions{})
	if err == nil {
		t.Fatalf("期望 span 不匹配时报错，实际 err=nil")
	}
}
