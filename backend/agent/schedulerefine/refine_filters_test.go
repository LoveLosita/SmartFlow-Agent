package schedulerefine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LoveLosita/smartflow/backend/model"
)

func TestQueryTargetTasksWeekFilterAndTaskID(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 1, Name: "task-w12", Week: 12, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, Status: "suggested", Type: "task"},
		{TaskItemID: 2, Name: "task-w13", Week: 13, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, Status: "suggested", Type: "task"},
		{TaskItemID: 3, Name: "task-w14", Week: 14, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6, Status: "suggested", Type: "task"},
	}
	policy := refineToolPolicy{OriginOrderMap: map[int]int{1: 1, 2: 2, 3: 3}}

	paramsWeek := map[string]any{
		"week_filter": []any{13.0, 14.0},
	}
	_, resultWeek := refineToolQueryTargetTasks(entries, paramsWeek, policy)
	if !resultWeek.Success {
		t.Fatalf("week_filter 查询失败: %s", resultWeek.Result)
	}
	var payloadWeek struct {
		Count int `json:"count"`
		Items []struct {
			TaskItemID int `json:"task_item_id"`
			Week       int `json:"week"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(resultWeek.Result), &payloadWeek); err != nil {
		t.Fatalf("解析 week_filter 结果失败: %v", err)
	}
	if payloadWeek.Count != 2 {
		t.Fatalf("week_filter 期望返回 2 条，实际=%d", payloadWeek.Count)
	}
	for _, item := range payloadWeek.Items {
		if item.Week != 13 && item.Week != 14 {
			t.Fatalf("week_filter 过滤失败，出现非法周次=%d", item.Week)
		}
	}

	paramsTaskID := map[string]any{
		"week_filter":  []any{13.0, 14.0},
		"task_item_id": 2,
	}
	_, resultTaskID := refineToolQueryTargetTasks(entries, paramsTaskID, policy)
	if !resultTaskID.Success {
		t.Fatalf("task_item_id 查询失败: %s", resultTaskID.Result)
	}
	var payloadTaskID struct {
		Count int `json:"count"`
		Items []struct {
			TaskItemID int `json:"task_item_id"`
			Week       int `json:"week"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(resultTaskID.Result), &payloadTaskID); err != nil {
		t.Fatalf("解析 task_item_id 结果失败: %v", err)
	}
	if payloadTaskID.Count != 1 {
		t.Fatalf("task_item_id 期望返回 1 条，实际=%d", payloadTaskID.Count)
	}
	if payloadTaskID.Items[0].TaskItemID != 2 || payloadTaskID.Items[0].Week != 13 {
		t.Fatalf("task_item_id 过滤错误: %+v", payloadTaskID.Items[0])
	}
}

func TestQueryAvailableSlotsExactSectionAlias(t *testing.T) {
	params := map[string]any{
		"week":             13,
		"section_duration": 2,
		"section_from":     1,
		"section_to":       2,
		"limit":            5,
	}
	_, result := refineToolQueryAvailableSlots(nil, params, planningWindow{Enabled: false})
	if !result.Success {
		t.Fatalf("QueryAvailableSlots 失败: %s", result.Result)
	}
	var payload struct {
		Count int `json:"count"`
		Slots []struct {
			Week        int `json:"week"`
			SectionFrom int `json:"section_from"`
			SectionTo   int `json:"section_to"`
		} `json:"slots"`
	}
	if err := json.Unmarshal([]byte(result.Result), &payload); err != nil {
		t.Fatalf("解析 QueryAvailableSlots 结果失败: %v", err)
	}
	if payload.Count == 0 {
		t.Fatalf("期望至少返回一个可用时段，实际=0")
	}
	for _, slot := range payload.Slots {
		if slot.Week != 13 {
			t.Fatalf("返回了错误周次: %+v", slot)
		}
		if slot.SectionFrom != 1 || slot.SectionTo != 2 {
			t.Fatalf("精确节次过滤失败: %+v", slot)
		}
	}
}

func TestQueryAvailableSlotsWeekFilterDayFilterAlias(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 1, Name: "task-w12", Week: 12, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, Status: "suggested", Type: "task"},
		{TaskItemID: 2, Name: "task-w17", Week: 17, DayOfWeek: 4, SectionFrom: 3, SectionTo: 4, Status: "suggested", Type: "task"},
	}
	params := map[string]any{
		"week_filter": []any{17.0},
		"day_filter":  []any{1.0, 2.0, 3.0},
		"limit":       20,
	}

	_, result := refineToolQueryAvailableSlots(entries, params, planningWindow{Enabled: false})
	if !result.Success {
		t.Fatalf("QueryAvailableSlots 别名查询失败: %s", result.Result)
	}
	var payload struct {
		Count int `json:"count"`
		Slots []struct {
			Week      int `json:"week"`
			DayOfWeek int `json:"day_of_week"`
		} `json:"slots"`
	}
	if err := json.Unmarshal([]byte(result.Result), &payload); err != nil {
		t.Fatalf("解析 week/day 过滤结果失败: %v", err)
	}
	if payload.Count == 0 {
		t.Fatalf("week_filter/day_filter 查询应返回 W17 周一到周三空位，实际为空")
	}
	for _, slot := range payload.Slots {
		if slot.Week != 17 {
			t.Fatalf("week_filter 失效，出现 week=%d", slot.Week)
		}
		if slot.DayOfWeek < 1 || slot.DayOfWeek > 3 {
			t.Fatalf("day_filter 失效，出现 day_of_week=%d", slot.DayOfWeek)
		}
	}
}

func TestCollectWorksetTaskIDsSourceWeekOnly(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 1, Week: 12, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2, Status: "suggested", Type: "task"},
		{TaskItemID: 2, Week: 14, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4, Status: "suggested", Type: "task"},
		{TaskItemID: 3, Week: 13, DayOfWeek: 1, SectionFrom: 5, SectionTo: 6, Status: "suggested", Type: "task"},
		{TaskItemID: 4, Week: 14, DayOfWeek: 2, SectionFrom: 7, SectionTo: 8, Status: "suggested", Type: "task"},
	}
	slice := RefineSlicePlan{WeekFilter: []int{14, 13}}
	originOrder := map[int]int{1: 1, 2: 2, 3: 3, 4: 4}

	got := collectWorksetTaskIDs(entries, slice, originOrder)
	if len(got) != 2 {
		t.Fatalf("来源周收敛失败，期望 2 条，实际=%d, got=%v", len(got), got)
	}
	if got[0] != 2 || got[1] != 4 {
		t.Fatalf("来源周结果错误，期望 [2 4]，实际=%v", got)
	}
}

func TestBuildSlicePlanDirectionalSourceTarget(t *testing.T) {
	st := &ScheduleRefineState{
		UserMessage: "帮我把第17周周四到周五的任务都收敛到17周的周一到周三，优先放空位，空位不够了再嵌入",
	}
	plan := buildSlicePlan(st)
	if len(plan.WeekFilter) == 0 || plan.WeekFilter[0] != 17 {
		t.Fatalf("week_filter 解析错误: %+v", plan.WeekFilter)
	}
	expectSource := []int{4, 5}
	expectTarget := []int{1, 2, 3}
	if len(plan.SourceDays) != len(expectSource) {
		t.Fatalf("source_days 长度错误: got=%v", plan.SourceDays)
	}
	for i := range expectSource {
		if plan.SourceDays[i] != expectSource[i] {
			t.Fatalf("source_days 错误: got=%v", plan.SourceDays)
		}
	}
	if len(plan.TargetDays) != len(expectTarget) {
		t.Fatalf("target_days 长度错误: got=%v", plan.TargetDays)
	}
	for i := range expectTarget {
		if plan.TargetDays[i] != expectTarget[i] {
			t.Fatalf("target_days 错误: got=%v", plan.TargetDays)
		}
	}
}

func TestVerifyTaskCoordinateMismatch(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{TaskItemID: 28, Name: "task-w17-d4", Week: 17, DayOfWeek: 4, SectionFrom: 5, SectionTo: 6, Status: "suggested", Type: "task"},
	}
	policy := refineToolPolicy{OriginOrderMap: map[int]int{28: 1}}
	params := map[string]any{
		"task_item_id": 28,
		"week":         17,
		"day_of_week":  1,
		"section_from": 1,
		"section_to":   2,
	}

	_, result := refineToolVerify(entries, params, policy)
	if result.Success {
		t.Fatalf("期望 Verify 在任务坐标不匹配时失败，实际 success=true, result=%s", result.Result)
	}
	if result.ErrorCode != "VERIFY_FAILED" {
		t.Fatalf("期望错误码 VERIFY_FAILED，实际=%s", result.ErrorCode)
	}
	if !strings.Contains(result.Result, "不匹配") {
		t.Fatalf("期望结果包含“不匹配”提示，实际=%s", result.Result)
	}
}

func TestMoveRejectsSuggestedCourseEntry(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{
			TaskItemID:  39,
			Name:        "面向对象程序设计-C++",
			Type:        "course",
			Status:      "suggested",
			Week:        17,
			DayOfWeek:   4,
			SectionFrom: 7,
			SectionTo:   8,
		},
	}
	params := map[string]any{
		"task_item_id":    39,
		"to_week":         17,
		"to_day":          1,
		"to_section_from": 7,
		"to_section_to":   8,
	}
	_, result := refineToolMove(entries, params, planningWindow{Enabled: false}, refineToolPolicy{OriginOrderMap: map[int]int{39: 1}})
	if result.Success {
		t.Fatalf("期望 course 类型的 suggested 条目不可移动，实际 success=true, result=%s", result.Result)
	}
	if !strings.Contains(result.Result, "可移动 suggested 任务") {
		t.Fatalf("期望返回不可移动提示，实际=%s", result.Result)
	}
}

func TestQueryAvailableSlotsSlotTypePureDisablesEmbed(t *testing.T) {
	entries := []model.HybridScheduleEntry{
		{
			Name:              "可嵌入课程",
			Type:              "course",
			Status:            "existing",
			Week:              17,
			DayOfWeek:         1,
			SectionFrom:       1,
			SectionTo:         2,
			BlockForSuggested: false,
		},
	}

	pureParams := map[string]any{
		"week":         17,
		"day_of_week":  1,
		"section_from": 1,
		"section_to":   2,
		"slot_type":    "pure",
	}
	_, pureResult := refineToolQueryAvailableSlots(entries, pureParams, planningWindow{Enabled: false})
	if !pureResult.Success {
		t.Fatalf("pure 查询失败: %s", pureResult.Result)
	}
	var purePayload struct {
		Count         int  `json:"count"`
		EmbeddedCount int  `json:"embedded_count"`
		FallbackUsed  bool `json:"fallback_used"`
	}
	if err := json.Unmarshal([]byte(pureResult.Result), &purePayload); err != nil {
		t.Fatalf("解析 pure 查询结果失败: %v", err)
	}
	if purePayload.Count != 0 || purePayload.EmbeddedCount != 0 || purePayload.FallbackUsed {
		t.Fatalf("slot_type=pure 应禁用嵌入兜底，实际 payload=%+v", purePayload)
	}

	defaultParams := map[string]any{
		"week":         17,
		"day_of_week":  1,
		"section_from": 1,
		"section_to":   2,
	}
	_, defaultResult := refineToolQueryAvailableSlots(entries, defaultParams, planningWindow{Enabled: false})
	if !defaultResult.Success {
		t.Fatalf("default 查询失败: %s", defaultResult.Result)
	}
	var defaultPayload struct {
		Count         int  `json:"count"`
		EmbeddedCount int  `json:"embedded_count"`
		FallbackUsed  bool `json:"fallback_used"`
	}
	if err := json.Unmarshal([]byte(defaultResult.Result), &defaultPayload); err != nil {
		t.Fatalf("解析 default 查询结果失败: %v", err)
	}
	if defaultPayload.Count == 0 || defaultPayload.EmbeddedCount == 0 || !defaultPayload.FallbackUsed {
		t.Fatalf("默认查询应允许嵌入候选，实际 payload=%+v", defaultPayload)
	}
}

func TestCompileObjectiveAndEvaluateMoveAllPass(t *testing.T) {
	initial := []model.HybridScheduleEntry{
		{TaskItemID: 39, Name: "任务39", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 4, SectionFrom: 7, SectionTo: 8},
		{TaskItemID: 51, Name: "任务51", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 5, SectionFrom: 9, SectionTo: 10},
	}
	final := []model.HybridScheduleEntry{
		{TaskItemID: 39, Name: "任务39", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 1, SectionFrom: 7, SectionTo: 8},
		{TaskItemID: 51, Name: "任务51", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 2, SectionFrom: 9, SectionTo: 10},
	}
	st := &ScheduleRefineState{
		UserMessage:          "把17周周四到周五任务收敛到周一到周三",
		InitialHybridEntries: initial,
		HybridEntries:        final,
		SlicePlan: RefineSlicePlan{
			WeekFilter: []int{17},
			SourceDays: []int{4, 5},
			TargetDays: []int{1, 2, 3},
		},
	}
	st.Objective = compileRefineObjective(st, st.SlicePlan)
	if st.Objective.Mode != "move_all" {
		t.Fatalf("期望目标模式 move_all，实际=%s", st.Objective.Mode)
	}

	pass, _, unmet, applied := evaluateObjectiveDeterministic(st)
	if !applied {
		t.Fatalf("期望命中确定性终审")
	}
	if !pass {
		t.Fatalf("期望确定性终审通过，unmet=%v", unmet)
	}
}

func TestCompileObjectiveAndEvaluateMoveAllFail(t *testing.T) {
	initial := []model.HybridScheduleEntry{
		{TaskItemID: 26, Name: "任务26", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 5, SectionFrom: 7, SectionTo: 8},
	}
	final := []model.HybridScheduleEntry{
		{TaskItemID: 26, Name: "任务26", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 5, SectionFrom: 7, SectionTo: 8},
	}
	st := &ScheduleRefineState{
		UserMessage:          "把17周周四到周五任务收敛到周一到周三",
		InitialHybridEntries: initial,
		HybridEntries:        final,
		SlicePlan: RefineSlicePlan{
			WeekFilter: []int{17},
			SourceDays: []int{4, 5},
			TargetDays: []int{1, 2, 3},
		},
	}
	st.Objective = compileRefineObjective(st, st.SlicePlan)

	pass, _, unmet, applied := evaluateObjectiveDeterministic(st)
	if !applied {
		t.Fatalf("期望命中确定性终审")
	}
	if pass {
		t.Fatalf("期望确定性终审失败")
	}
	if len(unmet) == 0 {
		t.Fatalf("期望返回未满足项")
	}
}

func TestCompileObjectiveMoveRatioFromContractAndEvaluatePass(t *testing.T) {
	initial, final := buildHalfTransferEntries(10, 5)
	st := &ScheduleRefineState{
		UserMessage:          "17周任务太多，帮我调整到16周",
		InitialHybridEntries: initial,
		HybridEntries:        final,
		SlicePlan: RefineSlicePlan{
			WeekFilter: []int{17, 16},
		},
		Contract: RefineContract{
			Intent:           "将第17周任务匀一半到第16周",
			HardRequirements: []string{"原第17周任务数调整为原来的一半", "调整到第16周的任务数为原第17周任务数的一半"},
		},
	}
	st.Objective = compileRefineObjective(st, st.SlicePlan)
	if st.Objective.Mode != "move_ratio" {
		t.Fatalf("期望目标模式 move_ratio，实际=%s", st.Objective.Mode)
	}
	if st.Objective.RequiredMoveMin != 5 || st.Objective.RequiredMoveMax != 5 {
		t.Fatalf("半数迁移阈值错误: min=%d max=%d", st.Objective.RequiredMoveMin, st.Objective.RequiredMoveMax)
	}

	pass, _, unmet, applied := evaluateObjectiveDeterministic(st)
	if !applied {
		t.Fatalf("期望命中确定性终审")
	}
	if !pass {
		t.Fatalf("期望半数迁移通过，unmet=%v", unmet)
	}
}

func TestCompileObjectiveMoveRatioFromContractAndEvaluateFail(t *testing.T) {
	initial, final := buildHalfTransferEntries(10, 4)
	st := &ScheduleRefineState{
		UserMessage:          "17周任务太多，帮我调整到16周",
		InitialHybridEntries: initial,
		HybridEntries:        final,
		SlicePlan: RefineSlicePlan{
			WeekFilter: []int{17, 16},
		},
		Contract: RefineContract{
			Intent:           "将第17周任务匀一半到第16周",
			HardRequirements: []string{"原第17周任务数调整为原来的一半", "调整到第16周的任务数为原第17周任务数的一半"},
		},
	}
	st.Objective = compileRefineObjective(st, st.SlicePlan)

	pass, _, unmet, applied := evaluateObjectiveDeterministic(st)
	if !applied {
		t.Fatalf("期望命中确定性终审")
	}
	if pass {
		t.Fatalf("期望半数迁移失败")
	}
	if len(unmet) == 0 {
		t.Fatalf("期望返回未满足项")
	}
}

func TestCompileObjectiveMoveRatioFromStructuredAssertion(t *testing.T) {
	initial, final := buildHalfTransferEntries(10, 5)
	st := &ScheduleRefineState{
		UserMessage:          "请把任务重新分配",
		InitialHybridEntries: initial,
		HybridEntries:        final,
		SlicePlan: RefineSlicePlan{
			WeekFilter: []int{17, 16},
		},
		Contract: RefineContract{
			Intent: "任务重新分配",
			HardAssertions: []RefineAssertion{
				{
					Metric:     "source_move_ratio_percent",
					Operator:   "==",
					Value:      50,
					Week:       17,
					TargetWeek: 16,
				},
			},
		},
	}
	st.Objective = compileRefineObjective(st, st.SlicePlan)
	if st.Objective.Mode != "move_ratio" {
		t.Fatalf("结构化断言未生效，期望 move_ratio，实际=%s", st.Objective.Mode)
	}
}

func buildHalfTransferEntries(total int, moved int) ([]model.HybridScheduleEntry, []model.HybridScheduleEntry) {
	initial := make([]model.HybridScheduleEntry, 0, total)
	final := make([]model.HybridScheduleEntry, 0, total)
	for i := 1; i <= total; i++ {
		initial = append(initial, model.HybridScheduleEntry{
			TaskItemID:  i,
			Name:        "task",
			Type:        "task",
			Status:      "suggested",
			Week:        17,
			DayOfWeek:   1,
			SectionFrom: 1,
			SectionTo:   2,
		})
		week := 17
		if i <= moved {
			week = 16
		}
		final = append(final, model.HybridScheduleEntry{
			TaskItemID:  i,
			Name:        "task",
			Type:        "task",
			Status:      "suggested",
			Week:        week,
			DayOfWeek:   1,
			SectionFrom: 1,
			SectionTo:   2,
		})
	}
	return initial, final
}

func TestNormalizeMovableTaskOrderByOrigin(t *testing.T) {
	st := &ScheduleRefineState{
		OriginOrderMap: map[int]int{
			101: 1,
			202: 2,
		},
		HybridEntries: []model.HybridScheduleEntry{
			{TaskItemID: 202, Name: "task-202", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2},
			{TaskItemID: 101, Name: "task-101", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 3, SectionFrom: 1, SectionTo: 2},
		},
	}
	changed := normalizeMovableTaskOrderByOrigin(st)
	if !changed {
		t.Fatalf("期望发生顺序归位")
	}
	sortHybridEntries(st.HybridEntries)
	if st.HybridEntries[0].TaskItemID != 101 || st.HybridEntries[1].TaskItemID != 202 {
		t.Fatalf("顺序归位失败: %+v", st.HybridEntries)
	}
}

func TestTryNormalizeMovableTaskOrderByOriginSkipsAfterMinContextSwitch(t *testing.T) {
	st := &ScheduleRefineState{
		OriginOrderMap: map[int]int{
			101: 1,
			202: 2,
		},
		CompositeToolSuccess: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": true,
		},
		HybridEntries: []model.HybridScheduleEntry{
			{TaskItemID: 202, Name: "task-202", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2},
			{TaskItemID: 101, Name: "task-101", Type: "task", Status: "suggested", Week: 17, DayOfWeek: 3, SectionFrom: 1, SectionTo: 2},
		},
	}
	changed, skipped := tryNormalizeMovableTaskOrderByOrigin(st)
	if !skipped {
		t.Fatalf("期望 MinContextSwitch 成功后跳过顺序归位")
	}
	if changed {
		t.Fatalf("跳过顺序归位时不应报告 changed=true")
	}
	if st.HybridEntries[0].TaskItemID != 202 || st.HybridEntries[1].TaskItemID != 101 {
		t.Fatalf("跳过顺序归位后不应改写任务顺序: %+v", st.HybridEntries)
	}
}

func TestEvaluateHardChecksSkipsOrderConstraintAfterMinContextSwitch(t *testing.T) {
	st := &ScheduleRefineState{
		UserMessage: "减少第15周科目切换",
		OriginOrderMap: map[int]int{
			101: 1,
			202: 2,
		},
		CompositeToolSuccess: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": true,
		},
		InitialHybridEntries: []model.HybridScheduleEntry{
			{TaskItemID: 101, Name: "概率任务", Type: "task", Status: "suggested", Week: 15, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2},
			{TaskItemID: 202, Name: "数电任务", Type: "task", Status: "suggested", Week: 15, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4},
		},
		HybridEntries: []model.HybridScheduleEntry{
			{TaskItemID: 202, Name: "数电任务", Type: "task", Status: "suggested", Week: 15, DayOfWeek: 1, SectionFrom: 1, SectionTo: 2},
			{TaskItemID: 101, Name: "概率任务", Type: "task", Status: "suggested", Week: 15, DayOfWeek: 1, SectionFrom: 3, SectionTo: 4},
		},
		Objective: RefineObjective{
			Mode:                    "move_all",
			SourceWeeks:             []int{15},
			TargetWeeks:             []int{15},
			BaselineSourceTaskCount: 2,
			RequiredMoveMin:         2,
			RequiredMoveMax:         2,
		},
		SlicePlan: RefineSlicePlan{
			WeekFilter: []int{15},
		},
	}
	report := evaluateHardChecks(nil, nil, st, nil)
	if !report.OrderPassed {
		t.Fatalf("期望 MinContextSwitch 成功后跳过顺序终审，实际 issues=%v", report.OrderIssues)
	}
}

func TestPrecheckToolCallPolicyRejectsRedundantSlotQuery(t *testing.T) {
	st := &ScheduleRefineState{
		SeenSlotQueries: make(map[string]struct{}),
		EntriesVersion:  0,
	}
	call := reactToolCall{
		Tool: "QueryAvailableSlots",
		Params: map[string]any{
			"week":        16,
			"day_of_week": 1,
		},
	}

	if blockedResult, blocked := precheckToolCallPolicy(st, call, nil); blocked {
		t.Fatalf("首次查询不应被拒绝: %+v", blockedResult)
	}
	if blockedResult, blocked := precheckToolCallPolicy(st, call, nil); !blocked {
		t.Fatalf("重复查询应被拒绝")
	} else if blockedResult.ErrorCode != "QUERY_REDUNDANT" {
		t.Fatalf("错误码不符合预期: %+v", blockedResult)
	}
	st.EntriesVersion++
	if blockedResult, blocked := precheckToolCallPolicy(st, call, nil); blocked {
		t.Fatalf("排程版本变化后应允许再次查询: %+v", blockedResult)
	}
}

func TestCanonicalizeMoveParamsFromRepairAliases(t *testing.T) {
	call := reactToolCall{
		Tool: "Move",
		Params: map[string]any{
			"task_item_id": 16,
			"new_week":     16,
			"day_of_week":  1,
			"section_from": 1,
			"section_to":   2,
		},
	}
	normalized := canonicalizeToolCall(call)
	if _, ok := paramIntAny(normalized.Params, "to_week"); !ok {
		t.Fatalf("to_week 规范化失败: %+v", normalized.Params)
	}
	if _, ok := paramIntAny(normalized.Params, "to_day"); !ok {
		t.Fatalf("to_day 规范化失败: %+v", normalized.Params)
	}
	if _, ok := paramIntAny(normalized.Params, "to_section_from"); !ok {
		t.Fatalf("to_section_from 规范化失败: %+v", normalized.Params)
	}
	if _, ok := paramIntAny(normalized.Params, "to_section_to"); !ok {
		t.Fatalf("to_section_to 规范化失败: %+v", normalized.Params)
	}
}

func TestDetectOrderIntentDefaultsToKeep(t *testing.T) {
	if !detectOrderIntent("16周总体任务太多了，帮我移动一半到12周") {
		t.Fatalf("未显式放宽顺序时，默认应保持顺序")
	}
}

func TestDetectOrderIntentExplicitAllowReorder(t *testing.T) {
	if detectOrderIntent("这次顺序无所谓，可以打乱顺序") {
		t.Fatalf("用户明确允许乱序时，应关闭顺序约束")
	}
}
