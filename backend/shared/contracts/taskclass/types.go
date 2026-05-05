package taskclass

// TargetTime 表示任务块被安排到课表中的相对时间坐标。
type TargetTime struct {
	Week        int `json:"week"`
	DayOfWeek   int `json:"day_of_week"`
	SectionFrom int `json:"section_from"`
	SectionTo   int `json:"section_to"`
}

// UpsertTaskClassRequest 是 task-class 服务新增/更新任务类的跨进程契约。
//
// 职责边界：
// 1. UserID 由 gateway 鉴权后补齐，不信任前端传入值；
// 2. TaskClassID 仅 update 场景使用，add 场景保持 0；
// 3. 业务校验仍由 task-class 服务 sv 层统一执行。
type UpsertTaskClassRequest struct {
	UserID             int                         `json:"user_id"`
	TaskClassID        int                         `json:"task_class_id,omitempty"`
	Name               string                      `json:"name" binding:"required"`
	StartDate          string                      `json:"start_date" binding:"required"`
	EndDate            string                      `json:"end_date" binding:"required"`
	Mode               string                      `json:"mode" binding:"required,oneof=auto manual"`
	SubjectType        string                      `json:"subject_type,omitempty"`
	DifficultyLevel    string                      `json:"difficulty_level,omitempty"`
	CognitiveIntensity string                      `json:"cognitive_intensity,omitempty"`
	Config             UpsertTaskClassConfig       `json:"config" binding:"required"`
	Items              []UpsertTaskClassItemConfig `json:"items" binding:"required"`
}

type UpsertTaskClassConfig struct {
	TotalSlots         int    `json:"total_slots" binding:"required,min=1"`
	AllowFillerCourse  bool   `json:"allow_filler_course"`
	Strategy           string `json:"strategy" binding:"required,oneof=steady rapid"`
	ExcludedSlots      []int  `json:"excluded_slots"`
	ExcludedDaysOfWeek []int  `json:"excluded_days_of_week"`
}

type UpsertTaskClassItemConfig struct {
	ID           int         `json:"id,omitempty"`
	Order        int         `json:"order" binding:"required,min=1"`
	Content      string      `json:"content" binding:"required"`
	EmbeddedTime *TargetTime `json:"embedded_time"`
}

type UserRequest struct {
	UserID int `json:"user_id"`
}

type GetTaskClassRequest struct {
	UserID      int `json:"user_id"`
	TaskClassID int `json:"task_class_id"`
}

type InsertTaskClassItemIntoScheduleRequest struct {
	UserID             int `json:"user_id"`
	TaskItemID         int `json:"task_item_id"`
	Week               int `json:"week" binding:"required,min=1"`
	DayOfWeek          int `json:"day_of_week" binding:"required,min=1,max=7"`
	StartSection       int `json:"start_section" binding:"required,min=1"`
	EndSection         int `json:"end_section" binding:"required,min=1,gtefield=StartSection"`
	EmbedCourseEventID int `json:"embed_course_event_id"`
}

type DeleteTaskClassItemRequest struct {
	UserID     int `json:"user_id"`
	TaskItemID int `json:"task_item_id"`
}

type DeleteTaskClassRequest struct {
	UserID      int `json:"user_id"`
	TaskClassID int `json:"task_class_id"`
}

type ApplyBatchIntoScheduleRequest struct {
	UserID      int                   `json:"user_id"`
	TaskClassID int                   `json:"task_class_id" binding:"required"`
	Items       []SingleTaskClassItem `json:"items" binding:"required,dive,required"`
}

type SingleTaskClassItem struct {
	TaskItemID         int `json:"task_item_id" binding:"required"`
	Week               int `json:"week" binding:"required,min=1"`
	DayOfWeek          int `json:"day_of_week" binding:"required,min=1,max=7"`
	StartSection       int `json:"start_section" binding:"required,min=1"`
	EndSection         int `json:"end_section" binding:"required,min=1,gtefield=StartSection"`
	EmbedCourseEventID int `json:"embed_course_event_id"`
}
