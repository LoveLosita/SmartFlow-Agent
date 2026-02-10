package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/gin-gonic/gin"
)

type TaskHandler struct {
	// 伸出手：准备接住 Service
	svc *service.TaskService
}

// NewTaskHandler 创建 TaskHandler 实例
func NewTaskHandler(svc *service.TaskService) *TaskHandler {
	return &TaskHandler{
		svc: svc,
	}
}

func (th *TaskHandler) AddTask(c *gin.Context) {
	//1. 绑定请求参数
	var req model.UserAddTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		fmt.Println(err)
		return
	}
	// 用户ID从上下文中获取
	userID := c.GetInt("user_id")
	//2. 调用 Service 层处理业务逻辑
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	resp, err := th.svc.AddTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//3. 返回响应
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

func (th *TaskHandler) GetUserTasks(c *gin.Context) {
	// 用户ID从上下文中获取
	userID := c.GetInt("user_id")
	//2. 调用 Service 层处理业务逻辑
	// 创建一个带 1 秒超时的上下文
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel() // 记得释放资源
	resp, err := th.svc.GetUserTasks(ctx, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}
	//3. 返回响应
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}
