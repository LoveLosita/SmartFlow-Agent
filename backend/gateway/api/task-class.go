package api

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/gateway/shared/respond"
	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
)

const taskClassRequestTimeout = 6 * time.Second

type TaskClassHandler struct {
	client ports.TaskClassCommandClient
}

// NewTaskClassHandler 创建 task-class HTTP 门面。
func NewTaskClassHandler(client ports.TaskClassCommandClient) *TaskClassHandler {
	return &TaskClassHandler{client: client}
}

func (api *TaskClassHandler) UserAddTaskClass(c *gin.Context) {
	var req taskclasscontracts.UpsertTaskClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	if _, err := api.client.AddTaskClass(ctx, req); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (api *TaskClassHandler) UserGetTaskClassInfos(c *gin.Context) {
	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	resp, err := api.client.ListTaskClasses(ctx, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (api *TaskClassHandler) UserGetCompleteTaskClass(c *gin.Context) {
	taskClassID := c.Query("task_class_id")
	intTaskClassID, err := strconv.Atoi(taskClassID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	if taskClassID == "" {
		c.JSON(http.StatusBadRequest, respond.MissingParam)
		return
	}
	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	resp, err := api.client.GetTaskClass(ctx, taskclasscontracts.GetTaskClassRequest{
		UserID:      userID,
		TaskClassID: intTaskClassID,
	})
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (api *TaskClassHandler) UserUpdateTaskClass(c *gin.Context) {
	var req taskclasscontracts.UpsertTaskClassRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	taskClassID := c.Query("task_class_id")
	intTaskClassID, err := strconv.Atoi(taskClassID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")
	req.TaskClassID = intTaskClassID

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	if _, err := api.client.UpdateTaskClass(ctx, req); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (api *TaskClassHandler) UserAddTaskClassItemIntoSchedule(c *gin.Context) {
	var req taskclasscontracts.InsertTaskClassItemIntoScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	taskID := c.Query("task_item_id")
	intTaskID, err := strconv.Atoi(taskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")
	req.TaskItemID = intTaskID

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	if _, err := api.client.InsertTaskClassItemIntoSchedule(ctx, req); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (api *TaskClassHandler) DeleteTaskClassItem(c *gin.Context) {
	taskID := c.Query("task_item_id")
	intTaskID, err := strconv.Atoi(taskID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	if _, err := api.client.DeleteTaskClassItem(ctx, taskclasscontracts.DeleteTaskClassItemRequest{
		UserID:     userID,
		TaskItemID: intTaskID,
	}); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (api *TaskClassHandler) DeleteTaskClass(c *gin.Context) {
	taskClassID := c.Query("task_class_id")
	intTaskClassID, err := strconv.Atoi(taskClassID)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	userID := c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	if _, err := api.client.DeleteTaskClass(ctx, taskclasscontracts.DeleteTaskClassRequest{
		UserID:      userID,
		TaskClassID: intTaskClassID,
	}); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}

func (api *TaskClassHandler) UserInsertBatchTaskClassItemsIntoSchedule(c *gin.Context) {
	var req taskclasscontracts.ApplyBatchIntoScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	req.UserID = c.GetInt("user_id")

	ctx, cancel := context.WithTimeout(c.Request.Context(), taskClassRequestTimeout)
	defer cancel()
	if _, err := api.client.ApplyBatchIntoSchedule(ctx, req); err != nil {
		respond.DealWithError(c, err)
		return
	}
	c.JSON(http.StatusOK, respond.Ok)
}
