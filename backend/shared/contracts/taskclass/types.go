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

// UpsertTaskClassResponse 是 task-class 写入完成后的最小确认结果。
//
// 职责边界：
// 1. 只返回 agent / gateway 后续编排需要的稳定主键和创建语义；
// 2. 不回传完整任务类详情，避免写接口承担读取 DTO 责任；
// 3. TaskClassID <= 0 视为服务端异常，由调用方按失败处理。
type UpsertTaskClassResponse struct {
	TaskClassID int  `json:"task_class_id"`
	Created     bool `json:"created"`
}

type UserRequest struct {
	UserID int `json:"user_id"`
}

type GetTaskClassRequest struct {
	UserID      int `json:"user_id"`
	TaskClassID int `json:"task_class_id"`
}

// AgentTaskClassesRequest 是 agent schedule provider 读取完整任务类的契约。
//
// 职责边界：
// 1. TaskClassIDs 为空表示读取该用户全部任务类，兼容全量日程状态加载；
// 2. TaskClassIDs 非空表示按本轮排程范围读取，服务端仍按 user_id 做归属过滤；
// 3. 该契约只服务 agent 内部编排，不改变前端 GetTaskClass 的响应形状。
type AgentTaskClassesRequest struct {
	UserID       int   `json:"user_id"`
	TaskClassIDs []int `json:"task_class_ids,omitempty"`
}

type AgentTaskClassesResponse struct {
	TaskClasses []AgentTaskClass `json:"task_classes"`
}

// AgentTaskClass 是跨进程传给 agent provider 的任务类原始事实。
//
// 说明：
// 1. 日期用 YYYY-MM-DD 字符串传输，避免跨服务 JSON 时区漂移；
// 2. Items 保留 status / embedded_time，确保 LoadScheduleState 的 pending/existing 口径不变；
// 3. 不携带 DAO / GORM 元信息，调用侧只还原内存模型。
type AgentTaskClass struct {
	ID                 int                  `json:"id"`
	UserID             int                  `json:"user_id"`
	Name               string               `json:"name"`
	Mode               string               `json:"mode"`
	StartDate          string               `json:"start_date"`
	EndDate            string               `json:"end_date"`
	SubjectType        string               `json:"subject_type,omitempty"`
	DifficultyLevel    string               `json:"difficulty_level,omitempty"`
	CognitiveIntensity string               `json:"cognitive_intensity,omitempty"`
	TotalSlots         int                  `json:"total_slots"`
	AllowFillerCourse  bool                 `json:"allow_filler_course"`
	Strategy           string               `json:"strategy"`
	ExcludedSlots      []int                `json:"excluded_slots,omitempty"`
	ExcludedDaysOfWeek []int                `json:"excluded_days_of_week,omitempty"`
	Items              []AgentTaskClassItem `json:"items,omitempty"`
}

type AgentTaskClassItem struct {
	ID           int         `json:"id"`
	CategoryID   *int        `json:"category_id,omitempty"`
	Order        *int        `json:"order,omitempty"`
	Content      string      `json:"content"`
	EmbeddedTime *TargetTime `json:"embedded_time,omitempty"`
	Status       *int        `json:"status,omitempty"`
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
