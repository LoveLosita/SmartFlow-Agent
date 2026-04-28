package newagenttools

import "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"

const scheduleReadResultViewType = "schedule.read_result"

type scheduleReadTaskOnDay struct {
	Task      *schedule.ScheduleTask
	SlotStart int
	SlotEnd   int
}

type scheduleReadFreeRange struct {
	Day       int
	SlotStart int
	SlotEnd   int
}

type scheduleReadAvailableSlotsPayload struct {
	Tool            string                            `json:"tool"`
	Success         bool                              `json:"success"`
	Error           string                            `json:"error"`
	Count           int                               `json:"count"`
	StrictCount     int                               `json:"strict_count"`
	EmbeddedCount   int                               `json:"embedded_count"`
	FallbackUsed    bool                              `json:"fallback_used"`
	DayScope        string                            `json:"day_scope"`
	DayOfWeek       []int                             `json:"day_of_week"`
	WeekFilter      []int                             `json:"week_filter"`
	WeekFrom        int                               `json:"week_from"`
	WeekTo          int                               `json:"week_to"`
	Span            int                               `json:"span"`
	AllowEmbed      bool                              `json:"allow_embed"`
	ExcludeSections []int                             `json:"exclude_sections"`
	Slots           []scheduleReadAvailableSlotRecord `json:"slots"`
}

type scheduleReadAvailableSlotRecord struct {
	Day       int    `json:"day"`
	Week      int    `json:"week"`
	DayOfWeek int    `json:"day_of_week"`
	SlotStart int    `json:"slot_start"`
	SlotEnd   int    `json:"slot_end"`
	SlotType  string `json:"slot_type"`
}

type scheduleReadTargetTasksPayload struct {
	Tool       string                              `json:"tool"`
	Success    bool                                `json:"success"`
	Error      string                              `json:"error"`
	Count      int                                 `json:"count"`
	Status     string                              `json:"status"`
	DayScope   string                              `json:"day_scope"`
	DayOfWeek  []int                               `json:"day_of_week"`
	WeekFilter []int                               `json:"week_filter"`
	WeekFrom   int                                 `json:"week_from"`
	WeekTo     int                                 `json:"week_to"`
	Enqueue    bool                                `json:"enqueue"`
	Enqueued   int                                 `json:"enqueued"`
	Queue      *scheduleReadTargetTasksQueueRecord `json:"queue"`
	Items      []scheduleReadTargetTaskRecord      `json:"items"`
}

type scheduleReadTargetTasksQueueRecord struct {
	PendingCount   int `json:"pending_count"`
	CompletedCount int `json:"completed_count"`
	SkippedCount   int `json:"skipped_count"`
	CurrentTaskID  int `json:"current_task_id"`
	CurrentAttempt int `json:"current_attempt"`
}

type scheduleReadTargetTaskRecord struct {
	TaskID      int                              `json:"task_id"`
	Name        string                           `json:"name"`
	Category    string                           `json:"category"`
	Status      string                           `json:"status"`
	Duration    int                              `json:"duration"`
	TaskClassID int                              `json:"task_class_id"`
	Slots       []scheduleReadTargetTaskSlotInfo `json:"slots"`
}

type scheduleReadTargetTaskSlotInfo struct {
	Day       int `json:"day"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	SlotStart int `json:"slot_start"`
	SlotEnd   int `json:"slot_end"`
}

type scheduleReadQueueStatusPayload struct {
	Tool           string                         `json:"tool"`
	Success        bool                           `json:"success"`
	Error          string                         `json:"error"`
	PendingCount   int                            `json:"pending_count"`
	CompletedCount int                            `json:"completed_count"`
	SkippedCount   int                            `json:"skipped_count"`
	CurrentTaskID  int                            `json:"current_task_id"`
	CurrentAttempt int                            `json:"current_attempt"`
	LastError      string                         `json:"last_error"`
	NextTaskIDs    []int                          `json:"next_task_ids"`
	Current        *scheduleReadQueueTaskSnapshot `json:"current"`
}

type scheduleReadQueueTaskSnapshot struct {
	TaskID      int                              `json:"task_id"`
	Name        string                           `json:"name"`
	Category    string                           `json:"category"`
	Status      string                           `json:"status"`
	Duration    int                              `json:"duration"`
	TaskClassID int                              `json:"task_class_id"`
	Slots       []scheduleReadTargetTaskSlotInfo `json:"slots"`
}
