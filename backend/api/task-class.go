package api

import (
	"errors"
	"net/http"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
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

func (api *TaskClassHandler) UserAddTaskClass(c *gin.Context) {
	var req model.UserAddTaskClassRequest
	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		return
	}
	userIDInterface := c.GetInt("user_id")
	err = api.svc.AddTaskClass(&req, userIDInterface)
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
	resp, err := api.svc.GetUserTaskClassInfos(userIDInterface)
	if err != nil {
		c.JSON(http.StatusInternalServerError, respond.InternalError(err))
		return
	}
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}
