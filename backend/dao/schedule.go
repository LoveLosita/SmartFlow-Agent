package dao

import (
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

type ScheduleDAO struct {
	db *gorm.DB
}

// NewScheduleDAO 创建ScheduleDAO实例
func NewScheduleDAO(db *gorm.DB) *ScheduleDAO {
	return &ScheduleDAO{
		db: db,
	}
}

func (dao *ScheduleDAO) AddUserCourses(courses []model.Schedule) error {
	if err := dao.db.Create(&courses).Error; err != nil {
		return err
	}
	return nil
}
