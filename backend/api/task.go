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

// BatchTaskStatus 批量查询当前用户任务的实时完成状态。
//
// 职责边界：
// 1. 负责解析 ids 与读取鉴权上下文中的 user_id；
// 2. 负责调用 Service 复用任务缓存读取链路；
// 3. 不修改任务、不触发幂等中间件、不反写 NewAgent timeline 历史 payload。
func (th *TaskHandler) BatchTaskStatus(c *gin.Context) {
	// 1. 绑定请求参数。ids 允许为空切片，表示前端当前没有需要 hydration 的任务卡片。
	var req model.BatchTaskStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, respond.WrongParamType)
		fmt.Println(err)
		return
	}

	// 2. 从鉴权上下文读取 user_id，Service 会继续用该 user_id 限定任务集合。
	userID := c.GetInt("user_id")

	// 3. 设置短超时：该接口只读缓存/任务列表，避免异常情况下长时间占用连接。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1*time.Second)
	defer cancel()

	// 4. 调用 Service 做 ID 归一化与当前状态查询。
	resp, err := th.svc.BatchTaskStatus(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构，items 为空时仍按 success 返回，便于前端无分支处理。
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

	// 4. 调用 Service 执行"标记完成"逻辑。
	resp, err := th.svc.CompleteTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// UndoCompleteTask 取消任务"已完成"勾选。
//
// 职责边界：
// 1. 负责解析请求与读取 user_id；
// 2. 负责调用 Service 执行业务恢复；
// 3. 不负责"任务是否已完成"的业务判断（由 Service/DAO 负责）。
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

	// 4. 调用 Service 执行"取消已完成勾选"逻辑。
	resp, err := th.svc.UndoCompleteTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// UpdateTask 更新任务属性（部分更新）。
//
// 职责边界：
// 1. 负责解析请求与读取 user_id；
// 2. 负责调用 Service 执行业务；
// 3. 不负责幂等校验（幂等由路由中间件处理）。
func (th *TaskHandler) UpdateTask(c *gin.Context) {
	// 1. 绑定请求参数。
	var req model.UserUpdateTaskRequest
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

	// 4. 调用 Service 执行更新逻辑。
	resp, err := th.svc.UpdateTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, resp))
}

// DeleteTask 永久删除指定任务。
//
// 职责边界：
// 1. 负责解析请求与读取 user_id；
// 2. 负责调用 Service 执行删除；
// 3. 不负责幂等校验（幂等由路由中间件处理）。
func (th *TaskHandler) DeleteTask(c *gin.Context) {
	// 1. 绑定请求参数。
	var req model.UserCompleteTaskRequest
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

	// 4. 调用 Service 执行删除逻辑。
	taskID, err := th.svc.DeleteTask(ctx, &req, userID)
	if err != nil {
		respond.DealWithError(c, err)
		return
	}

	// 5. 返回统一响应结构。
	c.JSON(http.StatusOK, respond.RespWithData(respond.Ok, gin.H{"task_id": taskID}))
}
