package dao

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

type CourseDAO struct {
	db *gorm.DB
}

// NewCourseDAO 创建ScheduleDAO实例
func NewCourseDAO(db *gorm.DB) *CourseDAO {
	return &CourseDAO{
		db: db,
	}
}

func (r *CourseDAO) WithTx(tx *gorm.DB) *CourseDAO {
	return &CourseDAO{db: tx}
}

func (dao *CourseDAO) AddUserCoursesIntoSchedule(ctx context.Context, courses []model.Schedule) error {
	if err := dao.db.WithContext(ctx).Create(&courses).Error; err != nil {
		return err
	}
	return nil
}

func (dao *CourseDAO) AddUserCoursesIntoScheduleEvents(ctx context.Context, events []model.ScheduleEvent) ([]int, error) {
	if err := dao.db.WithContext(ctx).Create(&events).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(events))
	for i := range events {
		ids = append(ids, int(events[i].ID))
	}
	return ids, nil
}

// Transaction 在同一个数据库事务中执行传入的函数，供 service 层复用（自动提交/回滚）
// 规则：fn 返回 nil \-\> 提交；fn 返回 error 或发生 panic \-\> 回滚
// 说明：gorm\.\(\\\*DB\)\.Transaction 会在 fn 返回 error 时回滚，并在发生 panic 时自动回滚后继续向上抛出 panic
func (dao *CourseDAO) Transaction(fn func(txDAO *CourseDAO) error) error {
	return dao.db.Transaction(func(tx *gorm.DB) error {
		return fn(NewCourseDAO(tx))
	})
}
