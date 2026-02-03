package service

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

type ScheduleService struct {
	// 伸出手：准备接住 DAO
	dao *dao.ScheduleDAO
}

// NewScheduleService 创建 ScheduleService 实例
func NewScheduleService(dao *dao.ScheduleDAO) *ScheduleService {
	return &ScheduleService{
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
func (ss *ScheduleService) AddUserCourses(req model.UserImportCoursesRequest, userID int) error {
	//1.先校验参数是否正确
	for _, course := range req.Courses {
		result := CheckSingleCourse(course)
		if !result {
			return respond.WrongCourseInfo
		}
	}
	//2.转换为 Schedule 切片
	for _, course := range req.Courses {
		var schedules []model.Schedule
		for _, arrangement := range course.Arrangements {
			for week := arrangement.StartWeek; week <= arrangement.EndWeek; week++ {
				sections := fmt.Sprintf("%d-%d", arrangement.StartSection, arrangement.EndSection)
				schedule := model.Schedule{
					Type:          "course",
					Week:          week,
					DayOfWeek:     arrangement.DayOfWeek,
					Sections:      sections,
					Status:        "normal",
					UserID:        userID,
					CanBeEmbedded: course.IsAllowTasks,
				}
				schedules = append(schedules, schedule)
			}
		}
		//3.调用 DAO 方法添加课程
		err := ss.dao.AddUserCourses(schedules)
		if err != nil {
			return err
		}
	}

	//4.返回结果
	return nil
}
