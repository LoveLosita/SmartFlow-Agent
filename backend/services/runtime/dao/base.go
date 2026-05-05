package dao

import (
	"context"

	"gorm.io/gorm"
)

// RepoManager 聚合所有 DAO，供服务层做跨仓储事务编排。
type RepoManager struct {
	db                    *gorm.DB
	Schedule              *ScheduleDAO
	Task                  *TaskDAO
	Course                *CourseDAO
	TaskClass             *TaskClassDAO
	Agent                 *AgentDAO
	ActiveSchedule        *ActiveScheduleDAO
	ActiveScheduleSession *ActiveScheduleSessionDAO
}

func NewManager(db *gorm.DB) *RepoManager {
	return &RepoManager{
		db:                    db,
		Schedule:              NewScheduleDAO(db),
		Task:                  NewTaskDAO(db),
		Course:                NewCourseDAO(db),
		TaskClass:             NewTaskClassDAO(db),
		Agent:                 NewAgentDAO(db),
		ActiveSchedule:        NewActiveScheduleDAO(db),
		ActiveScheduleSession: NewActiveScheduleSessionDAO(db),
	}
}

// WithTx 基于外部事务句柄构造“同事务 RepoManager”。
//
// 职责边界：
// 1. 只做 DAO 依赖重绑定，不开启/提交/回滚事务；
// 2. 让服务层在一个 tx 内调用多个 DAO 方法；
// 3. 适用于 outbox 消费处理器这类“基础设施事务 + 业务事务合并”的场景。
func (m *RepoManager) WithTx(tx *gorm.DB) *RepoManager {
	return &RepoManager{
		db:                    tx,
		Schedule:              m.Schedule.WithTx(tx),
		Task:                  m.Task.WithTx(tx),
		TaskClass:             m.TaskClass.WithTx(tx),
		Course:                m.Course.WithTx(tx),
		Agent:                 m.Agent.WithTx(tx),
		ActiveSchedule:        m.ActiveSchedule.WithTx(tx),
		ActiveScheduleSession: m.ActiveScheduleSession.WithTx(tx),
	}
}

// Transaction 开启事务并把“同事务 RepoManager”传给回调。
//
// 使用约束：
// 1. 回调里应只使用 txM 下挂 DAO，避免混入事务外句柄；
// 2. 回调返回 error 会触发整体回滚；
// 3. 回调返回 nil 表示提交事务。
func (m *RepoManager) Transaction(ctx context.Context, fn func(txM *RepoManager) error) error {
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txM := m.WithTx(tx)
		return fn(txM)
	})
}
