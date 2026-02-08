package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
)

type ScheduleAPI struct {
	scheduleService *service.ScheduleService
}

func NewScheduleAPI(scheduleService *service.ScheduleService) *ScheduleAPI {
	return &ScheduleAPI{
		scheduleService: scheduleService,
	}
}

func (s *ScheduleAPI) GetUserTodaySchedule(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID
	userID := c.GetInt("user_id")
	//2.调用服务层方法获取用户当天的日程安排
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	todaySchedules, err := s.scheduleService.GetUserTodaySchedule(ctx, userID)
	if err != nil {
		switch {
		case errors.Is(err, respond.WrongUserID):
			c.JSON(http.StatusBadRequest, respond.WrongUserID)
			return
		default:
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
			return
		}
	}
	//3.返回日程安排数据给前端
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, todaySchedules))
}

func (s *ScheduleAPI) GetUserWeeklySchedule(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID
	userID := c.GetInt("user_id")
	// 2. 从查询参数中获取 week 参数
	week, err := strconv.Atoi(c.Query("week"))
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//3.调用服务层方法获取用户当周的日程安排
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	weeklySchedules, err := s.scheduleService.GetUserWeeklySchedule(ctx, userID, week)
	if err != nil {
		switch {
		case errors.Is(err, respond.WrongUserID), errors.Is(err, respond.WeekOutOfRange):
			c.JSON(http.StatusBadRequest, err)
			return
		default:
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
			return
		}
	}
	//4.返回日程安排数据给前端
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, weeklySchedules))
}
