package model

import "time"

// TaskQueryParams 描述快捷任务查询路径传给业务层的内部查询参数。
//
// 职责边界：
// 1. 这里只承载“查询条件”本身，不负责 args 解析、默认值填充和错误提示；
// 2. 所有字段均为轻量筛选语义，便于 quick_task 节点和 service 层直接复用；
// 3. 不承担 LLM 工具协议，因为 query_tasks 工具链已下线。
type TaskQueryParams struct {
	Quadrant         *int
	SortBy           string // deadline | priority | id
	Order            string // asc | desc
	Limit            int
	IncludeCompleted bool
	Keyword          string
	DeadlineBefore   *time.Time
	DeadlineAfter    *time.Time
}

// TaskQueryResult 描述快捷任务查询返回给上层的轻量任务视图。
//
// 职责边界：
// 1. 这里只保留展示所需字段，避免把底层任务模型直接暴露给 newAgent 节点；
// 2. 结果既可用于 quick_task 节点文本回复，也可供 service 装配其他轻量输出；
// 3. 不负责序列化策略和文案渲染。
type TaskQueryResult struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	PriorityGroup int    `json:"priority_group"`
	PriorityLabel string `json:"priority_label"`
	IsCompleted   bool   `json:"is_completed"`
	DeadlineAt    string `json:"deadline_at,omitempty"`
}
