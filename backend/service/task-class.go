package service

import (
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
)

type TaskClassService struct {
	// 这里可以添加数据库连接或其他依赖
	taskClassRepo *dao.TaskClassDAO
}

func NewTaskClassService(taskClassRepo *dao.TaskClassDAO) *TaskClassService {
	return &TaskClassService{
		taskClassRepo: taskClassRepo,
	}
}

// AddTaskClass 为指定用户添加任务类
func (sv *TaskClassService) AddTaskClass(req *model.UserAddTaskClassRequest, userID int) error {
	return sv.taskClassRepo.Transaction(func(txDAO *dao.TaskClassDAO) error {
		//1.先转换
		taskClass, items, err := conv.ProcessUserAddTaskClassRequest(req, userID)
		if err != nil {
			return err
		}
		//2.插入 task class 并获取 ID
		taskClassID, err := txDAO.AddTaskClass(taskClass)
		if err != nil {
			return err
		}
		//3.插入 items
		for i := range items {
			items[i].CategoryID = &taskClassID // 关联 task class ID
		}
		if err := txDAO.AddTaskClassItems(items); err != nil {
			return err
		}
		return nil
	})
}

func (sv *TaskClassService) GetUserTaskClassInfos(userID int) (*model.UserGetTaskClassesResponse, error) {
	taskClasses, err := sv.taskClassRepo.GetUserTaskClasses(userID)
	if err != nil {
		return nil, err
	}
	resp := conv.TaskClassModelToResponse(taskClasses)
	return resp, nil
}
