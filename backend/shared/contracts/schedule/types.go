package schedule

import "time"

const (
	TargetTypeTaskPool      = "task_pool"
	TargetTypeTaskItem      = "task_item"
	TargetTypeScheduleEvent = "schedule_event"
)

// UserRequest 是 schedule 只按用户读取数据的通用请求。
//
// 职责边界：
// 1. 只承载鉴权后得到的 user_id；
// 2. 不承载 token、角色或 HTTP 参数；
// 3. 业务校验由 schedule 服务内部完成。
type UserRequest struct {
	UserID int `json:"user_id"`
}

type WeekRequest struct {
	UserID int `json:"user_id"`
	Week   int `json:"week"`
}

type DeleteScheduleEventsRequest struct {
	UserID int                       `json:"user_id"`
	Events []UserDeleteScheduleEvent `json:"events"`
}

type UserDeleteScheduleEvent struct {
	ID                 int  `json:"id"`
	DeleteCourse       bool `json:"delete_course"`
	DeleteEmbeddedTask bool `json:"delete_embedded_task"`
}

type RecentCompletedRequest struct {
	UserID int `json:"user_id"`
	Index  int `json:"index"`
	Limit  int `json:"limit"`
}

type RevokeTaskItemRequest struct {
	UserID  int `json:"user_id"`
	EventID int `json:"event_id"`
}

type SmartPlanningRequest struct {
	UserID      int `json:"user_id"`
	TaskClassID int `json:"task_class_id"`
}

type SmartPlanningMultiRequest struct {
	UserID       int   `json:"user_id"`
	TaskClassIDs []int `json:"task_class_ids"`
}

// AgentScheduleWeekRequest 是 agent schedule provider 按周读取原始日程槽位的契约。
//
// 职责边界：
// 1. 只按 user_id + week 读取已经落库的 schedule/schedule_event 事实；
// 2. 不走前端周课表 DTO，避免丢失 embedded_task_id / can_be_embedded 等编排语义；
// 3. 该契约只服务 agent 内部状态加载，不改变现有 HTTP 周课表响应。
type AgentScheduleWeekRequest struct {
	UserID int `json:"user_id"`
	Week   int `json:"week"`
}

type AgentScheduleWeekResponse struct {
	Schedules []AgentScheduleSlot `json:"schedules"`
}

type AgentScheduleSlot struct {
	ID             int                    `json:"id"`
	EventID        int                    `json:"event_id"`
	UserID         int                    `json:"user_id"`
	Week           int                    `json:"week"`
	DayOfWeek      int                    `json:"day_of_week"`
	Section        int                    `json:"section"`
	EmbeddedTaskID *int                   `json:"embedded_task_id,omitempty"`
	Status         string                 `json:"status"`
	Event          *AgentScheduleEvent    `json:"event,omitempty"`
	EmbeddedTask   *AgentScheduleTaskItem `json:"embedded_task,omitempty"`
}

type AgentScheduleEvent struct {
	ID             int       `json:"id"`
	UserID         int       `json:"user_id"`
	Name           string    `json:"name"`
	Location       *string   `json:"location,omitempty"`
	Type           string    `json:"type"`
	RelID          *int      `json:"rel_id,omitempty"`
	TaskSourceType string    `json:"task_source_type,omitempty"`
	CanBeEmbedded  bool      `json:"can_be_embedded"`
	StartTime      time.Time `json:"start_time,omitempty"`
	EndTime        time.Time `json:"end_time,omitempty"`
}

type AgentScheduleTaskItem struct {
	ID           int                      `json:"id"`
	CategoryID   *int                     `json:"category_id,omitempty"`
	Order        *int                     `json:"order,omitempty"`
	Content      string                   `json:"content"`
	EmbeddedTime *AgentScheduleTargetTime `json:"embedded_time,omitempty"`
	Status       *int                     `json:"status,omitempty"`
}

type AgentScheduleTargetTime struct {
	Week        int `json:"week"`
	DayOfWeek   int `json:"day_of_week"`
	SectionFrom int `json:"section_from"`
	SectionTo   int `json:"section_to"`
}

// Slot 是跨进程表达日程原子节次的稳定契约。
type Slot struct {
	Week      int       `json:"week"`
	DayOfWeek int       `json:"day_of_week"`
	Section   int       `json:"section"`
	StartAt   time.Time `json:"start_at,omitempty"`
	EndAt     time.Time `json:"end_at,omitempty"`
}

type ScheduleEventFact struct {
	ID             int    `json:"id"`
	UserID         int    `json:"user_id"`
	Title          string `json:"title"`
	SourceType     string `json:"source_type"`
	RelID          int    `json:"rel_id"`
	IsDynamicTask  bool   `json:"is_dynamic_task"`
	IsCompleted    bool   `json:"is_completed"`
	Slots          []Slot `json:"slots"`
	TaskClassID    int    `json:"task_class_id"`
	TaskItemID     int    `json:"task_item_id"`
	CanBeShortened bool   `json:"can_be_shortened"`
}

type ScheduleWindowFacts struct {
	Events                 []ScheduleEventFact `json:"events"`
	OccupiedSlots          []Slot              `json:"occupied_slots"`
	FreeSlots              []Slot              `json:"free_slots"`
	NextDynamicTask        *ScheduleEventFact  `json:"next_dynamic_task,omitempty"`
	TargetAlreadyScheduled bool                `json:"target_already_scheduled"`
}

type ScheduleWindowRequest struct {
	UserID      int       `json:"user_id"`
	TargetType  string    `json:"target_type"`
	TargetID    int       `json:"target_id"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	Now         time.Time `json:"now"`
}

type FeedbackRequest struct {
	UserID         int    `json:"user_id"`
	FeedbackID     string `json:"feedback_id"`
	IdempotencyKey string `json:"idempotency_key"`
	TargetType     string `json:"target_type"`
	TargetID       int    `json:"target_id"`
}

type FeedbackFact struct {
	FeedbackID       string    `json:"feedback_id"`
	Text             string    `json:"text"`
	TargetKnown      bool      `json:"target_known"`
	TargetEventID    int       `json:"target_event_id"`
	TargetTaskItemID int       `json:"target_task_item_id"`
	TargetTitle      string    `json:"target_title"`
	SubmittedAt      time.Time `json:"submitted_at"`
}

type FeedbackResponse struct {
	Feedback FeedbackFact `json:"feedback"`
	Found    bool         `json:"found"`
}

type ApplyActiveScheduleRequest struct {
	PreviewID   string        `json:"preview_id"`
	ApplyID     string        `json:"apply_id"`
	UserID      int           `json:"user_id"`
	CandidateID string        `json:"candidate_id"`
	Changes     []ApplyChange `json:"changes"`
	RequestedAt time.Time     `json:"requested_at"`
	TraceID     string        `json:"trace_id"`
}

type ApplyChange struct {
	ChangeID         string            `json:"change_id"`
	ChangeType       string            `json:"change_type"`
	TargetType       string            `json:"target_type"`
	TargetID         int               `json:"target_id"`
	ToSlot           *SlotSpan         `json:"to_slot,omitempty"`
	DurationSections int               `json:"duration_sections"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type SlotSpan struct {
	Start            Slot `json:"start"`
	End              Slot `json:"end"`
	DurationSections int  `json:"duration_sections"`
}

type ApplyActiveScheduleResult struct {
	ApplyID            string `json:"apply_id"`
	AppliedEventIDs    []int  `json:"applied_event_ids,omitempty"`
	AppliedScheduleIDs []int  `json:"applied_schedule_ids,omitempty"`
}
