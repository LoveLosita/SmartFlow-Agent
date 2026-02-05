package service

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

type CourseService struct {
	// 伸出手：准备接住 DAO
	dao *dao.CourseDAO
}

// NewCourseService 创建 CourseService 实例
func NewCourseService(dao *dao.CourseDAO) *CourseService {
	return &CourseService{
		dao: dao,
	}
}

func CheckSingleCourse(req model.UserCheckCourseRequest) bool {
	for _, arrangement := range req.Arrangements {
		if arrangement.StartWeek > arrangement.EndWeek ||
			arrangement.DayOfWeek < 1 || arrangement.DayOfWeek > 7 ||
			arrangement.StartSection < 1 || arrangement.EndSection < arrangement.StartSection ||
			arrangement.EndSection > 12 || arrangement.StartWeek < 1 || arrangement.EndWeek > 24 {
			return false
		}
	}
	return true
}

// AddUserCourses 添加用户课程表
func (ss *CourseService) AddUserCourses(ctx context.Context, req model.UserImportCoursesRequest, userID int) error {
	//1.先校验参数是否正确
	for _, course := range req.Courses {
		result := CheckSingleCourse(course)
		if !result {
			return respond.WrongCourseInfo
		}
	}
	//2.转换为 Schedule 切片
	var finalSchedules []model.Schedule
	for _, course := range req.Courses {
		var schedules []model.Schedule
		for _, arrangement := range course.Arrangements {
			for week := arrangement.StartWeek; week <= arrangement.EndWeek; week++ {
				for section := arrangement.StartSection; section <= arrangement.EndSection; section++ {
					schedule := model.Schedule{
						Type:          "course",
						Week:          week,
						DayOfWeek:     arrangement.DayOfWeek,
						Section:       section,
						Status:        "normal",
						UserID:        userID,
						CanBeEmbedded: course.IsAllowTasks,
					}
					schedules = append(schedules, schedule)
				}
			}
		}
		finalSchedules = append(finalSchedules, schedules...)
	}
	//3.调用 DAO 方法添加课程
	err := ss.dao.AddUserCourses(finalSchedules)
	if err != nil {
		return err
	}
	//4.返回结果
	return nil
}
