package newagenttools

// DayMapping maps a day_index to a real (week, day_of_week) coordinate.
type DayMapping struct {
	DayIndex  int `json:"day_index"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
}

// ScheduleWindow defines the planning window.
type ScheduleWindow struct {
	TotalDays  int          `json:"total_days"`
	DayMapping []DayMapping `json:"day_mapping"`
}

// TaskSlot is a compressed time slot using day_index and section range.
type TaskSlot struct {
	Day       int `json:"day"`
	SlotStart int `json:"slot_start"`
	SlotEnd   int `json:"slot_end"`
}

// ScheduleTask is a unified task representation in the tool state.
// It merges existing schedules (from schedule_events) and pending tasks (from task_items)
// into one flat list that the tool layer operates on.
type ScheduleTask struct {
	StateID  int    `json:"state_id"`
	Source   string `json:"source"`    // "event" | "task_item"
	SourceID int    `json:"source_id"` // ScheduleEvent.ID or TaskClassItem.ID
	Name     string `json:"name"`
	Category string `json:"category"` // e.g. "课程", "学习", "作业"
	Status   string `json:"status"`   // "existing" | "pending"
	Locked   bool   `json:"locked"`

	// Existing task: compressed slot ranges. Pending task: nil until placed.
	Slots []TaskSlot `json:"slots,omitempty"`
	// Pending task: required consecutive slot count.
	Duration int `json:"duration,omitempty"`
	// source=task_item only: TaskClass.ID for category lookup.
	CategoryID int `json:"category_id,omitempty"`
	// source=event only: whether this slot allows embedding other tasks.
	CanEmbed bool `json:"can_embed,omitempty"`

	// Embed relationships (resolved after all tasks are loaded).
	EmbeddedBy *int `json:"embedded_by,omitempty"` // host: which state_id is embedded into me
	EmbedHost  *int `json:"embed_host,omitempty"`  // guest: which state_id's slot I'm embedded into

	// Internal: not exposed to LLM, used for flush/diff logic.
	EventType string `json:"event_type,omitempty"` // "course" | "task" (source=event only)
}

// ScheduleState is the full tool operation state.
type ScheduleState struct {
	Window ScheduleWindow `json:"window"`
	Tasks  []ScheduleTask `json:"tasks"`
}

// DayToWeekDay converts day_index to (week, day_of_week).
func (s *ScheduleState) DayToWeekDay(day int) (week, dayOfWeek int, ok bool) {
	for _, m := range s.Window.DayMapping {
		if m.DayIndex == day {
			return m.Week, m.DayOfWeek, true
		}
	}
	return 0, 0, false
}

// WeekDayToDay converts (week, day_of_week) to day_index.
func (s *ScheduleState) WeekDayToDay(week, dayOfWeek int) (day int, ok bool) {
	for _, m := range s.Window.DayMapping {
		if m.Week == week && m.DayOfWeek == dayOfWeek {
			return m.DayIndex, true
		}
	}
	return 0, false
}

// TaskByStateID finds a task by state_id. Returns nil if not found.
func (s *ScheduleState) TaskByStateID(stateID int) *ScheduleTask {
	for i := range s.Tasks {
		if s.Tasks[i].StateID == stateID {
			return &s.Tasks[i]
		}
	}
	return nil
}

// Clone returns a deep copy of the ScheduleState.
func (s *ScheduleState) Clone() *ScheduleState {
	if s == nil {
		return nil
	}
	clone := &ScheduleState{
		Window: ScheduleWindow{
			TotalDays:  s.Window.TotalDays,
			DayMapping: make([]DayMapping, len(s.Window.DayMapping)),
		},
		Tasks: make([]ScheduleTask, len(s.Tasks)),
	}
	copy(clone.Window.DayMapping, s.Window.DayMapping)
	for i, t := range s.Tasks {
		clone.Tasks[i] = t
		if t.Slots != nil {
			clone.Tasks[i].Slots = make([]TaskSlot, len(t.Slots))
			copy(clone.Tasks[i].Slots, t.Slots)
		}
		if t.EmbeddedBy != nil {
			v := *t.EmbeddedBy
			clone.Tasks[i].EmbeddedBy = &v
		}
		if t.EmbedHost != nil {
			v := *t.EmbedHost
			clone.Tasks[i].EmbedHost = &v
		}
	}
	return clone
}
