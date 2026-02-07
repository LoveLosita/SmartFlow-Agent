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
	var finalSchedules []model.Schedule
	var finalScheduleEvents []model.ScheduleEvent
	var pos []int
	for _, course := range req.Courses {
		// 避免取 range 迭代变量字段地址导致指针复用问题
		location := course.Location
		for _, arrangement := range course.Arrangements {
			weekType := arrangement.WeekType
			for week := arrangement.StartWeek; week <= arrangement.EndWeek; week++ {
				if weekType == "odd" && week%2 == 0 {
					continue
				}
				if weekType == "even" && week%2 != 0 {
					continue
				}
				//2.转换为 Schedule_event 切片
				scheduleEvent := model.ScheduleEvent{
					UserID:        userID,
					Name:          course.CourseName,
					Location:      &location,
					Type:          "course",
					RelID:         nil,
					CanBeEmbedded: course.IsAllowTasks,
				}
				finalScheduleEvents = append(finalScheduleEvents, scheduleEvent)
				//3.转换为 Schedule 切片
				for section := arrangement.StartSection; section <= arrangement.EndSection; section++ {
					schedule := model.Schedule{
						Week:      week,
						DayOfWeek: arrangement.DayOfWeek,
						Section:   section,
						Status:    "normal",
						UserID:    userID,
						EventID:   0,
					}
					finalSchedules = append(finalSchedules, schedule)
					pos = append(pos, len(finalScheduleEvents)-1)
				}
			}
		}
	}
	//TODO 冲突处理、重复检测...预计0.2.0版本之前完成
	//4.事务：插入两个表要么都成功，要么都回滚
	return ss.dao.Transaction(func(txDAO *dao.CourseDAO) error {
		ids, err := txDAO.AddUserCoursesIntoScheduleEvents(ctx, finalScheduleEvents)
		if err != nil {
			return err
		}
		// 将生成的 ScheduleEvent ID 赋值给对应的 Schedule 的 EventID 字段
		for i := range finalSchedules {
			finalSchedules[i].EventID = ids[pos[i]]
		}
		if err := txDAO.AddUserCoursesIntoSchedule(ctx, finalSchedules); err != nil {
			return err
		}
		return nil
	})
}
