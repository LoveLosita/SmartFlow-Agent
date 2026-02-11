package service

import (
	"context"
	"errors"
	"log"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/go-redis/redis/v8"
)

type TaskService struct {
	// 伸出手：准备接住 DAO
	dao   *dao.TaskDAO
	cache *dao.CacheDAO
}

// NewTaskService 创建 TaskService 实例
func NewTaskService(dao *dao.TaskDAO, cache *dao.CacheDAO) *TaskService {
	return &TaskService{
		dao:   dao,
		cache: cache,
	}
}

func (ts *TaskService) AddTask(ctx context.Context, req *model.UserAddTaskRequest, userID int) (*model.UserAddTaskResponse, error) {
	//1. 调用 conv 层进行转换
	taskModel := conv.UserAddTaskRequestToModel(req, userID)
	//2.检查优先级是否合法
	if taskModel.Priority < 1 || taskModel.Priority >= 5 {
		return nil, respond.InvalidPriority
	}
	//3. 调用 courseDAO 层进行数据持久化
	createdTask, err := ts.dao.AddTask(taskModel)
	if err != nil {
		return nil, err
	}
	//4. 调用 conv 层进行响应转换
	response := conv.ModelToUserAddTaskResponse(createdTask)
	//5. 添加成功后，清除相关缓存（如果有的话），以保证数据一致性
	err = ts.cache.DeleteUserTasksFromCache(ctx, userID)
	if err != nil {
		// 缓存删除失败，记录日志但不影响正常返回数据
		log.Printf("Failed to delete user tasks cache for userID %d: %v", userID, err)
	}
	return response, nil
}

func (ts *TaskService) GetUserTasks(ctx context.Context, userID int) ([]model.GetUserTaskResp, error) {
	//1. 先尝试从缓存获取数据
	cachedResp, err := ts.cache.GetUserTasksFromCache(ctx, userID)
	if err == nil {
		// 缓存命中，直接返回
		return cachedResp, nil
	}
	// 如果是 redis.Nil 错误，说明缓存未命中，我们继续查库
	if !errors.Is(err, redis.Nil) {
		return nil, err // 其他错误，返回错误
	}
	//2. 调用 courseDAO 层获取数据
	tasks, err := ts.dao.GetTasksByUserID(userID)
	if err != nil {
		return nil, err
	}
	//3. 调用 conv 层进行响应转换
	response := conv.ModelToGetUserTasksResp(tasks)
	//4. 将结果存入缓存，设置合理的过期时间（24h）
	err = ts.cache.SetUserTasksToCache(ctx, userID, response)
	if err != nil {
		// 缓存写入失败，记录日志但不影响正常返回数据
		log.Printf("Failed to cache user tasks for userID %d: %v", userID, err)
	}
	return response, nil
}
