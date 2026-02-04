package service

import (
	"context"
	"errors"
	"log"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/go-redis/redis/v8"
)

type TaskClassService struct {
	// 这里可以添加数据库连接或其他依赖
	taskClassRepo *dao.TaskClassDAO
	cacheRepo     *dao.CacheDAO
}

func NewTaskClassService(taskClassRepo *dao.TaskClassDAO, cacheRepo *dao.CacheDAO) *TaskClassService {
	return &TaskClassService{
		taskClassRepo: taskClassRepo,
		cacheRepo:     cacheRepo,
	}
}

// AddTaskClass 为指定用户添加任务类
func (sv *TaskClassService) AddTaskClass(ctx context.Context, req *model.UserAddTaskClassRequest, userID int) error {
	// 1) 先写数据库（事务内）
	if err := sv.taskClassRepo.Transaction(func(txDAO *dao.TaskClassDAO) error {
		taskClass, items, err := conv.ProcessUserAddTaskClassRequest(req, userID)
		if err != nil {
			return err
		}

		taskClassID, err := txDAO.AddTaskClass(taskClass)
		if err != nil {
			return err
		}

		for i := range items {
			items[i].CategoryID = &taskClassID
		}
		if err := txDAO.AddTaskClassItems(items); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}

	// 2) 事务提交成功后删除该用户缓存(如果有)
	err := sv.cacheRepo.DeleteTaskClassList(ctx, userID)
	if err != nil {
		log.Printf("redis删除任务分类列表缓存失败: userID=%d err=%v", userID, err)
	}
	return nil
}

func (sv *TaskClassService) GetUserTaskClassInfos(ctx context.Context, userID int) (*model.UserGetTaskClassesResponse, error) {
	//1.先查询redis
	list, err := sv.cacheRepo.GetTaskClassList(ctx, userID)
	if err == nil {
		//命中缓存
		return &list, nil
	} else if !errors.Is(err, redis.Nil) { //不是缓存未命中错误，说明redis可能炸了，照常放行
		log.Println("redis获取任务分类列表失败:", err)
	}
	//2.缓存未命中，查询数据库
	taskClasses, err := sv.taskClassRepo.GetUserTaskClasses(userID)
	if err != nil {
		return nil, err
	}
	resp := conv.TaskClassModelToResponse(taskClasses)
	//3.写入缓存
	err = sv.cacheRepo.AddTaskClassList(ctx, userID, resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}
