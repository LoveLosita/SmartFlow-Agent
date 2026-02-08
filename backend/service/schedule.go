package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"gorm.io/gorm"
)

type ScheduleService struct {
	scheduleDAO *dao.ScheduleDAO
	userDAO     *dao.UserDAO
}

func NewScheduleService(scheduleDAO *dao.ScheduleDAO, userDAO *dao.UserDAO) *ScheduleService {
	return &ScheduleService{
		scheduleDAO: scheduleDAO,
		userDAO:     userDAO,
	}
}

func (ss *ScheduleService) GetUserTodaySchedule(ctx context.Context, userID int) ([]model.UserTodaySchedule, error) {
	//1.先检查用户id是否存在
	_, err := ss.userDAO.GetUserByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongUserID
		}
		return nil, err
	}
	//2.获取当前日期
	curTime := time.Now().Format("2006-01-02")
	/*curTime := "2026-03-02" //测试数据*/
	week, dayOfWeek, err := conv.RealDateToRelativeDate(curTime)
	if err != nil {
		return nil, err
	}
	fmt.Println(week, dayOfWeek)
	//3.查询用户当天的日程安排
	schedules, err := ss.scheduleDAO.GetUserTodaySchedule(ctx, userID, week, dayOfWeek) //测试数据
	if err != nil {
		return nil, err
	}
	//4.转换为前端需要的格式
	todaySchedules := conv.SchedulesToUserTodaySchedule(schedules)
	return todaySchedules, nil
}

func (ss *ScheduleService) GetUserWeeklySchedule(ctx context.Context, userID, week int) ([]model.UserWeekSchedule, error) {
	//1.先检查 week 参数是否合法
	if week < 0 || week > 25 {
		return nil, respond.WeekOutOfRange
	}
	//2.先检查用户id是否存在
	_, err := ss.userDAO.GetUserByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongUserID
		}
		return nil, err
	}
	//2.查询用户每周的日程安排
	//如果没有传入 week 参数，则默认查询当前周的日程安排
	if week == 0 {
		curTime := time.Now().Format("2006-01-02")
		var err error
		week, _, err = conv.RealDateToRelativeDate(curTime)
		if err != nil {
			return nil, err
		}
	}
	schedules, err := ss.scheduleDAO.GetUserWeeklySchedule(ctx, userID, week)
	if err != nil {
		return nil, err
	}
	//3.转换为前端需要的格式
	weeklySchedules := conv.SchedulesToUserWeeklySchedule(schedules)
	return weeklySchedules, nil
}
