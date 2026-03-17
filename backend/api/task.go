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

// CompleteTask 标记任务为已完成。
//
// 职责边界：
// 1. 负责解析请求与读取 user_id；
// 2. 负责调用 Service 执行业务；
// 3. 不负责幂等校验（幂等由路由中间件处理）。
func (th *TaskHandler) CompleteTask(c *gin.Context) {
	// 1. 绑定请求参数。
	var req model.UserCompleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		fmt.Println(err)
		return
	}

	// 2. 从鉴权上下文获取 user_id，保证只能操作自己的任务。
	userID := c.GetInt("user_id")

	// 3. 设置短超时，避免该写接口长期占用连接。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	// 4. 调用 Service 执行“标记完成”逻辑。
	resp, err := th.svc.CompleteTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// UndoCompleteTask 取消任务“已完成”勾选。
//
// 职责边界：
// 1. 负责解析请求与读取 user_id；
// 2. 负责调用 Service 执行业务恢复；
// 3. 不负责“任务是否已完成”的业务判断（由 Service/DAO 负责）。
func (th *TaskHandler) UndoCompleteTask(c *gin.Context) {
	// 1. 绑定请求参数。
	var req model.UserUndoCompleteTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		fmt.Println(err)
		return
	}

	// 2. 从鉴权上下文读取 user_id，保证只操作当前用户任务。
	userID := c.GetInt("user_id")

	// 3. 设置短超时，避免该写接口占用连接过久。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	// 4. 调用 Service 执行“取消已完成勾选”逻辑。
	resp, err := th.svc.UndoCompleteTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}
