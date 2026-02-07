package dao

import (
	"context"

	"gorm.io/gorm"
)

// RepoManager 囊括了所有的 Repo
type RepoManager struct {
	db        *gorm.DB
	Schedule  *ScheduleDAO
	Task      *TaskDAO
	Course    *CourseDAO
	TaskClass *TaskClassDAO
	User      *UserDAO
}

func NewManager(db *gorm.DB) *RepoManager {
	return &RepoManager{
		db:        db,
		Schedule:  NewScheduleDAO(db),
		Task:      NewTaskDAO(db),
		Course:    NewCourseDAO(db),
		TaskClass: NewTaskClassDAO(db),
		User:      NewUserDAO(db),
	}
}

// Transaction 核心函数：开启一个带事务的“新管理器”
func (m *RepoManager) Transaction(ctx context.Context, fn func(txM *RepoManager) error) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 💡 关键：创建一个新的 RepoManager，里面的 Repo 全部注入这个 tx 句柄
		txM := &RepoManager{
			db:        tx,
			Schedule:  m.Schedule.WithTx(tx),
			Task:      m.Task.WithTx(tx),
			TaskClass: m.TaskClass.WithTx(tx),
			Course:    m.Course.WithTx(tx),
			User:      m.User.WithTx(tx),
		}
		return fn(txM)
	})
}
