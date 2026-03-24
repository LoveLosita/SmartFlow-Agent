package schedulerefine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/LoveLosita/smartflow/backend/model"
)

func TestRefineToolSpreadEvenSuccess(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 1, Name: "任务1", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, ContextTag: "A"},
		{TaskItemID: 2, Name: "任务2", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, ContextTag: "B"},
		{TaskItemID: 99, Name: "课程", Type: "course", Status: "existing", Week: 12, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6, BlockForSuggested: true},
	}
	params := map[string]any{
		"task_item_ids": []any{1.0, 2.0},
		"week":          12,
		"day_of_week":   []any{1.0, 2.0, 3.0},
		"allow_embed":   false,
	}
	policy := refineToolPolicy{OriginOrderMap: map[int]int{1: 1, 2: 2}}

	nextEntries, result := refineToolSpreadEven(entries, params, planningWindow{Enabled: false}, policy)
	if !result.Success {
		t.Fatalf("SpreadEven 执行失败: %s", result.Result)
	}
	if result.Tool != "SpreadEven" {
		t.Fatalf("工具名错误，期望 SpreadEven，实际=%s", result.Tool)
	}

	idx1 := findSuggestedByID(nextEntries, 1)
	idx2 := findSuggestedByID(nextEntries, 2)
	if idx1 < 0 || idx2 < 0 {
		t.Fatalf("移动后未找到目标任务: idx1=%d idx2=%d", idx1, idx2)
	}
	task1 := nextEntries[idx1]
	task2 := nextEntries[idx2]
	if task1.Week != 12 || task2.Week != 12 {
		t.Fatalf("期望任务被移动到 W12，实际 task1=%d task2=%d", task1.Week, task2.Week)
	}
	if task1.DayOfWeek < 1 || task1.DayOfWeek > 3 || task2.DayOfWeek < 1 || task2.DayOfWeek > 3 {
		t.Fatalf("期望任务被移动到周一到周三，实际 task1=%d task2=%d", task1.DayOfWeek, task2.DayOfWeek)
	}
	if task1.DayOfWeek == task2.DayOfWeek && sectionsOverlap(task1.SectionFrom, task1.SectionTo, task2.SectionFrom, task2.SectionTo) {
		t.Fatalf("复合工具不应产出重叠坑位: task1=%+v task2=%+v", task1, task2)
	}
}

func TestRefineToolMinContextSwitchGroupsContext(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 11, Name: "任务11", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, ContextTag: "数学"},
		{TaskItemID: 12, Name: "任务12", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, ContextTag: "算法"},
		{TaskItemID: 13, Name: "任务13", Type: "task", Status: "suggested", Week: 16, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6, ContextTag: "数学"},
		{TaskItemID: 99, Name: "课程", Type: "course", Status: "existing", Week: 12, DayOfWeek: 1, SectionFrom: 11, SectionTo: 12, BlockForSuggested: true},
	}
	params := map[string]any{
		"task_item_ids": []any{11.0, 12.0, 13.0},
		"week":          12,
		"day_of_week":   []any{1.0},
	}
	policy := refineToolPolicy{OriginOrderMap: map[int]int{11: 1, 12: 2, 13: 3}}

	nextEntries, result := refineToolMinContextSwitch(entries, params, planningWindow{Enabled: false}, policy)
	if !result.Success {
		t.Fatalf("MinContextSwitch 执行失败: %s", result.Result)
	}
	if result.Tool != "MinContextSwitch" {
		t.Fatalf("工具名错误，期望 MinContextSwitch，实际=%s", result.Tool)
	}

	selected := make([]model.HybridScheduleEntry, 0, 3)
	for _, id := range []int{11, 12, 13} {
		idx := findSuggestedByID(nextEntries, id)
		if idx < 0 {
			t.Fatalf("未找到任务 id=%d", id)
		}
		selected = append(selected, nextEntries[idx])
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Week != selected[j].Week {
			return selected[i].Week < selected[j].Week
		}
		if selected[i].DayOfWeek != selected[j].DayOfWeek {
			return selected[i].DayOfWeek < selected[j].DayOfWeek
		}
		return selected[i].SectionFrom < selected[j].SectionFrom
	})

	switches := 0
	for i := 1; i < len(selected); i++ {
		if selected[i].ContextTag != selected[i-1].ContextTag {
			switches++
		}
	}
	if switches > 1 {
		t.Fatalf("期望最少上下文切换（<=1），实际 switches=%d, tasks=%+v", switches, selected)
	}
	if selected[0].TaskItemID != 11 || selected[1].TaskItemID != 13 || selected[2].TaskItemID != 12 {
		t.Fatalf("期望在原坑位集合内重排为 11,13,12，实际=%+v", selected)
	}
	for _, task := range selected {
		if task.Week != 16 || task.DayOfWeek != 1 {
			t.Fatalf("MinContextSwitch 不应跳出原坑位集合，实际 task=%+v", task)
		}
	}
}

func TestRefineToolMinContextSwitchKeepsCurrentSlotSet(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 21, Name: "随机事件与概率基础概念复习", Type: "task", Status: "suggested", Week: 14, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, ContextTag: "General"},
		{TaskItemID: 22, Name: "数制、码制与逻辑代数基础", Type: "task", Status: "suggested", Week: 14, DayOfWeek: 1, SectionFrom: 11, SectionTo: 12, ContextTag: "General"},
		{TaskItemID: 23, Name: "第二章 条件概率与全概率公式", Type: "task", Status: "suggested", Week: 14, DayOfWeek: 3, SectionFrom: 3, SectionTo: 4, ContextTag: "General"},
	}
	params := map[string]any{
		"task_item_ids": []any{21.0, 22.0, 23.0},
		"week":          14,
		"limit":         48,
		"allow_embed":   true,
	}
	policy := refineToolPolicy{OriginOrderMap: map[int]int{21: 1, 22: 2, 23: 3}}

	nextEntries, result := refineToolMinContextSwitch(entries, params, planningWindow{Enabled: false}, policy)
	if !result.Success {
		t.Fatalf("MinContextSwitch 执行失败: %s", result.Result)
	}

	selected := make([]model.HybridScheduleEntry, 0, 3)
	for _, id := range []int{21, 22, 23} {
		idx := findSuggestedByID(nextEntries, id)
		if idx < 0 {
			t.Fatalf("未找到任务 id=%d", id)
		}
		selected = append(selected, nextEntries[idx])
	}
	sort.SliceStable(selected, func(i, j int) bool {
		if selected[i].Week != selected[j].Week {
			return selected[i].Week < selected[j].Week
		}
		if selected[i].DayOfWeek != selected[j].DayOfWeek {
			return selected[i].DayOfWeek < selected[j].DayOfWeek
		}
		return selected[i].SectionFrom < selected[j].SectionFrom
	})

	if selected[0].TaskItemID != 21 || selected[1].TaskItemID != 23 || selected[2].TaskItemID != 22 {
		t.Fatalf("期望按原坑位集合重排为概率, 概率, 数电，实际=%+v", selected)
	}
	expectedSlots := map[int]string{
		21: "14-1-1-2",
		23: "14-1-11-12",
		22: "14-3-3-4",
	}
	for _, task := range selected {
		got := fmt.Sprintf("%d-%d-%d-%d", task.Week, task.DayOfWeek, task.SectionFrom, task.SectionTo)
		if got != expectedSlots[task.TaskItemID] {
			t.Fatalf("任务 id=%d 应仅在原坑位集合内换位，期望=%s 实际=%s", task.TaskItemID, expectedSlots[task.TaskItemID], got)
		}
	}
}

func TestListTaskIDsFromToolCallComposite(t *testing.T) {
	call := reactToolCall{
		Tool: "SpreadEven",
		Params: map[string]any{
			"task_item_ids": []any{1.0, 2.0, 2.0},
			"task_item_id":  3,
		},
	}
	ids := listTaskIDsFromToolCall(call)
	if len(ids) != 3 {
		t.Fatalf("期望提取 3 个去重 ID，实际=%v", ids)
	}
	sort.Ints(ids)
	if ids[0] != 1 || ids[1] != 2 || ids[2] != 3 {
		t.Fatalf("提取结果错误，实际=%v", ids)
	}
}
