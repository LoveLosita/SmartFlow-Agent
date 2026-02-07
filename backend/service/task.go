package service

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

type TaskService struct {
	// 伸出手：准备接住 DAO
	dao *dao.TaskDAO
}

// NewTaskService 创建 TaskService 实例
func NewTaskService(dao *dao.TaskDAO) *TaskService {
	return &TaskService{
		dao: dao,
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
	return response, nil
}

func (ts *TaskService) GetUserTasks(ctx context.Context, userID int) ([]model.GetUserTaskResp, error) {
	//1. 调用 courseDAO 层获取数据
	tasks, err := ts.dao.GetTasksByUserID(userID)
	if err != nil {
		return nil, err
	}
	//2. 调用 conv 层进行响应转换
	response := conv.ModelToGetUserTasksResp(tasks)
	return response, nil
}
