package conv

import (
	"testing"

	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

// buildTestState 构造最小可用的 ScheduleState，DayMapping 让 expandToCoords 能正常工作。
func buildTestState(days []newagenttools.DayMapping, tasks []newagenttools.ScheduleTask) *newagenttools.ScheduleState {
	return &newagenttools.ScheduleState{
		Window: newagenttools.ScheduleWindow{
			TotalDays:  len(days),
			DayMapping: days,
		},
		Tasks: tasks,
	}
}

// defaultDays 返回 3 天的 DayMapping：day1=week3/dow1, day2=week3/dow2, day3=week3/dow3
func defaultDays() []newagenttools.DayMapping {
	return []newagenttools.DayMapping{
		{DayIndex: 1, Week: 3, DayOfWeek: 1},
		{DayIndex: 2, Week: 3, DayOfWeek: 2},
		{DayIndex: 3, Week: 3, DayOfWeek: 3},
	}
}

// ==================== DiffScheduleState: task_item place ====================

// TestDiff_PlaceTaskItem_NonEmbed 验证：普通放置 task_item 时 HostEventID=0。
func TestDiff_PlaceTaskItem_NonEmbed(t *testing.T) {
	days := defaultDays()

	original := buildTestState(days, []newagenttools.ScheduleTask{
		{StateID: 1, Source: "task_item", SourceID: 10, Name: "复习线代", Status: "pending", Duration: 2},
	})

	modified := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:  1,
			Source:   "task_item",
			SourceID: 10,
			Name:     "复习线代",
			Status:   "existing",
			Slots:    []newagenttools.TaskSlot{{Day: 1, SlotStart: 1, SlotEnd: 2}},
		},
	})

	changes := DiffScheduleState(original, modified)
	if len(changes) != 1 {
		t.Fatalf("期望 1 个变更，实际 %d 个", len(changes))
	}
	c := changes[0]
	if c.Type != ChangePlace {
		t.Errorf("期望 ChangePlace，实际 %s", c.Type)
	}
	if c.Source != "task_item" || c.SourceID != 10 {
		t.Errorf("source 或 sourceID 错误: %s/%d", c.Source, c.SourceID)
	}
	if c.HostEventID != 0 {
		t.Errorf("非嵌入路径 HostEventID 应为 0，实际 %d", c.HostEventID)
	}
	if len(c.NewCoords) != 2 {
		t.Errorf("期望 2 个节次坐标，实际 %d", len(c.NewCoords))
	}
}

// TestDiff_PlaceTaskItem_Embed 验证：嵌入放置时 HostEventID = 宿主的 SourceID。
func TestDiff_PlaceTaskItem_Embed(t *testing.T) {
	days := defaultDays()

	// 原始：宿主（水课）已安排，guest 待安排
	original := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:  100,
			Source:   "event",
			SourceID: 999, // ScheduleEvent.ID of the host course
			Name:     "高数",
			Status:   "existing",
			CanEmbed: true,
			Slots:    []newagenttools.TaskSlot{{Day: 2, SlotStart: 3, SlotEnd: 4}},
		},
		{StateID: 1, Source: "task_item", SourceID: 10, Name: "复习线代", Status: "pending", Duration: 2},
	})

	hostID := 100
	// 修改后：guest 嵌入到宿主
	modified := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:    100,
			Source:     "event",
			SourceID:   999,
			Name:       "高数",
			Status:     "existing",
			CanEmbed:   true,
			Slots:      []newagenttools.TaskSlot{{Day: 2, SlotStart: 3, SlotEnd: 4}},
			EmbeddedBy: &[]int{1}[0],
		},
		{
			StateID:   1,
			Source:    "task_item",
			SourceID:  10,
			Name:      "复习线代",
			Status:    "existing",
			Slots:     []newagenttools.TaskSlot{{Day: 2, SlotStart: 3, SlotEnd: 4}},
			EmbedHost: &hostID,
		},
	})

	changes := DiffScheduleState(original, modified)
	// 宿主 slots 未变，只有 guest 产生 place 变更
	var placeChange *ScheduleChange
	for i := range changes {
		if changes[i].SourceID == 10 {
			placeChange = &changes[i]
		}
	}
	if placeChange == nil {
		t.Fatal("未找到 task_item 的 place 变更")
	}
	if placeChange.HostEventID != 999 {
		t.Errorf("嵌入路径 HostEventID 应为 999（宿主 SourceID），实际 %d", placeChange.HostEventID)
	}
}

// ==================== DiffScheduleState: task_item unplace ====================

// TestDiff_UnplaceTaskItem_NonEmbed 验证：从普通位置移除时 HostEventID=0。
func TestDiff_UnplaceTaskItem_NonEmbed(t *testing.T) {
	days := defaultDays()

	original := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:  1,
			Source:   "task_item",
			SourceID: 10,
			Name:     "复习线代",
			Status:   "existing",
			Slots:    []newagenttools.TaskSlot{{Day: 1, SlotStart: 5, SlotEnd: 6}},
		},
	})
	modified := buildTestState(days, []newagenttools.ScheduleTask{
		{StateID: 1, Source: "task_item", SourceID: 10, Name: "复习线代", Status: "pending"},
	})

	changes := DiffScheduleState(original, modified)
	if len(changes) != 1 {
		t.Fatalf("期望 1 个变更，实际 %d", len(changes))
	}
	c := changes[0]
	if c.Type != ChangeUnplace {
		t.Errorf("期望 ChangeUnplace，实际 %s", c.Type)
	}
	if c.HostEventID != 0 {
		t.Errorf("普通移除 HostEventID 应为 0，实际 %d", c.HostEventID)
	}
	if len(c.OldCoords) != 2 {
		t.Errorf("期望 2 个旧坐标，实际 %d", len(c.OldCoords))
	}
}

// TestDiff_UnplaceTaskItem_Embed 验证：从嵌入位置移除时 HostEventID = 宿主 SourceID。
func TestDiff_UnplaceTaskItem_Embed(t *testing.T) {
	days := defaultDays()
	hostStateID := 100

	original := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:    100,
			Source:     "event",
			SourceID:   999,
			Name:       "高数",
			Status:     "existing",
			CanEmbed:   true,
			Slots:      []newagenttools.TaskSlot{{Day: 2, SlotStart: 3, SlotEnd: 4}},
			EmbeddedBy: &[]int{1}[0],
		},
		{
			StateID:   1,
			Source:    "task_item",
			SourceID:  10,
			Name:      "复习线代",
			Status:    "existing",
			Slots:     []newagenttools.TaskSlot{{Day: 2, SlotStart: 3, SlotEnd: 4}},
			EmbedHost: &hostStateID,
		},
	})
	modified := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:  100,
			Source:   "event",
			SourceID: 999,
			Name:     "高数",
			Status:   "existing",
			CanEmbed: true,
			Slots:    []newagenttools.TaskSlot{{Day: 2, SlotStart: 3, SlotEnd: 4}},
		},
		{StateID: 1, Source: "task_item", SourceID: 10, Name: "复习线代", Status: "pending"},
	})

	changes := DiffScheduleState(original, modified)
	var unplaceChange *ScheduleChange
	for i := range changes {
		if changes[i].SourceID == 10 {
			unplaceChange = &changes[i]
		}
	}
	if unplaceChange == nil {
		t.Fatal("未找到 task_item 的 unplace 变更")
	}
	if unplaceChange.HostEventID != 999 {
		t.Errorf("嵌入移除 HostEventID 应为 999，实际 %d", unplaceChange.HostEventID)
	}
}

// ==================== DiffScheduleState: task_item move ====================

// TestDiff_MoveTaskItem 验证：task_item 移动时 OldHostEventID 和 HostEventID 分别对应旧/新位置宿主。
func TestDiff_MoveTaskItem_NonEmbedToNonEmbed(t *testing.T) {
	days := defaultDays()

	original := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:  1,
			Source:   "task_item",
			SourceID: 10,
			Name:     "复习线代",
			Status:   "existing",
			Slots:    []newagenttools.TaskSlot{{Day: 1, SlotStart: 1, SlotEnd: 2}},
		},
	})
	modified := buildTestState(days, []newagenttools.ScheduleTask{
		{
			StateID:  1,
			Source:   "task_item",
			SourceID: 10,
			Name:     "复习线代",
			Status:   "existing",
			Slots:    []newagenttools.TaskSlot{{Day: 2, SlotStart: 5, SlotEnd: 6}},
		},
	})

	changes := DiffScheduleState(original, modified)
	if len(changes) != 1 {
		t.Fatalf("期望 1 个变更，实际 %d", len(changes))
	}
	c := changes[0]
	if c.Type != ChangeMove {
		t.Errorf("期望 ChangeMove，实际 %s", c.Type)
	}
	if c.HostEventID != 0 || c.OldHostEventID != 0 {
		t.Errorf("非嵌入移动两个 HostEventID 均应为 0，实际 %d/%d", c.OldHostEventID, c.HostEventID)
	}
	if len(c.OldCoords) != 2 || len(c.NewCoords) != 2 {
		t.Errorf("旧坐标 %d 个，新坐标 %d 个，均期望 2 个", len(c.OldCoords), len(c.NewCoords))
	}
}

// ==================== resolveHostEventID ====================

func TestResolveHostEventID_NoEmbed(t *testing.T) {
	task := &newagenttools.ScheduleTask{StateID: 1, EmbedHost: nil}
	state := buildTestState(defaultDays(), nil)
	if got := resolveHostEventID(task, state); got != 0 {
		t.Errorf("无嵌入时应返回 0，实际 %d", got)
	}
}

func TestResolveHostEventID_WithEmbed(t *testing.T) {
	hostID := 100
	task := &newagenttools.ScheduleTask{StateID: 1, EmbedHost: &hostID}
	state := buildTestState(defaultDays(), []newagenttools.ScheduleTask{
		{StateID: 100, Source: "event", SourceID: 999},
	})
	if got := resolveHostEventID(task, state); got != 999 {
		t.Errorf("期望宿主 SourceID=999，实际 %d", got)
	}
}
