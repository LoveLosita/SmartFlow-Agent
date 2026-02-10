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

type TaskClassService struct {
	// 这里可以添加数据库连接或其他依赖
	taskClassRepo *dao.TaskClassDAO
	cacheRepo     *dao.CacheDAO
	scheduleRepo  *dao.ScheduleDAO
	repoManager   *dao.RepoManager // 统一管理多个 DAO 的事务
}

func NewTaskClassService(taskClassRepo *dao.TaskClassDAO, cacheRepo *dao.CacheDAO, scheduleRepo *dao.ScheduleDAO, manager *dao.RepoManager) *TaskClassService {
	return &TaskClassService{
		taskClassRepo: taskClassRepo,
		cacheRepo:     cacheRepo,
		scheduleRepo:  scheduleRepo,
		repoManager:   manager,
	}
}

// AddOrUpdateTaskClass 为指定用户添加任务类
func (sv *TaskClassService) AddOrUpdateTaskClass(ctx context.Context, req *model.UserAddTaskClassRequest, userID int, method int, targetTaskClassID int) error {
	// 1) 先写数据库（事务内）
	if err := sv.taskClassRepo.Transaction(func(txDAO *dao.TaskClassDAO) error {
		taskClass, items, err := conv.ProcessUserAddTaskClassRequest(req, userID)
		if err != nil {
			return err
		}
		if method == 1 { // 更新操作
			taskClass.ID = targetTaskClassID
		}

		taskClassID, err := txDAO.AddOrUpdateTaskClass(userID, taskClass)
		if err != nil {
			return err
		}

		for i := range items {
			items[i].CategoryID = &taskClassID
		}
		if err := txDAO.AddOrUpdateTaskClassItems(userID, items); err != nil {
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

func (sv *TaskClassService) GetUserCompleteTaskClass(ctx context.Context, userID int, taskClassID int) (*model.UserAddTaskClassRequest, error) {
	//1.查询数据库
	taskClass, err := sv.taskClassRepo.GetCompleteTaskClassByID(ctx, taskClassID, userID)
	if err != nil {
		return nil, err
	}
	//2.转换为响应结构体
	resp, err := conv.ProcessUserGetCompleteTaskClassRequest(taskClass)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (sv *TaskClassService) AddTaskClassItemIntoSchedule(ctx context.Context, req *model.UserInsertTaskClassItemToScheduleRequest, userID int, taskID int) error {
	//1.先验证任务块归属
	taskClassID, err := sv.taskClassRepo.GetTaskClassIDByTaskItemID(ctx, taskID) //通过任务块ID获取所属任务类ID
	if err != nil {
		return err
	}
	ownerID, err := sv.taskClassRepo.GetTaskClassUserIDByID(ctx, taskClassID) //通过任务类ID获取所属用户ID
	if err != nil {
		return err
	}
	if ownerID != userID {
		return respond.TaskClassItemNotBelongToUser
	}
	//2.再检查任务块本身是否已经被安排
	result, err := sv.taskClassRepo.IfTaskClassItemArranged(ctx, taskID)
	if err != nil {
		return err
	}
	if result {
		return respond.TaskClassItemAlreadyArranged
	}
	//3.取出任务块信息
	taskItem, err := sv.taskClassRepo.GetTaskClassItemByID(ctx, taskID) //通过任务块ID获取任务块信息
	if err != nil {
		return err
	}
	//更新TaskClassItem的embedded_time字段
	taskItem.EmbeddedTime = &model.TargetTime{
		DayOfWeek:   req.DayOfWeek,
		Week:        req.Week,
		SectionFrom: req.StartSection,
		SectionTo:   req.EndSection,
	}
	//3.判断是否嵌入课程
	if req.EmbedCourseEventID != 0 {
		//先检查看课程是否存在、是否归属该用户以及是否已经被嵌入了其他任务块
		courseOwnerID, err := sv.scheduleRepo.GetCourseUserIDByID(ctx, req.EmbedCourseEventID)
		if err != nil {
			return err
		}
		if courseOwnerID != userID {
			return respond.CourseNotBelongToUser
		}
		//再检查用户给的时间是否和课程的时间匹配(目前逻辑是给的区间必须完全匹配)
		match, err := sv.scheduleRepo.IsCourseTimeMatch(ctx, req.EmbedCourseEventID, req.Week, req.DayOfWeek, req.StartSection, req.EndSection)
		if err != nil {
			return err
		}
		if !match {
			return respond.CourseTimeNotMatch
		}
		//查询对应时段的课程是否已被其他任务块嵌入了（目前业务限制：一个课程只能被一个任务块嵌入，但是目前设计是支持多个任务块嵌入一节课的，只要放得下）
		isEmbedded, err := sv.scheduleRepo.IsCourseEmbeddedByOtherTaskBlock(ctx, req.EmbedCourseEventID, req.StartSection, req.EndSection)
		if err != nil {
			return err
		}
		if isEmbedded {
			return respond.CourseAlreadyEmbeddedByOtherTaskBlock
		}
		//嵌入课程，直接更新日程表对应时段的 embedded_task_id 字段
		err = sv.scheduleRepo.EmbedTaskIntoSchedule(req.StartSection, req.EndSection, req.DayOfWeek, req.Week, userID, taskID)
		if err != nil {
			return err
		}
		//更新任务块的 embedded_time 字段
		err = sv.taskClassRepo.UpdateTaskClassItemEmbeddedTime(ctx, taskID, taskItem.EmbeddedTime)
		if err != nil {
			return err
		}
		return nil
	}
	//4.否则构造Schedule模型
	sections := make([]int, 0, req.EndSection-req.StartSection+1)
	schedules, scheduleEvent := conv.UserInsertTaskItemRequestToModel(req, taskItem, nil, userID, req.StartSection, req.EndSection)
	//将节次区间转换为节次切片，方便后续检查冲突
	for section := req.StartSection; section <= req.EndSection; section++ {
		sections = append(sections, section)
	}
	//4.1 统一检查冲突（避免逐条查库）
	conflict, err := sv.scheduleRepo.HasUserScheduleConflict(ctx, userID, req.Week, req.DayOfWeek, sections)
	if err != nil {
		return err
	}
	if conflict {
		return respond.ScheduleConflict
	}
	//5.写入数据库（事务）
	/*if err := sv.taskClassRepo.Transaction(func(txDAO *dao.TaskClassDAO) error {
		//5.1 先将任务块插入scheduleEvent表，获取生成的ID后再插入schedule表（因为schedule表有外键关联）
		id, err := sv.scheduleRepo.AddScheduleEvent(scheduleEvent)
		if err != nil {
			return err
		}
		//5.2 将生成的 ScheduleEvent ID 赋值给对应的 Schedule 的 EventID 字段
		for i := range schedules {
			schedules[i].EventID = id
		}
		//5.3 插入 Schedule 表
		_, err = sv.scheduleRepo.AddSchedules(schedules)
		if err != nil {
			return err
		}
		//5.4 更新任务块的 embedded_time 字段
		if err := txDAO.UpdateTaskClassItemEmbeddedTime(ctx, taskID, taskItem.EmbeddedTime); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}*/
	// 5. 写入数据库（通过 RepoManager 统一管理事务）
	// 这里的 sv.daoManager 是你在初始化 Service 时注入的全局 RepoManager 实例
	if err := sv.repoManager.Transaction(ctx, func(txM *dao.RepoManager) error {
		// 5.1 使用事务中的 ScheduleRepo 插入 Event
		// 💡 这里的 txM.Schedule 已经注入了事务句柄
		eventID, err := txM.Schedule.AddScheduleEvent(scheduleEvent)
		if err != nil {
			return err // 触发回滚
		}
		// 5.2 关联 ID（纯内存操作，无需 tx）
		for i := range schedules {
			schedules[i].EventID = eventID
		}
		// 5.3 使用事务中的 ScheduleRepo 批量插入原子槽位
		// 💡 如果这里因为外键或唯一索引报错，5.1 的 Event 也会被撤回
		if _, err = txM.Schedule.AddSchedules(schedules); err != nil {
			return err // 触发回滚
		}
		// 5.4 使用事务中的 TaskRepo 更新任务状态
		// 💡 这里的 txM.Task 取代了你原来的 txDAO
		if err := txM.TaskClass.UpdateTaskClassItemEmbeddedTime(ctx, taskID, taskItem.EmbeddedTime); err != nil {
			return err // 触发回滚
		}
		return nil
	}); err != nil {
		// 这里处理最终的错误返回，比如 respond.Error
		return err
	}
	return nil
}
