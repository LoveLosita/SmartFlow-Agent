package model

import "time"

type Task struct {
	ID          int        `gorm:"primaryKey;autoIncrement"`
	UserID      int        `gorm:"column:user_id;index"`
	Title       string     `gorm:"type:varchar(255)"`
	Priority    int        `gorm:"not null"`
	IsCompleted bool       `gorm:"column:is_completed;default:false"`
	DeadlineAt  *time.Time `gorm:"column:deadline_at"`
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

type GetUserTaskResp struct {
	ID            int    `json:"id"`
	UserID        int    `json:"user_id"`
	Title         string `json:"title"`
	PriorityGroup int    `json:"priority_group"`
	Status        string `json:"status"`
	Deadline      string `json:"deadline"`
	IsCompleted   bool   `json:"is_completed"`
}
