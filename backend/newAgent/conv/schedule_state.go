package newagentconv

import (
	"sort"

	"github.com/LoveLosita/smartflow/backend/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

// WindowDay 表示排课窗口中的一天（相对周 + 周几）。
type WindowDay struct {
	Week      int
	DayOfWeek int
}

// LoadScheduleState 将数据库层的 schedules + taskClasses 聚合为 newAgent 工具层可直接操作的 ScheduleState。
//
// 职责边界：
// 1. 只负责数据映射与状态归一，不做数据库读写；
// 2. 同时兼容三种“任务已落位”信号：event.rel_id、schedules.embedded_task_id、task_item.embedded_time；
// 3. 对嵌入课程任务优先判定为 existing，避免误挂回 pending。
func LoadScheduleState(
	schedules []model.Schedule,
	taskClasses []model.TaskClass,
	extraItemCategories map[int]string,
	windowDays []WindowDay,
) *newagenttools.ScheduleState {
	state := &newagenttools.ScheduleState{
		Window: newagenttools.ScheduleWindow{
			TotalDays:  len(windowDays),
			DayMapping: make([]newagenttools.DayMapping, len(windowDays)),
		},
		Tasks: make([]newagenttools.ScheduleTask, 0),
	}

	// 1. 构建 day_index 与 (week, day_of_week) 的双向转换基础索引。
	dayLookup := make(map[[2]int]int, len(windowDays))
	for i, wd := range windowDays {
		dayIndex := i + 1
		state.Window.DayMapping[i] = newagenttools.DayMapping{
			DayIndex:  dayIndex,
			Week:      wd.Week,
			DayOfWeek: wd.DayOfWeek,
		}
		dayLookup[[2]int{wd.Week, wd.DayOfWeek}] = dayIndex
	}

	// 2. 构建 task_item -> 分类名映射。
	// 2.1 先放 extraItemCategories（低优先级，兜底）；
	// 2.2 再用 taskClasses 覆盖（高优先级，确保本轮排课分类准确）。
	itemCategoryLookup := make(map[int]string)
	for id, name := range extraItemCategories {
		itemCategoryLookup[id] = name
	}
	for _, tc := range taskClasses {
		catName := "任务"
		if tc.Name != nil && *tc.Name != "" {
			catName = *tc.Name
		}
		for _, item := range tc.Items {
			itemCategoryLookup[item.ID] = catName
		}
	}

	// 3. 先把 schedules 聚合成 event 任务（existing）。
	type slotGroup struct {
		week      int
		dayOfWeek int
		sections  []int
	}
	eventSlotMap := make(map[int][]slotGroup) // eventID -> 多天多段槽位
	eventInfo := make(map[int]*model.ScheduleEvent)

	for i := range schedules {
		s := &schedules[i]
		if s.Event == nil {
			continue
		}
		if _, exists := eventInfo[s.EventID]; !exists {
			eventInfo[s.EventID] = s.Event
		}

		groups := eventSlotMap[s.EventID]
		found := false
		for gi := range groups {
			if groups[gi].week == s.Week && groups[gi].dayOfWeek == s.DayOfWeek {
				groups[gi].sections = append(groups[gi].sections, s.Section)
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, slotGroup{
				week:      s.Week,
				dayOfWeek: s.DayOfWeek,
				sections:  []int{s.Section},
			})
		}
		eventSlotMap[s.EventID] = groups
	}

	nextStateID := 1
	eventStateIDs := make(map[int]int) // eventID -> stateID
	for eventID, groups := range eventSlotMap {
		event := eventInfo[eventID]
		if event == nil {
			continue
		}

		category := "课程"
		if event.Type == "task" {
			category = "任务"
			if event.RelID != nil {
				if cat, ok := itemCategoryLookup[*event.RelID]; ok && cat != "" {
					category = cat
				}
			}
		}

		locked := event.Type == "course" && !event.CanBeEmbedded
		var slots []newagenttools.TaskSlot
		for _, g := range groups {
			if len(g.sections) == 0 {
				continue
			}
			sort.Ints(g.sections)
			start, end := g.sections[0], g.sections[0]
			for _, sec := range g.sections[1:] {
				if sec == end+1 {
					end = sec
					continue
				}
				if day, ok := dayLookup[[2]int{g.week, g.dayOfWeek}]; ok {
					slots = append(slots, newagenttools.TaskSlot{Day: day, SlotStart: start, SlotEnd: end})
				}
				start, end = sec, sec
			}
			if day, ok := dayLookup[[2]int{g.week, g.dayOfWeek}]; ok {
				slots = append(slots, newagenttools.TaskSlot{Day: day, SlotStart: start, SlotEnd: end})
			}
		}
		sort.Slice(slots, func(i, j int) bool {
			if slots[i].Day != slots[j].Day {
				return slots[i].Day < slots[j].Day
			}
			return slots[i].SlotStart < slots[j].SlotStart
		})

		stateID := nextStateID
		state.Tasks = append(state.Tasks, newagenttools.ScheduleTask{
			StateID:   stateID,
			Source:    "event",
			SourceID:  eventID,
			Name:      event.Name,
			Category:  category,
			Status:    "existing",
			Locked:    locked,
			Slots:     slots,
			CanEmbed:  event.CanBeEmbedded,
			EventType: event.Type,
		})
		eventStateIDs[eventID] = stateID
		nextStateID++
	}

	// 4. 构建 task_item 占位索引（后续 pending 判定优先用这两个索引短路）。
	// 4.1 event.rel_id 占位：该 item 已有 task event；
	// 4.2 schedules.embedded_task_id 占位：该 item 已嵌入到课程槽位。
	itemIDToTaskEventStateID := make(map[int]int)
	for eventID, stateID := range eventStateIDs {
		event := eventInfo[eventID]
		if event == nil || event.Type != "task" || event.RelID == nil {
			continue
		}
		itemIDToTaskEventStateID[*event.RelID] = stateID
	}

	itemIDToEmbedHostStateID := make(map[int]int)
	for i := range schedules {
		s := &schedules[i]
		if s.EmbeddedTaskID == nil {
			continue
		}
		hostStateID, ok := eventStateIDs[s.EventID]
		if !ok {
			continue
		}
		itemIDToEmbedHostStateID[*s.EmbeddedTaskID] = hostStateID
	}

	// 5. 处理 task_items：
	// 5.1 先消化 existing（task event / 课程嵌入 / embedded_time）；
	// 5.2 剩余条目再按 status 判 pending。
	itemStateIDs := make(map[int]int) // task_item_id -> stateID
	for _, tc := range taskClasses {
		catName := "任务"
		if tc.Name != nil && *tc.Name != "" {
			catName = *tc.Name
		}
		defaultDuration := estimateTaskItemDuration(tc)
		pendingCount := 0

		for _, item := range tc.Items {
			if stateID, ok := itemIDToTaskEventStateID[item.ID]; ok {
				itemStateIDs[item.ID] = stateID
				continue
			}

			if hostStateID, ok := itemIDToEmbedHostStateID[item.ID]; ok {
				hostSlots := []newagenttools.TaskSlot(nil)
				if hostTask := state.TaskByStateID(hostStateID); hostTask != nil {
					hostSlots = cloneTaskSlots(hostTask.Slots)
				}
				stateID := nextStateID
				state.Tasks = append(state.Tasks, newagenttools.ScheduleTask{
					StateID:     stateID,
					Source:      "task_item",
					SourceID:    item.ID,
					Name:        taskItemName(item),
					Category:    catName,
					Status:      "existing",
					Slots:       hostSlots,
					CategoryID:  tc.ID,
					TaskClassID: tc.ID,
				})
				itemStateIDs[item.ID] = stateID
				nextStateID++
				continue
			}

			if slots, ok := slotsFromTargetTime(item.EmbeddedTime, dayLookup); ok {
				stateID := nextStateID
				state.Tasks = append(state.Tasks, newagenttools.ScheduleTask{
					StateID:     stateID,
					Source:      "task_item",
					SourceID:    item.ID,
					Name:        taskItemName(item),
					Category:    catName,
					Status:      "existing",
					Slots:       slots,
					CategoryID:  tc.ID,
					TaskClassID: tc.ID,
				})
				itemStateIDs[item.ID] = stateID
				nextStateID++
				continue
			}

			if !isTaskItemPending(item) {
				continue
			}

			stateID := nextStateID
			state.Tasks = append(state.Tasks, newagenttools.ScheduleTask{
				StateID:     stateID,
				Source:      "task_item",
				SourceID:    item.ID,
				Name:        taskItemName(item),
				Category:    catName,
				Status:      "pending",
				Duration:    defaultDuration,
				CategoryID:  tc.ID,
				TaskClassID: tc.ID,
			})
			itemStateIDs[item.ID] = stateID
			nextStateID++
			pendingCount++
		}

		// 仅当该任务类仍有 pending item 时，才把约束暴露给 LLM。
		if pendingCount > 0 {
			meta := newagenttools.TaskClassMeta{
				ID:   tc.ID,
				Name: catName,
			}
			if tc.Strategy != nil {
				meta.Strategy = *tc.Strategy
			}
			if tc.TotalSlots != nil {
				meta.TotalSlots = *tc.TotalSlots
			}
			if tc.AllowFillerCourse != nil {
				meta.AllowFillerCourse = *tc.AllowFillerCourse
			}
			if tc.ExcludedSlots != nil {
				meta.ExcludedSlots = []int(tc.ExcludedSlots)
			}
			if tc.StartDate != nil {
				meta.StartDate = tc.StartDate.Format("2006-01-02")
			}
			if tc.EndDate != nil {
				meta.EndDate = tc.EndDate.Format("2006-01-02")
			}
			state.TaskClasses = append(state.TaskClasses, meta)
		}
	}

	// 6. 统一回填嵌入关系：
	// 6.1 host 记录 EmbeddedBy；
	// 6.2 guest 记录 EmbedHost；
	// 6.3 guest 强制 existing + host slots，防止“嵌入任务残留 pending”。
	for i := range schedules {
		s := &schedules[i]
		if s.EmbeddedTaskID == nil {
			continue
		}
		hostStateID, ok := eventStateIDs[s.EventID]
		if !ok {
			continue
		}
		hostTask := state.TaskByStateID(hostStateID)
		itemID := *s.EmbeddedTaskID

		guestStateID, ok := itemStateIDs[itemID]
		if !ok {
			// 兜底：只在 schedules 层看到嵌入关系，taskClasses 不含该 item 时补建 guest。
			name := ""
			categoryID := 0
			taskClassID := 0
			if s.EmbeddedTask != nil {
				name = taskItemName(*s.EmbeddedTask)
				if s.EmbeddedTask.CategoryID != nil {
					categoryID = *s.EmbeddedTask.CategoryID
					taskClassID = *s.EmbeddedTask.CategoryID
				}
			}
			category := "任务"
			if cat, exists := itemCategoryLookup[itemID]; exists && cat != "" {
				category = cat
			}
			hostSlots := []newagenttools.TaskSlot(nil)
			if hostTask != nil {
				hostSlots = cloneTaskSlots(hostTask.Slots)
			}
			guestStateID = nextStateID
			state.Tasks = append(state.Tasks, newagenttools.ScheduleTask{
				StateID:     guestStateID,
				Source:      "task_item",
				SourceID:    itemID,
				Name:        name,
				Category:    category,
				Status:      "existing",
				Slots:       hostSlots,
				CategoryID:  categoryID,
				TaskClassID: taskClassID,
			})
			itemStateIDs[itemID] = guestStateID
			nextStateID++
		}

		if hostTask != nil && hostTask.EmbeddedBy == nil {
			v := guestStateID
			hostTask.EmbeddedBy = &v
		}

		guestTask := state.TaskByStateID(guestStateID)
		if guestTask == nil {
			continue
		}
		if guestTask.EmbedHost == nil {
			v := hostStateID
			guestTask.EmbedHost = &v
		}
		guestTask.Status = "existing"
		if hostTask != nil && len(guestTask.Slots) == 0 {
			guestTask.Slots = cloneTaskSlots(hostTask.Slots)
		}
		// existing 的 task_item 不应再携带 Duration，避免预览层误判成 suggested。
		guestTask.Duration = 0
	}

	return state
}

// isTaskItemPending 仅根据 status 判断是否应进入 pending 池。
//
// 说明：
// 1. status=nil 兼容历史数据，按“未安排”处理；
// 2. 仅 status=TaskItemStatusUnscheduled 进入 pending；
// 3. 其它“已安排”信号由 LoadScheduleState 主流程统一判定，避免多处口径不一致。
func isTaskItemPending(item model.TaskClassItem) bool {
	if item.Status == nil {
		return true
	}
	return *item.Status == model.TaskItemStatusUnscheduled
}

// estimateTaskItemDuration 估算 pending 任务默认时长。
//
// 规则：若任务类声明了 total_slots，则按 total_slots / item_count 取整（最少 1）；
// 否则回退到 2 节。
func estimateTaskItemDuration(tc model.TaskClass) int {
	duration := 2
	if tc.TotalSlots != nil && *tc.TotalSlots > 0 && len(tc.Items) > 0 {
		if d := *tc.TotalSlots / len(tc.Items); d > 0 {
			duration = d
		}
	}
	return duration
}

// taskItemName 读取任务项展示名。
func taskItemName(item model.TaskClassItem) string {
	if item.Content == nil {
		return ""
	}
	return *item.Content
}

// slotsFromTargetTime 将 task_items.embedded_time 转换为 state 的槽位结构。
// 若 target 为空、节次非法、或不在窗口内，返回 false。
func slotsFromTargetTime(
	target *model.TargetTime,
	dayLookup map[[2]int]int,
) ([]newagenttools.TaskSlot, bool) {
	if target == nil {
		return nil, false
	}
	if target.SectionFrom < 1 || target.SectionTo < target.SectionFrom {
		return nil, false
	}
	day, ok := dayLookup[[2]int{target.Week, target.DayOfWeek}]
	if !ok {
		return nil, false
	}
	return []newagenttools.TaskSlot{
		{
			Day:       day,
			SlotStart: target.SectionFrom,
			SlotEnd:   target.SectionTo,
		},
	}, true
}

// ScheduleChangeType 表示两份 ScheduleState 对比后的变更类型。
type ScheduleChangeType string

const (
	ChangePlace   ScheduleChangeType = "place"   // 从 pending 变为已放置
	ChangeMove    ScheduleChangeType = "move"    // 已有槽位发生移动
	ChangeUnplace ScheduleChangeType = "unplace" // 从已放置变回 pending
)

// SlotCoord 表示数据库坐标系中的单节槽位（week/day_of_week/section）。
type SlotCoord struct {
	Week      int
	DayOfWeek int
	Section   int
}

// ScheduleChange 描述单个任务在前后状态间的变化。
type ScheduleChange struct {
	Type       ScheduleChangeType
	StateID    int
	Source     string // "event" | "task_item"
	SourceID   int    // ScheduleEvent.ID 或 TaskClassItem.ID
	EventType  string // 仅 source=event 时有意义（course/task）
	CategoryID int    // 仅 source=task_item 时有意义
	Name       string

	// place/move 的新位置（展开到逐节坐标）。
	NewCoords []SlotCoord
	// move/unplace 的旧位置（展开到逐节坐标）。
	OldCoords []SlotCoord

	// HostEventID：变更后位置对应的宿主 event（非嵌入为 0）。
	HostEventID int
	// OldHostEventID：move 时旧位置对应的宿主 event（非嵌入为 0）。
	OldHostEventID int
}

// DiffScheduleState 比较 original 与 modified，返回需要持久化的变更集合。
func DiffScheduleState(
	original *newagenttools.ScheduleState,
	modified *newagenttools.ScheduleState,
) []ScheduleChange {
	if original == nil || modified == nil {
		return nil
	}

	origTasks := indexByStateID(original)
	var changes []ScheduleChange

	for i := range modified.Tasks {
		mod := &modified.Tasks[i]
		orig := origTasks[mod.StateID]

		wasPending := orig == nil || orig.Status == "pending"
		hasSlots := len(mod.Slots) > 0
		hadSlots := orig != nil && len(orig.Slots) > 0

		switch {
		case wasPending && hasSlots:
			changes = append(changes, ScheduleChange{
				Type:        ChangePlace,
				StateID:     mod.StateID,
				Source:      mod.Source,
				SourceID:    mod.SourceID,
				EventType:   mod.EventType,
				CategoryID:  mod.CategoryID,
				Name:        mod.Name,
				NewCoords:   expandToCoords(mod.Slots, modified),
				HostEventID: resolveHostEventID(mod, modified),
			})
		case hadSlots && hasSlots && !slotsEqual(orig.Slots, mod.Slots):
			changes = append(changes, ScheduleChange{
				Type:           ChangeMove,
				StateID:        mod.StateID,
				Source:         mod.Source,
				SourceID:       mod.SourceID,
				EventType:      mod.EventType,
				CategoryID:     mod.CategoryID,
				Name:           mod.Name,
				OldCoords:      expandToCoords(orig.Slots, original),
				NewCoords:      expandToCoords(mod.Slots, modified),
				HostEventID:    resolveHostEventID(mod, modified),
				OldHostEventID: resolveHostEventID(orig, original),
			})
		case hadSlots && !hasSlots:
			changes = append(changes, ScheduleChange{
				Type:        ChangeUnplace,
				StateID:     mod.StateID,
				Source:      orig.Source,
				SourceID:    orig.SourceID,
				EventType:   orig.EventType,
				Name:        orig.Name,
				OldCoords:   expandToCoords(orig.Slots, original),
				HostEventID: resolveHostEventID(orig, original),
			})
		}
	}

	return changes
}

// indexByStateID 将任务列表按 state_id 建立索引。
func indexByStateID(state *newagenttools.ScheduleState) map[int]*newagenttools.ScheduleTask {
	m := make(map[int]*newagenttools.ScheduleTask, len(state.Tasks))
	for i := range state.Tasks {
		m[state.Tasks[i].StateID] = &state.Tasks[i]
	}
	return m
}

// slotsEqual 判断两个压缩槽位切片是否完全一致。
func slotsEqual(a, b []newagenttools.TaskSlot) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// cloneTaskSlots 深拷贝槽位切片。
func cloneTaskSlots(src []newagenttools.TaskSlot) []newagenttools.TaskSlot {
	if len(src) == 0 {
		return nil
	}
	dst := make([]newagenttools.TaskSlot, len(src))
	copy(dst, src)
	return dst
}

// resolveHostEventID 通过任务的 EmbedHost 反查宿主 event_id。
// 非嵌入任务或宿主不存在时返回 0。
func resolveHostEventID(task *newagenttools.ScheduleTask, state *newagenttools.ScheduleState) int {
	if task == nil || task.EmbedHost == nil {
		return 0
	}
	host := state.TaskByStateID(*task.EmbedHost)
	if host == nil {
		return 0
	}
	return host.SourceID
}

// expandToCoords 将压缩槽位展开成逐节坐标，便于后续持久化层处理。
func expandToCoords(slots []newagenttools.TaskSlot, state *newagenttools.ScheduleState) []SlotCoord {
	var coords []SlotCoord
	for _, slot := range slots {
		week, dow, ok := state.DayToWeekDay(slot.Day)
		if !ok {
			continue
		}
		for sec := slot.SlotStart; sec <= slot.SlotEnd; sec++ {
			coords = append(coords, SlotCoord{Week: week, DayOfWeek: dow, Section: sec})
		}
	}
	return coords
}
