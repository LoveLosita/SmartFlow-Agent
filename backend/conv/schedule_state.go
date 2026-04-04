package conv

import (
	"sort"

	"github.com/LoveLosita/smartflow/backend/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
)

// WindowDay represents a single day in the planning window.
type WindowDay struct {
	Week      int
	DayOfWeek int
}

// ==================== Load: DB → State ====================

// LoadScheduleState builds a ScheduleState from database query results.
//
// Parameters:
//   - schedules: existing Schedule records in the window (must preload Event and EmbeddedTask)
//   - taskClasses: TaskClasses being scheduled (must preload Items)
//   - extraItemCategories: optional TaskClassItem.ID → category name,
//     for task events not belonging to the provided taskClasses
//   - windowDays: ordered (week, day_of_week) pairs defining the planning window
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

	// --- Step 1: Build day mapping and lookup index ---
	dayLookup := make(map[[2]int]int, len(windowDays))
	for i, wd := range windowDays {
		idx := i + 1
		state.Window.DayMapping[i] = newagenttools.DayMapping{
			DayIndex:  idx,
			Week:      wd.Week,
			DayOfWeek: wd.DayOfWeek,
		}
		dayLookup[[2]int{wd.Week, wd.DayOfWeek}] = idx
	}

	// --- Step 2: Build itemID → categoryName lookup ---
	// extraItemCategories first (lower priority), then taskClasses overwrites (higher priority).
	itemCategoryLookup := make(map[int]string)
	for id, name := range extraItemCategories {
		itemCategoryLookup[id] = name
	}
	for _, tc := range taskClasses {
		catName := "任务"
		if tc.Name != nil {
			catName = *tc.Name
		}
		for _, item := range tc.Items {
			itemCategoryLookup[item.ID] = catName
		}
	}

	// --- Step 3: Process existing schedules → existing tasks ---
	type slotGroup struct {
		week      int
		dayOfWeek int
		sections  []int
	}

	eventSlotMap := make(map[int][]slotGroup) // eventID → groups
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
	eventStateIDs := make(map[int]int) // eventID → stateID

	for eventID, groups := range eventSlotMap {
		event := eventInfo[eventID]

		// Category
		category := "课程"
		if event.Type == "task" {
			category = "任务"
			if event.RelID != nil {
				if cat, ok := itemCategoryLookup[*event.RelID]; ok {
					category = cat
				}
			}
		}

		// Locked: course + not embeddable
		locked := event.Type == "course" && !event.CanBeEmbedded

		// Compress sections into slot ranges
		var slots []newagenttools.TaskSlot
		for _, g := range groups {
			sort.Ints(g.sections)
			start, end := g.sections[0], g.sections[0]
			for _, sec := range g.sections[1:] {
				if sec == end+1 {
					end = sec
				} else {
					if day, ok := dayLookup[[2]int{g.week, g.dayOfWeek}]; ok {
						slots = append(slots, newagenttools.TaskSlot{Day: day, SlotStart: start, SlotEnd: end})
					}
					start, end = sec, sec
				}
			}
			if day, ok := dayLookup[[2]int{g.week, g.dayOfWeek}]; ok {
				slots = append(slots, newagenttools.TaskSlot{Day: day, SlotStart: start, SlotEnd: end})
			}
		}

		// Sort slots by day, then slot_start
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

	// --- Step 4: Process pending task items → pending tasks ---
	itemStateIDs := make(map[int]int) // TaskClassItem.ID → stateID

	for _, tc := range taskClasses {
		catName := "任务"
		if tc.Name != nil {
			catName = *tc.Name
		}
		catID := tc.ID

		for _, item := range tc.Items {
			if item.Status == nil || *item.Status != model.TaskItemStatusUnscheduled {
				continue
			}

			duration := 2
			if tc.TotalSlots != nil && *tc.TotalSlots > 0 && len(tc.Items) > 0 {
				if d := *tc.TotalSlots / len(tc.Items); d > 0 {
					duration = d
				}
			}

			name := ""
			if item.Content != nil {
				name = *item.Content
			}

			stateID := nextStateID
			state.Tasks = append(state.Tasks, newagenttools.ScheduleTask{
				StateID:    stateID,
				Source:     "task_item",
				SourceID:   item.ID,
				Name:       name,
				Category:   catName,
				Status:     "pending",
				Duration:   duration,
				CategoryID: catID,
			})
			itemStateIDs[item.ID] = stateID
			nextStateID++
		}
	}

	// --- Step 5: Resolve embed relationships ---
	// Extend itemStateIDs with existing task events (rel_id → stateID)
	for eventID, stateID := range eventStateIDs {
		event := eventInfo[eventID]
		if event.Type == "task" && event.RelID != nil {
			if _, exists := itemStateIDs[*event.RelID]; !exists {
				itemStateIDs[*event.RelID] = stateID
			}
		}
	}

	for i := range schedules {
		s := &schedules[i]
		if s.EmbeddedTaskID == nil || s.Event == nil {
			continue
		}
		hostStateID, ok := eventStateIDs[s.EventID]
		if !ok {
			continue
		}
		guestStateID, ok := itemStateIDs[*s.EmbeddedTaskID]
		if !ok {
			continue
		}

		// Host: record which guest is embedded
		hostTask := state.TaskByStateID(hostStateID)
		if hostTask != nil && hostTask.EmbeddedBy == nil {
			v := guestStateID
			hostTask.EmbeddedBy = &v
		}

		// Guest: record which host it's embedded into, copy host's slots
		guestTask := state.TaskByStateID(guestStateID)
		if guestTask != nil && guestTask.EmbedHost == nil {
			v := hostStateID
			guestTask.EmbedHost = &v
		}
	}

	return state
}

// ==================== Diff: State comparison ====================

// ScheduleChangeType classifies the type of state change.
type ScheduleChangeType string

const (
	ChangePlace   ScheduleChangeType = "place"   // pending → placed
	ChangeMove    ScheduleChangeType = "move"    // slots relocated
	ChangeUnplace ScheduleChangeType = "unplace" // placed → pending
)

// SlotCoord is an individual section position in DB coordinates (week, day_of_week, section).
type SlotCoord struct {
	Week      int
	DayOfWeek int
	Section   int
}

// ScheduleChange represents a single task change between original and modified state.
type ScheduleChange struct {
	Type       ScheduleChangeType
	StateID    int
	Source     string // "event" | "task_item"
	SourceID   int    // ScheduleEvent.ID or TaskClassItem.ID
	EventType  string // "course" | "task" (source=event only)
	CategoryID int    // source=task_item only
	Name       string

	// For place/move: new slot positions (expanded to individual sections)
	NewCoords []SlotCoord
	// For move/unplace: old slot positions
	OldCoords []SlotCoord
}

// DiffScheduleState compares original and modified ScheduleState,
// returning the changes that need to be persisted to the database.
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
		// Place: pending → has slots
		case wasPending && hasSlots:
			changes = append(changes, ScheduleChange{
				Type:       ChangePlace,
				StateID:    mod.StateID,
				Source:     mod.Source,
				SourceID:   mod.SourceID,
				EventType:  mod.EventType,
				CategoryID: mod.CategoryID,
				Name:       mod.Name,
				NewCoords:  expandToCoords(mod.Slots, modified),
			})

		// Move: had slots → different slots
		case hadSlots && hasSlots && !slotsEqual(orig.Slots, mod.Slots):
			changes = append(changes, ScheduleChange{
				Type:       ChangeMove,
				StateID:    mod.StateID,
				Source:     mod.Source,
				SourceID:   mod.SourceID,
				EventType:  mod.EventType,
				CategoryID: mod.CategoryID,
				Name:       mod.Name,
				OldCoords:  expandToCoords(orig.Slots, original),
				NewCoords:  expandToCoords(mod.Slots, modified),
			})

		// Unplace: had slots → no slots
		case hadSlots && !hasSlots:
			changes = append(changes, ScheduleChange{
				Type:      ChangeUnplace,
				StateID:   mod.StateID,
				Source:    orig.Source,
				SourceID:  orig.SourceID,
				EventType: orig.EventType,
				Name:      orig.Name,
				OldCoords: expandToCoords(orig.Slots, original),
			})
		}
	}

	return changes
}

// indexByStateID creates a map of stateID → *ScheduleTask.
func indexByStateID(state *newagenttools.ScheduleState) map[int]*newagenttools.ScheduleTask {
	m := make(map[int]*newagenttools.ScheduleTask, len(state.Tasks))
	for i := range state.Tasks {
		m[state.Tasks[i].StateID] = &state.Tasks[i]
	}
	return m
}

// slotsEqual compares two TaskSlot slices for equality.
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

// expandToCoords converts compressed TaskSlots to individual SlotCoords.
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
