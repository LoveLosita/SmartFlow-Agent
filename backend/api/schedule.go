package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
)

type ScheduleHandler struct {
	// 伸出手：准备接住 Service
	service *service.ScheduleService
}

// NewScheduleHandler 创建 ScheduleHandler 实例
func NewScheduleHandler(service *service.ScheduleService) *ScheduleHandler {
	return &ScheduleHandler{
		service: service,
	}
}

func (sa *ScheduleHandler) CheckUserCourse(c *gin.Context) {
	//1.从请求中获取课程信息
	var req model.UserCheckCourseRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//2.调用 service 层的 CheckSingleCourse 方法进行校验
	result := service.CheckSingleCourse(req)
	//3.根据校验结果返回响应
	if result {
		c.JSON(http.StatusOK, respond.Ok)
	} else {
		c.JSON(http.StatusBadRequest, respond.WrongCourseInfo)
	}
}

func (sa *ScheduleHandler) AddUserCourses(c *gin.Context) {
	//1.从请求中获取课程信息
	var req model.UserImportCoursesRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	//2.从上下文获取用户ID
	userIDInterface := c.GetInt("user_id")
	//3.调用 service 层的 AddUserCourses 方法添加课程
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	err = sa.service.AddUserCourses(ctx, req, userIDInterface)
	if err != nil {
		switch {
		case errors.Is(err, respond.WrongParamType), errors.Is(err, respond.WrongCourseInfo):
			c.JSON(http.StatusBadRequest, err)
		default:
			c.JSON(http.StatusInternalServerError, respond.InternalError(err))
		}
		return
	}
	//4.返回成功响应
	c.JSON(http.StatusOK, respond.Ok)
}
