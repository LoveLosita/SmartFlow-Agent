package sv

import (
	"context"
	"strings"

	"github.com/LoveLosita/smartflow/backend/conv"
	rootdao "github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	coursedao "github.com/LoveLosita/smartflow/backend/services/course/dao"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

type CourseService struct {
	// 伸出手：准备接住 DAO
	courseDAO                  *coursedao.CourseDAO
	scheduleDAO                *rootdao.ScheduleDAO
	courseImageResponsesClient *llmservice.ArkResponsesClient
	courseImageConfig          CourseImageParseConfig
	courseImageModel           string
}

// NewCourseService 创建 CourseService 实例
func NewCourseService(
	courseDAO *coursedao.CourseDAO,
	scheduleDAO *rootdao.ScheduleDAO,
	courseImageResponsesClient *llmservice.ArkResponsesClient,
	courseImageConfig CourseImageParseConfig,
	courseImageModel string,
) *CourseService {
	return &CourseService{
		courseDAO:                  courseDAO,
		scheduleDAO:                scheduleDAO,
		courseImageResponsesClient: courseImageResponsesClient,
		courseImageConfig:          courseImageConfig,
		courseImageModel:           strings.TrimSpace(courseImageModel),
	}
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// 兼容常见 MySQL / PostgreSQL / SQLite 的报错关键字
	// 也可以进一步精确到你的索引名 idx_user_slot_atomic
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "duplicate entry") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "unique violation") ||
		strings.Contains(msg, "duplicate key") {
		return true
	}
	return false
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
func (ss *CourseService) AddUserCourses(ctx context.Context, req model.UserImportCoursesRequest, userID int) ([]model.ScheduleConflictDetail, error) {
	//1.先校验参数是否正确
	for _, course := range req.Courses {
		result := CheckSingleCourse(course)
		if !result {
			return nil, respond.WrongCourseInfo
		}
	}
	//2.将前端传来的课程信息转换为 Schedule 和 ScheduleEvent 切片
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
				st, ed, err := conv.RelativeTimeToRealTime(week, arrangement.DayOfWeek, arrangement.StartSection, arrangement.EndSection)
				if err != nil {
					return nil, err
				}
				scheduleEvent := model.ScheduleEvent{
					UserID:        userID,
					Name:          course.CourseName,
					Location:      &location,
					Type:          "course",
					RelID:         nil,
					CanBeEmbedded: course.IsAllowTasks,
					StartTime:     st,
					EndTime:       ed,
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
	//3.先检测是否重复插入了课程（同一周、同一天、同一节已有课程）
	exists, err := ss.scheduleDAO.CheckScheduleConflict(ctx, finalSchedules)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, respond.InsertCourseTwice
	}
	//4.再检查是否和某些非课程的日程冲突（同一周、同一天、同一节已有非课程日程），并给出具体的冲突信息
	conflicts, err := ss.scheduleDAO.GetNonCourseScheduleConflicts(ctx, finalSchedules)
	if err != nil {
		return nil, err
	}
	if len(conflicts) > 0 {
		ret := conv.SchedulesToScheduleConflictDetail(conflicts)
		return ret, respond.ScheduleConflict
	}
	//5.事务：插入两个表要么都成功，要么都回滚
	err = ss.courseDAO.Transaction(func(txDAO *coursedao.CourseDAO) error {
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
	if err != nil {
		if isUniqueViolation(err) {
			return nil, respond.InsertCourseTwice
		}
		return nil, err
	}
	return nil, nil
}
