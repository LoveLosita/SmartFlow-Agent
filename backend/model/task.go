package model

import "time"

// Task 是任务表的领域模型。
//
// 职责边界：
// 1. 负责映射 tasks 表字段；
// 2. 不负责接口入参校验和业务规则判断；
// 3. 不负责"自动平移"执行（自动平移由 Service + Outbox 事件链路负责）。
type Task struct {
	// 1. 主键。
	ID int `gorm:"primaryKey;autoIncrement"`
	// 2. 归属用户 ID。
	// 2.1 单列索引用于常规按用户查任务；
	// 2.2 同时参与"懒触发平移"复合索引的最左前缀。
	UserID int `gorm:"column:user_id;index;index:idx_user_done_threshold_priority,priority:1"`
	// 3. 任务标题。
	Title string `gorm:"type:varchar(255)"`
	// 4. 四象限优先级：
	// 4.1 1=重要且紧急；
	// 4.2 2=重要不紧急；
	// 4.3 3=简单不重要；
	// 4.4 4=不简单不重要。
	//
	// 说明：该字段参与"懒触发平移"复合索引。
	Priority int `gorm:"not null;index:idx_user_done_threshold_priority,priority:4"`
	// 5. 完成状态。
	//
	// 说明：已完成任务不参与自动平移；该字段参与复合索引。
	IsCompleted bool `gorm:"column:is_completed;default:false;index:idx_user_done_threshold_priority,priority:2"`
	// 6. 任务业务截止时间。
	DeadlineAt *time.Time `gorm:"column:deadline_at"`
	// 7. 紧急分界时间（自动平移阈值）。
	//
	// 规则：
	// 7.1 到达该时间后，任务可从"不紧急象限"自动平移到"紧急象限"；
	// 7.2 该值由上游（例如 LLM 规划）给出，不在模型层做推断；
	// 7.3 为空表示该任务不参与自动平移；
	// 7.4 该字段参与"懒触发平移"复合索引。
	UrgencyThresholdAt *time.Time `gorm:"column:urgency_threshold_at;index:idx_user_done_threshold_priority,priority:3"`
	// 8. 任务预计占用节数。
	//
	// 说明：
	// 8.1 主动调度只消费该字段，不在调度阶段重新推断任务复杂度；
	// 8.2 MVP 约定有效范围为 1~4，模型层仅提供默认值，具体截断由主动调度上下文构造负责；
	// 8.3 默认 1 节，兼容历史任务与未显式填写的任务。
	EstimatedSections int `gorm:"column:estimated_sections;not null;default:1"`
}

type UserAddTaskResponse struct {
	ID            int        `json:"id"`
	Title         string     `json:"title"`
	PriorityGroup int        `json:"priority_group"`
	DeadlineAt    *time.Time `json:"deadline_at"`
	Status        string     `json:"status"`
	CreatedAt     time.Time  `json:"created_at"`
}

type UserAddTaskRequest struct {
	Title         string     `json:"title"`
	PriorityGroup int        `json:"priority_group"`
	DeadlineAt    *time.Time `json:"deadline_at"`
}

// UserCompleteTaskRequest 是"标记任务完成"接口的请求体。
//
// 职责边界：
// 1. 只承载目标任务 ID；
// 2. 不承载 user_id（user_id 一律由鉴权中间件注入，避免越权）。
type UserCompleteTaskRequest struct {
	TaskID int `json:"task_id"`
}

// UserCompleteTaskResponse 是"标记任务完成"接口的响应体。
//
// 字段语义：
//  1. TaskID：本次操作的目标任务；
//  2. IsCompleted：操作后的完成状态（成功时恒为 true）；
//  3. AlreadyCompleted：
//     3.1 true：任务原本就已完成，本次请求命中幂等语义；
//     3.2 false：任务由未完成切换为完成；
//  4. Status：给前端的简短状态文案。
type UserCompleteTaskResponse struct {
	TaskID           int    `json:"task_id"`
	IsCompleted      bool   `json:"is_completed"`
	AlreadyCompleted bool   `json:"already_completed"`
	Status           string `json:"status"`
}

// UserUndoCompleteTaskRequest 是"取消任务已完成勾选"接口请求体。
//
// 职责边界：
// 1. 只承载目标 task_id；
// 2. 不承载 user_id（user_id 始终由鉴权中间件注入，防止越权操作）。
type UserUndoCompleteTaskRequest struct {
	TaskID int `json:"task_id"`
}

// UserUndoCompleteTaskResponse 是"取消任务已完成勾选"接口响应体。
//
// 字段语义：
// 1. TaskID：本次操作目标任务；
// 2. IsCompleted：操作后完成状态（成功时恒为 false）；
// 3. Status：给前端的简短状态文案。
type UserUndoCompleteTaskResponse struct {
	TaskID      int    `json:"task_id"`
	IsCompleted bool   `json:"is_completed"`
	Status      string `json:"status"`
}

type GetUserTaskResp struct {
	ID                 int    `json:"id"`
	UserID             int    `json:"user_id"`
	Title              string `json:"title"`
	PriorityGroup      int    `json:"priority_group"`
	Status             string `json:"status"`
	Deadline           string `json:"deadline"`
	IsCompleted        bool   `json:"is_completed"`
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty"`
}

// BatchTaskStatusRequest 是任务批量状态查询请求体。
//
// 职责边界：
// 1. 只承载前端从历史卡片中提取的任务 ID 列表；
// 2. 不承载 user_id，用户身份必须来自鉴权上下文，避免越权查询；
// 3. 不表达任务是否必须存在，不存在或无权访问的任务由 Service 静默过滤。
type BatchTaskStatusRequest struct {
	IDs []int `json:"ids"`
}

// BatchTaskStatusItem 是单个任务当前完成状态快照。
//
// 说明：
// 1. 当前 Task 模型未维护 UpdatedAt 字段，因此这里只返回可用的 id/is_completed；
// 2. 该结构表示"当前状态"，不用于反写 NewAgent timeline 历史 payload。
type BatchTaskStatusItem struct {
	ID          int  `json:"id"`
	IsCompleted bool `json:"is_completed"`
}

// BatchTaskStatusResponse 是批量任务状态查询响应体。
//
// 职责边界：
// 1. items 只包含当前登录用户有权访问且仍存在的任务；
// 2. ids 为空、非法 ID 全部被过滤、或无匹配任务时，items 为空切片而不是业务错误。
type BatchTaskStatusResponse struct {
	Items []BatchTaskStatusItem `json:"items"`
}

// UserUpdateTaskRequest 是"更新任务属性"接口的请求体。
//
// 职责边界：
// 1. 指针字段表示"部分更新"语义：nil 表示不修改，非 nil 表示更新为指定值；
// 2. TaskID 为必填；
// 3. 不承载 user_id（由鉴权中间件注入，防止越权）。
type UserUpdateTaskRequest struct {
	TaskID             int        `json:"task_id"`
	Title              *string    `json:"title"`
	PriorityGroup      *int       `json:"priority_group"`
	DeadlineAt         *time.Time `json:"deadline_at"`
	UrgencyThresholdAt *time.Time `json:"urgency_threshold_at"`
}

// TaskUrgencyPromoteRequestedPayload 是"任务紧急性平移请求"事件载荷。
//
// 职责边界：
// 1. 只承载"哪个用户的哪些任务需要尝试平移"；
// 2. 不包含 outbox/kafka 协议字段（这些由基础设施层统一封装）；
// 3. TriggeredAt 只用于追踪触发时间，最终是否更新仍以消费时数据库条件为准。
type TaskUrgencyPromoteRequestedPayload struct {
	UserID      int       `json:"user_id"`
	TaskIDs     []int     `json:"task_ids"`
	TriggeredAt time.Time `json:"triggered_at"`
}
