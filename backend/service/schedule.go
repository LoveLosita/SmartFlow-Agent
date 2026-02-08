package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
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

func (ss *ScheduleService) GetUserTodaySchedule(ctx context.Context, userID int) (interface{}, error) {
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
