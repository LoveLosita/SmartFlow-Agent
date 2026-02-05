package dao

import (
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

func (dao *CourseDAO) AddUserCourses(courses []model.Schedule) error {
	if err := dao.db.Create(&courses).Error; err != nil {
		return err
	}
	return nil
}
