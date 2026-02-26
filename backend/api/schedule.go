package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
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
		respond.DealWithError(c, err)
		return
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
		respond.DealWithError(c, err)
		return
	}
	//4.返回日程安排数据给前端
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, weeklySchedules))
}

func (s *ScheduleAPI) DeleteScheduleEvent(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID
	userID := c.GetInt("user_id")
	// 2. 从请求体中获取要删除的日程事件信息
	var req []model.UserDeleteScheduleEvent
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//3.调用服务层方法删除指定的日程事件
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	err := s.scheduleService.DeleteScheduleEvent(ctx, req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//4.返回删除成功的响应给前端
	c.JSON(http.StatusOK, respond.Ok)
}

func (s *ScheduleAPI) GetUserRecentCompletedSchedules(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID以及其他查询参数（如 index 和 limit）
	userID := c.GetInt("user_id")
	index := c.Query("index")
	limit := c.Query("limit")
	intIndex, err := strconv.Atoi(index)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	intLimit, err := strconv.Atoi(limit)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//2.调用服务层方法获取用户最近完成的日程事件
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	completedSchedules, err := s.scheduleService.GetUserRecentCompletedSchedules(ctx, userID, intIndex, intLimit)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//3.返回数据给前端
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, completedSchedules))
}

func (s *ScheduleAPI) GetUserOngoingSchedule(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID
	userID := c.GetInt("user_id")
	//2.调用服务层方法获取用户正在进行的日程事件
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	ongoingSchedule, err := s.scheduleService.GetUserOngoingSchedule(ctx, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//3.返回数据给前端
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, ongoingSchedule))
}

func (s *ScheduleAPI) UserRevocateTaskItemFromSchedule(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID
	userID := c.GetInt("user_id")
	// 2. 获取要撤销的任务块ID
	eventID := c.Query("event_id")
	intEventID, err := strconv.Atoi(eventID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//3.调用服务层方法撤销任务块的安排
	/*ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源*/
	err = s.scheduleService.RevocateUserTaskClassItem(context.Background(), userID, intEventID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//4.返回撤销成功的响应给前端
	c.JSON(http.StatusOK, respond.Ok)
}

func (s *ScheduleAPI) SmartPlanning(c *gin.Context) {
	// 1. 从请求上下文中获取用户ID
	userID := c.GetInt("user_id")
	// 2. 从请求体中获取智能规划的参数
	taskClassID := c.Query("task_class_id")
	intTaskClassID, err := strconv.Atoi(taskClassID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//3.调用服务层方法进行智能规划
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	res, err := s.scheduleService.SmartPlanning(ctx, userID, intTaskClassID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//4.返回智能规划成功的响应给前端
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, res))
}
