package schedule

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

// TaskClassMeta 是任务类级别的调度与认知画像元数据。
//
// 职责边界：
// 1. 负责向 LLM 暴露会影响粗排与主动优化判断的高价值字段；
// 2. 不负责暴露数据库内部细节，也不承载 task_item 级别的数据；
// 3. 这些字段会被 prompt、analyze_health、analyze_rhythm 共同消费，因此要保持轻量且稳定。
type TaskClassMeta struct {
	ID                 int    `json:"id"`
	Name               string `json:"name"`
	Strategy           string `json:"strategy"`               // "steady"=均匀分布 | "rapid"=集中突击
	TotalSlots         int    `json:"total_slots"`            // 该任务类总时段预算
	AllowFillerCourse  bool   `json:"allow_filler_course"`    // 是否允许嵌入水课时段
	ExcludedSlots      []int  `json:"excluded_slots"`         // 排除的半天时段索引（空=无限制）
	ExcludedDaysOfWeek []int  `json:"excluded_days_of_week"`  // 排除的星期几（1-7，空=无限制）
	StartDate          string `json:"start_date,omitempty"`   // 排程起始日期（YYYY-MM-DD）
	EndDate            string `json:"end_date,omitempty"`     // 排程截止日期（YYYY-MM-DD）
	SubjectType        string `json:"subject_type,omitempty"` // "quantitative" | "memory" | "reading" | "mixed"
	DifficultyLevel    string `json:"difficulty_level,omitempty"`
	CognitiveIntensity string `json:"cognitive_intensity,omitempty"`
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
	Status   string `json:"status"`   // "existing" | "suggested" | "pending"
	Locked   bool   `json:"locked"`

	// Existing / suggested task: compressed slot ranges. Pending task: nil until placed.
	Slots []TaskSlot `json:"slots,omitempty"`
	// Pending / suggested task: required consecutive slot count.
	Duration int `json:"duration,omitempty"`
	// source=task_item only: TaskClass.ID，用于反查任务类约束。
	TaskClassID int `json:"task_class_id,omitempty"`
	// source=task_item only: 任务在所属任务类内的稳定顺序。
	// 该字段只用于写工具层的“同任务类内部顺序约束”，不直接暴露给 LLM 做决策。
	TaskOrder int `json:"task_order,omitempty"`
	// source=task_item only: TaskClass.ID for category lookup (internal alias).
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
	Window      ScheduleWindow  `json:"window"`
	Tasks       []ScheduleTask  `json:"tasks"`
	TaskClasses []TaskClassMeta `json:"task_classes,omitempty"` // 任务类约束与语义画像，供 LLM 排课参考
	// RuntimeQueue 是“本轮 execute 微调”的临时待处理队列。
	//
	// 职责边界：
	// 1. 负责承载 LLM 队列化微调时的运行态（待处理/当前处理/已完成/已跳过）；
	// 2. 只用于 agent 运行期，不参与数据库持久化；
	// 3. 支持随 AgentStateSnapshot 一起快照，便于断线恢复后继续处理队首任务。
	RuntimeQueue *TaskProcessingQueue `json:"runtime_queue,omitempty"`
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
		Tasks:       make([]ScheduleTask, len(s.Tasks)),
		TaskClasses: make([]TaskClassMeta, len(s.TaskClasses)),
	}
	copy(clone.Window.DayMapping, s.Window.DayMapping)
	copy(clone.TaskClasses, s.TaskClasses)
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
	clone.RuntimeQueue = cloneTaskProcessingQueue(s.RuntimeQueue)
	return clone
}
