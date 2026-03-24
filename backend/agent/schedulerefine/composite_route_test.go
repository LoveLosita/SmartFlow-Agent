package schedulerefine

import (
	"context"
	"testing"

	"github.com/LoveLosita/smartflow/backend/model"
)

func TestRefineToolSpreadEvenRespectsCanonicalRouteFilters(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 1, Name: "任务1", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, ContextTag: "A"},
		// 1. 这里放一个更早周次的 existing 条目，用来把可查询窗口拉到 W11；
		// 2. 若复合工具内部丢了 week_filter/day_of_week，就会优先落到更早的 W11D1，而不是目标 W12D3。
		{TaskItemID: 99, Name: "课程", Type: "course", Status: "existing", Week: 11, DayOfWeek: 5, SectionFrom: 11, SectionTo: 12, BlockForSuggested: true},
	}
	params := map[string]any{
		"task_item_ids": []int{1},
		"week_filter":   []int{12},
		"day_of_week":   []int{3},
		"allow_embed":   false,
	}

	nextEntries, result := refineToolSpreadEven(entries, params, planningWindow{Enabled: false}, refineToolPolicy{
		OriginOrderMap: map[int]int{1: 1},
	})
	if !result.Success {
		t.Fatalf("SpreadEven 执行失败: %s", result.Result)
	}

	idx := findSuggestedByID(nextEntries, 1)
	if idx < 0 {
		t.Fatalf("未找到 task_item_id=1")
	}
	got := nextEntries[idx]
	if got.Week != 12 || got.DayOfWeek != 3 {
		t.Fatalf("期望复合工具严格遵守 week_filter/day_of_week，实际落点=W%dD%d", got.Week, got.DayOfWeek)
	}
}

func TestRunCompositeRouteNodeAllowsHandoffWithoutDeterministicObjective(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 11, Name: "任务11", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, ContextTag: "数学"},
		{TaskItemID: 12, Name: "任务12", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, ContextTag: "算法"},
		{TaskItemID: 13, Name: "任务13", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6, ContextTag: "数学"},
	}
	st := &ScheduleRefineState{
		UserMessage:           "把这些任务按最少上下文切换整理一下",
		HybridEntries:         cloneHybridEntries(entries),
		InitialHybridEntries:  cloneHybridEntries(entries),
		WorksetTaskIDs:        []int{11, 12, 13},
		RequiredCompositeTool: "MinContextSwitch",
		CompositeRetryMax:     0,
		ExecuteMax:            4,
		OriginOrderMap:        map[int]int{11: 1, 12: 2, 13: 3},
		CompositeToolCalled: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		},
		CompositeToolSuccess: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		},
	}

	stageLogs := make([]string, 0, 8)
	nextState, err := runCompositeRouteNode(context.Background(), st, func(stage, detail string) {
		stageLogs = append(stageLogs, stage+"|"+detail)
	})
	if err != nil {
		t.Fatalf("runCompositeRouteNode 返回错误: %v", err)
	}
	if nextState == nil {
		t.Fatalf("runCompositeRouteNode 返回 nil state")
	}
	if !nextState.CompositeRouteSucceeded {
		t.Fatalf("期望复合分支在缺少 deterministic objective 时直接出站，实际 CompositeRouteSucceeded=false, stages=%v, action_logs=%v", stageLogs, nextState.ActionLogs)
	}
	if nextState.DisableCompositeTools {
		t.Fatalf("期望复合分支直接进入终审，不应降级为禁复合 ReAct")
	}
	if !nextState.CompositeToolSuccess["MinContextSwitch"] {
		t.Fatalf("期望 MinContextSwitch 成功状态被记录")
	}
}
