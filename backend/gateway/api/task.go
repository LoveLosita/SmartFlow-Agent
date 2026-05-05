package api

import (
	"context"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	taskcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/task"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

const taskRequestTimeout = 6 * time.Second

type TaskHandler struct {
	client ports.TaskCommandClient
}

// NewTaskHandler 创建 task HTTP 门面。
func NewTaskHandler(client ports.TaskCommandClient) *TaskHandler {
	return &TaskHandler{client: client}
}

func (th *TaskHandler) AddTask(c *gin.Context) {
	var req taskcontracts.AddTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.AddTask(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (th *TaskHandler) GetUserTasks(c *gin.Context) {
	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.GetUserTasks(ctx, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// BatchTaskStatus 批量查询当前用户任务的实时完成状态。
//
// 职责边界：
// 1. 负责解析 ids 与读取鉴权上下文中的 user_id；
// 2. 负责调用 task 服务，不直接读取任务缓存或数据库；
// 3. 不修改任务、不触发幂等中间件、不反写 NewAgent timeline 历史 payload。
func (th *TaskHandler) BatchTaskStatus(c *gin.Context) {
	var req taskcontracts.BatchTaskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.BatchTaskStatus(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// CompleteTask 标记任务为已完成。
func (th *TaskHandler) CompleteTask(c *gin.Context) {
	var req taskcontracts.CompleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.CompleteTask(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// UndoCompleteTask 取消任务“已完成”勾选。
func (th *TaskHandler) UndoCompleteTask(c *gin.Context) {
	var req taskcontracts.UndoCompleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.UndoCompleteTask(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// UpdateTask 更新任务属性（部分更新）。
func (th *TaskHandler) UpdateTask(c *gin.Context) {
	var req taskcontracts.UpdateTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.UpdateTask(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// DeleteTask 永久删除指定任务。
func (th *TaskHandler) DeleteTask(c *gin.Context) {
	var req taskcontracts.DeleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskRequestTimeout)
	defer cancel()
	resp, err := th.client.DeleteTask(ctx, req)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}
