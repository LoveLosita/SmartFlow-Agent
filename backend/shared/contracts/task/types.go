package task

import "time"

// AddTaskRequest 是 task 服务新增任务的跨进程契约。
//
// 职责边界：
// 1. 只承载 gateway 鉴权后补齐的 user_id 和前端任务字段；
// 2. 不承载 HTTP token、幂等键或缓存语义；
// 3. 业务校验仍由 task 服务内部完成。
type AddTaskRequest struct {
	UserID             int        `json:"user_id"`
	Title              string     `json:"title"`
	PriorityGroup      int        `json:"priority_group"`
	EstimatedSections  int        `json:"estimated_sections"`
	DeadlineAt         *time.Time `json:"deadline_at"`
	UrgencyThresholdAt *time.Time `json:"urgency_threshold_at"`
}

// AddTaskResponse 是 task 服务新增任务后的稳定响应契约。
//
// 职责边界：
// 1. 只承载调用方需要的任务基础快照，不暴露 DAO / GORM 模型；
// 2. CP4 agent 快捷任务只消费 ID，但保留完整字段以匹配既有 HTTP 响应语义；
// 3. UrgencyThresholdAt 暂不回显，新增链路只要求写入语义不丢。
type AddTaskResponse struct {
	ID                int        `json:"id"`
	Title             string     `json:"title"`
	PriorityGroup     int        `json:"priority_group"`
	EstimatedSections int        `json:"estimated_sections"`
	DeadlineAt        *time.Time `json:"deadline_at"`
	Status            string     `json:"status"`
	CreatedAt         time.Time  `json:"created_at"`
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

// TaskListItem 是 task 列表读取返回给跨进程调用方的轻量任务视图。
//
// 职责边界：
// 1. 字段形状保持 `/api/v1/task/get` 既有响应，便于 gateway 继续 JSON 透传；
// 2. agent 快捷查询只基于这些字段做本地过滤、排序和文案渲染；
// 3. Deadline / UrgencyThresholdAt 仍沿用历史字符串格式，避免 CP4 改动前端响应契约。
type TaskListItem struct {
	ID                 int    `json:"id"`
	UserID             int    `json:"user_id"`
	Title              string `json:"title"`
	PriorityGroup      int    `json:"priority_group"`
	EstimatedSections  int    `json:"estimated_sections"`
	Status             string `json:"status"`
	Deadline           string `json:"deadline"`
	IsCompleted        bool   `json:"is_completed"`
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty"`
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
