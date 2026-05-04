package task

import "time"

// AddTaskRequest 是 task 服务新增任务的跨进程契约。
//
// 职责边界：
// 1. 只承载 gateway 鉴权后补齐的 user_id 和前端任务字段；
// 2. 不承载 HTTP token、幂等键或缓存语义；
// 3. 业务校验仍由 task 服务内部完成。
type AddTaskRequest struct {
	UserID            int        `json:"user_id"`
	Title             string     `json:"title"`
	PriorityGroup     int        `json:"priority_group"`
	EstimatedSections int        `json:"estimated_sections"`
	DeadlineAt        *time.Time `json:"deadline_at"`
}

type UserRequest struct {
	UserID int `json:"user_id"`
}

type CompleteTaskRequest struct {
	UserID int `json:"user_id"`
	TaskID int `json:"task_id"`
}

type UndoCompleteTaskRequest struct {
	UserID int `json:"user_id"`
	TaskID int `json:"task_id"`
}

type DeleteTaskRequest struct {
	UserID int `json:"user_id"`
	TaskID int `json:"task_id"`
}

type UpdateTaskRequest struct {
	UserID             int        `json:"user_id"`
	TaskID             int        `json:"task_id"`
	Title              *string    `json:"title"`
	PriorityGroup      *int       `json:"priority_group"`
	DeadlineAt         *time.Time `json:"deadline_at"`
	UrgencyThresholdAt *time.Time `json:"urgency_threshold_at"`
}

type BatchTaskStatusRequest struct {
	UserID int   `json:"user_id"`
	IDs    []int `json:"ids"`
}

type TaskFactRequest struct {
	UserID int       `json:"user_id"`
	TaskID int       `json:"task_id"`
	Now    time.Time `json:"now"`
}

// TaskFact 是 active-scheduler 读取 task_pool 事实时需要的最小快照。
type TaskFact struct {
	ID                 int        `json:"id"`
	UserID             int        `json:"user_id"`
	Title              string     `json:"title"`
	Priority           int        `json:"priority"`
	IsCompleted        bool       `json:"is_completed"`
	DeadlineAt         *time.Time `json:"deadline_at,omitempty"`
	UrgencyThresholdAt *time.Time `json:"urgency_threshold_at,omitempty"`
	EstimatedSections  int        `json:"estimated_sections"`
}

type TaskFactResponse struct {
	Task  TaskFact `json:"task"`
	Found bool     `json:"found"`
}
