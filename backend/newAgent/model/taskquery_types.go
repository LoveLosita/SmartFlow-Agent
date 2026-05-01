package model

import "time"

// TaskQueryRequest 是任务查询工具的请求参数。
type TaskQueryRequest struct {
	UserID           int
	Quadrant         *int
	SortBy           string
	Order            string
	Limit            int
	IncludeCompleted bool
	Keyword          string
	DeadlineBefore   *time.Time
	DeadlineAfter    *time.Time
}

// TaskQueryTaskRecord 是任务查询工具返回的单条任务记录。
type TaskQueryTaskRecord struct {
	ID                 int
	Title              string
	PriorityGroup      int
	EstimatedSections  int
	IsCompleted        bool
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
}
