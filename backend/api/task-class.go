package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type TaskClassHandler struct {
	svc *service.TaskClassService
}

// NewTaskClassHandler：组装 Handler 的“工厂”
func NewTaskClassHandler(svc *service.TaskClassService) *TaskClassHandler {
	return &TaskClassHandler{
		svc: svc, // 把传进来的 Service 揣进口袋里
	}
}

const (
	create = 0
	update = 1
)

func (api *TaskClassHandler) UserAddTaskClass(c *gin.Context) {
	var req model.UserAddTaskClassRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	userIDInterface := c.GetInt("user_id")
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	err = api.svc.AddOrUpdateTaskClass(ctx, &req, userIDInterface, create, 0)
	if err != nil {
		if errors.Is(err, respond.WrongParamType) {
			c.JSON(http.StatusBadRequest, respond.WrongParamType)
			return
		}
		c.JSON(http.StatusInternalServerError, respond.InternalError(err))
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (api *TaskClassHandler) UserGetTaskClassInfos(c *gin.Context) {
	userIDInterface := c.GetInt("user_id")
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	resp, err := api.svc.GetUserTaskClassInfos(ctx, userIDInterface)
	if err != nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(err))
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (api *TaskClassHandler) UserGetCompleteTaskClass(c *gin.Context) {
	taskClassID := c.Query("task_class_id")
	//将taskClassID转换为int
	intTaskClassID, err := strconv.Atoi(taskClassID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	if taskClassID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}
	userIDInterface := c.GetInt("user_id")
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	resp, err := api.svc.GetUserCompleteTaskClass(ctx, userIDInterface, intTaskClassID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, respond.UserTaskClassNotFound)
			return
		}
		c.JSON(http.StatusInternalServerError, respond.InternalError(err))
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (api *TaskClassHandler) UserUpdateTaskClass(c *gin.Context) {
	var req model.UserAddTaskClassRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	taskClassID := c.Query("task_class_id")
	//将taskClassID转换为int
	intTaskClassID, err := strconv.Atoi(taskClassID)
	userIDInterface := c.GetInt("user_id")
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	err = api.svc.AddOrUpdateTaskClass(ctx, &req, userIDInterface, update, intTaskClassID)
	if err != nil {
		if errors.Is(err, respond.WrongParamType) || errors.Is(err, respond.UserTaskClassForbidden) {
			c.JSON(http.StatusBadRequest, err)
			return
		}
		c.JSON(http.StatusInternalServerError, respond.InternalError(err))
	}
	c.JSON(http.StatusOK, respond.Ok)
}
