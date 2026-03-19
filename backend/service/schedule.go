package service

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/logic"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/go-redis/redis/v8"
)

type ScheduleService struct {
	scheduleDAO  *dao.ScheduleDAO
	userDAO      *dao.UserDAO
	taskClassDAO *dao.TaskClassDAO
	repoManager  *dao.RepoManager // 统一管理多个 DAO 的事务
	cacheDAO     *dao.CacheDAO    // 需要在 ScheduleService 中使用缓存
}

func NewScheduleService(scheduleDAO *dao.ScheduleDAO, userDAO *dao.UserDAO, taskClassDAO *dao.TaskClassDAO, repoManager *dao.RepoManager, cacheDAO *dao.CacheDAO) *ScheduleService {
	return &ScheduleService{
		scheduleDAO:  scheduleDAO,
		userDAO:      userDAO,
		taskClassDAO: taskClassDAO,
		repoManager:  repoManager,
		cacheDAO:     cacheDAO,
	}
}

func (ss *ScheduleService) GetUserTodaySchedule(ctx context.Context, userID int) ([]model.UserTodaySchedule, error) {
	//1.先检查用户id是否存在(考虑移除)
	/*_, err := ss.userDAO.GetUserByID(userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongUserID
		}
		return nil, err
	}*/
	//1.先尝试从缓存获取数据
	cachedResp, err := ss.cacheDAO.GetUserTodayScheduleFromCache(ctx, userID)
	if err == nil {
		// 缓存命中，直接返回
		return cachedResp, nil
	}
	// 如果是 redis.Nil 错误，说明缓存未命中，我们继续查库
	if !errors.Is(err, redis.Nil) {
		return nil, err
	}
	//2.获取当前日期
	/*curTime := time.Now().Format("2006-01-02")*/
	curTime := "2026-03-02" //测试数据
	week, dayOfWeek, err := conv.RealDateToRelativeDate(curTime)
	if err != nil {
		return nil, err
	}
	//3.查询用户当天的日程安排
	schedules, err := ss.scheduleDAO.GetUserTodaySchedule(ctx, userID, week, dayOfWeek) //测试数据
	if err != nil {
		return nil, err
	}
	//4.转换为前端需要的格式
	todaySchedules := conv.SchedulesToUserTodaySchedule(schedules)
	//5.将查询结果存入缓存，设置过期时间为当天结束
	err = ss.cacheDAO.SetUserTodayScheduleToCache(ctx, userID, todaySchedules)
	return todaySchedules, nil
}

func (ss *ScheduleService) GetUserWeeklySchedule(ctx context.Context, userID, week int) (*model.UserWeekSchedule, error) {
	//1.先检查 week 参数是否合法
	if week < 0 || week > 25 {
		return nil, respond.WeekOutOfRange
	}
	//2.先看看缓存里有没有数据（如果有的话直接返回，没有的话继续查库）
	cachedResp, err := ss.cacheDAO.GetUserWeeklyScheduleFromCache(ctx, userID, week)
	if err == nil {
		// 缓存命中，直接返回
		return cachedResp, nil
	}
	// 如果是 redis.Nil 错误，说明缓存未命中，我们继续查库
	if !errors.Is(err, redis.Nil) {
		return nil, err
	}
	//3.查询用户每周的日程安排
	//如果没有传入 week 参数，则默认查询当前周的日程安排
	if week == 0 {
		curTime := time.Now().Format("2006-01-02")
		var err error
		week, _, err = conv.RealDateToRelativeDate(curTime)
		if err != nil {
			return nil, err
		}
	}
	schedules, err := ss.scheduleDAO.GetUserWeeklySchedule(ctx, userID, week)
	if err != nil {
		return nil, err
	}
	//3.转换为前端需要的格式
	weeklySchedule := conv.SchedulesToUserWeeklySchedule(schedules)
	weeklySchedule.Week = week
	//4.将查询结果存入缓存，设置过期时间为一周（或者根据实际情况调整）
	err = ss.cacheDAO.SetUserWeeklyScheduleToCache(ctx, userID, weeklySchedule)
	return weeklySchedule, nil
}

func (ss *ScheduleService) DeleteScheduleEvent(ctx context.Context, requests []model.UserDeleteScheduleEvent, userID int) error {
	err := ss.repoManager.Transaction(ctx, func(txM *dao.RepoManager) error {
		for _, req := range requests {
			//1.如果要删课程和嵌入的事件
			if req.DeleteEmbeddedTask && req.DeleteCourse {
				//通过schedule表的embedded_task_id字段找到对应的task_id
				taskID, err := txM.Schedule.GetScheduleEmbeddedTaskID(ctx, req.ID)
				if err != nil {
					return err
				}
				//再将task_items表中对应的embedded_time字段设置为null
				if taskID != 0 {
					err = txM.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, taskID)
					if err != nil {
						return err
					}
				}
				//再删除课程事件和嵌入的事件（通过级联删除实现）
				err = txM.Schedule.DeleteScheduleEventAndSchedule(ctx, req.ID, userID)
				if err != nil {
					return err
				}
				continue
			}
			//2.只删课程/事件
			if req.DeleteCourse {
				//2.1.检查课程是否有嵌入的任务事件
				exists, err := txM.Schedule.IfScheduleEventIDExists(ctx, req.ID)
				if err != nil {
					return err
				}
				if !exists {
					return respond.WrongScheduleEventID
				}
				embeddedTaskID, err := txM.Schedule.GetScheduleEmbeddedTaskID(ctx, req.ID)
				if err != nil {
					return err
				}
				//2.2.如果有，则需另外为其创建新的scheduleEvent（type=task）
				//课程事件先删除后再创建任务事件
				if embeddedTaskID != 0 {
					//2.2.1.先通过id取出taskClassItem详情
					taskClassItem, err := txM.TaskClass.GetTaskClassItemByID(ctx, embeddedTaskID)
					if err != nil {
						return err
					}
					//下方开启事务，删除课程事件并创建新的任务事件
					//2.2.2.删除课程事件
					txErr := txM.Schedule.DeleteScheduleEventAndSchedule(ctx, req.ID, userID)
					if txErr != nil {
						return txErr
					}
					//2.2.3.再复用代码创建新的scheduleEvent，下方代码改编自AddTaskClassItemIntoSchedule函数
					//直接构造Schedule模型
					sections := make([]int, 0, taskClassItem.EmbeddedTime.SectionTo-taskClassItem.EmbeddedTime.SectionFrom+1)
					// 这里的 req 主要是为了传递 Week 和 DayOfWeek，其他字段不需要了
					schedules, scheduleEvent, err := conv.UserInsertTaskItemRequestToModel(
						&model.UserInsertTaskClassItemToScheduleRequest{
							Week:      taskClassItem.EmbeddedTime.Week,
							DayOfWeek: taskClassItem.EmbeddedTime.DayOfWeek},
						taskClassItem, nil, userID, taskClassItem.EmbeddedTime.SectionFrom, taskClassItem.EmbeddedTime.SectionTo)
					if err != nil {
						return err
					}
					//将节次区间转换为节次切片，方便后续检查冲突
					for section := taskClassItem.EmbeddedTime.SectionFrom; section <= taskClassItem.EmbeddedTime.SectionTo; section++ {
						sections = append(sections, section)
					}
					//单用户不存在删除时这个格子被占用的情况，所以不检查冲突了
					/*//4.1 统一检查冲突（避免逐条查库）
					conflict, err := ss.scheduleDAO.HasUserScheduleConflict(ctx, userID, req.Week, req.DayOfWeek, sections)
					if err != nil {
						return err
					}
					if conflict {
						return respond.ScheduleConflict
					}*/
					// 5. 写入数据库（通过 RepoManager 统一管理事务）
					// 这里的 sv.daoManager 是你在初始化 Service 时注入的全局 RepoManager 实例
					// 5.1 使用事务中的 ScheduleRepo 插入 Event
					eventID, txErr := txM.Schedule.AddScheduleEvent(scheduleEvent)
					if txErr != nil {
						return txErr // 触发回滚
					}
					// 5.2 关联 ID（纯内存操作，无需 tx）
					for i := range schedules {
						schedules[i].EventID = eventID
					}
					// 5.3 使用事务中的 ScheduleRepo 批量插入原子槽位
					if _, txErr = txM.Schedule.AddSchedules(schedules); txErr != nil {
						return txErr // 触发回滚
					}
					// 5.4 使用事务中的 TaskRepo 更新任务状态
					if txErr = txM.TaskClass.UpdateTaskClassItemEmbeddedTime(ctx, embeddedTaskID, taskClassItem.EmbeddedTime); txErr != nil {
						return txErr // 触发回滚
					}
					continue
				}
				//2.3.如果没有嵌入的事件，就直接删除课程事件
				err = txM.Schedule.DeleteScheduleEventAndSchedule(ctx, req.ID, userID)
				if err != nil {
					return err
				}
				//先通过rel_id找到对应的task_id
				taskID, txErr := txM.Schedule.GetRelIDByScheduleEventID(ctx, req.ID)
				if txErr != nil {
					return err
				}
				//2.4.如果是任务块，转而去清除task_items表中的嵌入时间
				if taskID != 0 {
					//再将task_items表中对应的embedded_time字段设置为null
					txErr = txM.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, taskID)
					if txErr != nil {
						return txErr
					}
				}
				continue
			}
			//3.只删嵌入的事件
			if req.DeleteEmbeddedTask {
				//下面先设置schedule表的embedded_task_id字段为null，再设置task_items表的embedded_time字段为null，实现删除嵌入事件的效果
				//3.1.先将schedule表的embedded_task_id字段设置为null
				taskID, txErr := txM.Schedule.SetScheduleEmbeddedTaskIDToNull(ctx, req.ID)
				if txErr != nil {
					return txErr
				}
				//3.2.再将task_items表的embedded_time字段设置为null
				txErr = txM.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, taskID)
				if txErr != nil {
					return txErr
				}
				continue
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

func (ss *ScheduleService) GetUserRecentCompletedSchedules(ctx context.Context, userID, index, limit int) (*model.UserRecentCompletedScheduleResponse, error) {
	//1.先查缓存
	cachedResp, err := ss.cacheDAO.GetUserRecentCompletedSchedulesFromCache(ctx, userID, index, limit)
	if err == nil {
		// 缓存命中，直接返回
		return cachedResp, nil
	}
	// 如果是 redis.Nil 错误，说明缓存未命中，我们继续查库
	if !errors.Is(err, redis.Nil) {
		return nil, err
	}
	//2.查询用户最近完成的日程安排
	//获取现在的时间
	/*nowTime := time.Now()*/
	nowTime := time.Date(2026, 6, 30, 12, 0, 0, 0, time.Local) //测试数据
	schedules, err := ss.scheduleDAO.GetUserRecentCompletedSchedules(ctx, nowTime, userID, index, limit)
	if err != nil {
		return nil, err
	}
	//3.转换为前端需要的格式
	result := conv.SchedulesToRecentCompletedSchedules(schedules)
	//4.将查询结果存入缓存，设置过期时间为30分钟（根据实际情况调整）
	err = ss.cacheDAO.SetUserRecentCompletedSchedulesToCache(ctx, userID, index, limit, result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (ss *ScheduleService) GetUserOngoingSchedule(ctx context.Context, userID int) (*model.OngoingSchedule, error) {
	//1.先查缓存
	cachedResp, err := ss.cacheDAO.GetUserOngoingScheduleFromCache(ctx, userID)
	if err == nil && cachedResp == nil {
		// 之前缓存过没有正在进行的日程，直接返回 nil
		return nil, respond.NoOngoingOrUpcomingSchedule
	}
	if err == nil {
		// 缓存命中，直接返回
		return cachedResp, nil
	}
	// 如果是 redis.Nil 错误，说明缓存未命中，我们继续查库
	if !errors.Is(err, redis.Nil) {
		return nil, err
	}
	//2.查询用户正在进行的日程安排
	/*nowTime := time.Now()*/
	nowTime := time.Date(2026, 6, 30, 18, 50, 0, 0, time.Local) //测试数据
	schedules, err := ss.scheduleDAO.GetUserOngoingSchedule(ctx, userID, nowTime)
	if err != nil {
		return nil, err
	}
	//3.转换为前端需要的格式
	result := conv.SchedulesToUserOngoingSchedule(schedules)
	if result != nil {
		if result.StartTime.After(nowTime) {
			result.TimeStatus = "upcoming"
		} else {
			result.TimeStatus = "ongoing"
		}
	}
	//4.将查询结果存入缓存，设置过期时间直到此任务结束（根据实际情况调整）
	err = ss.cacheDAO.SetUserOngoingScheduleToCache(ctx, userID, result)
	if err != nil {
		return nil, err
	}
	if result == nil {
		// 没有正在进行或即将开始的日程，返回特定错误
		return nil, respond.NoOngoingOrUpcomingSchedule
	}
	return result, nil
}

func (ss *ScheduleService) RevocateUserTaskClassItem(ctx context.Context, userID, eventID int) error {
	//1.先查库，看看这个event是任务事件还是课程事件，以及判断它是否属于用户
	eventType, err := ss.scheduleDAO.GetScheduleTypeByEventID(ctx, eventID, userID)
	if err != nil {
		return err
	}
	//2.根据查询结果进行不同的撤销操作
	if eventType == "course" {
		//下面开启事务，撤销嵌入事件
		err := ss.repoManager.Transaction(ctx, func(txM *dao.RepoManager) error {
			//下面先设置schedule表的embedded_task_id字段为null，再设置task_items表的embedded_time字段为null，实现删除嵌入事件的效果
			//3.1.先将schedule表的embedded_task_id字段设置为null
			taskID, txErr := txM.Schedule.SetScheduleEmbeddedTaskIDToNull(ctx, eventID)
			if txErr != nil {
				return txErr
			}
			//3.2.再将task_items表的embedded_time字段设置为null
			txErr = txM.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, taskID)
			if txErr != nil {
				return txErr
			}
			//3.3.最后设置task_items表的status字段为已撤销
			txErr = txM.Schedule.RevocateSchedulesByEventID(ctx, eventID)
			if txErr != nil {
				return txErr
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else if eventType == "task" {
		//下面开启事务，撤销任务事件
		err := ss.repoManager.Transaction(ctx, func(txM *dao.RepoManager) error {
			//先通过rel_id找到对应的task_id
			taskID, txErr := txM.Schedule.GetRelIDByScheduleEventID(ctx, eventID)
			if txErr != nil {
				return err
			}
			//再将task_items表中对应的embedded_time字段设置为null
			txErr = txM.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, taskID)
			if txErr != nil {
				return txErr
			}
			//最后将其从日程表中删除（通过级联删除实现）
			err = txM.Schedule.DeleteScheduleEventAndSchedule(ctx, eventID, userID)
			if err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
	} else {
		log.Println("ScheduleService.RevocateUserTaskClassItem: eventType is neither embedded_task nor task, something must be wrong")
	}
	return nil
}

func (ss *ScheduleService) SmartPlanning(ctx context.Context, userID, taskClassID int) ([]model.UserWeekSchedule, error) {
	//1.通过任务类id获取任务类详情
	taskClass, err := ss.taskClassDAO.GetCompleteTaskClassByID(ctx, taskClassID, userID)
	if err != nil {
		return nil, err
	}
	//2.校验任务类的参数是否合法
	if taskClass == nil {
		return nil, respond.WrongTaskClassID
	}
	if *taskClass.Mode != "auto" {
		return nil, respond.TaskClassModeNotAuto
	}
	//3.获取任务类安排的时间范围内的全部周数信息(左右边界不足一周的情况也要算作一周)
	schedules, err := ss.scheduleDAO.GetUserSchedulesByTimeRange(ctx, userID, conv.CalculateFirstDayOfWeek(*taskClass.StartDate), conv.CalculateLastDayOfWeek(*taskClass.EndDate))
	if err != nil {
		return nil, err
	}
	//4.将多个周的信息传入智能排课算法，获取推荐的时间安排（周+周内的天+节次）
	result, err := logic.SmartPlanningMainLogic(schedules, taskClass)
	if err != nil {
		return nil, err
	}
	//5.将推荐的时间安排转换为前端需要的格式返回
	return result, nil
}

// SmartPlanningRaw 执行粗排算法并同时返回展示结构和已分配的任务项。
//
// 职责边界：
// 1) 与 SmartPlanning 共享完全相同的前置校验和粗排逻辑；
// 2) 额外返回 allocatedItems（每项的 EmbeddedTime 已由算法回填），
//    供 Agent 排程链路直接转换为 BatchApplyPlans 请求，无需再让模型"二次分配"。
func (ss *ScheduleService) SmartPlanningRaw(ctx context.Context, userID, taskClassID int) ([]model.UserWeekSchedule, []model.TaskClassItem, error) {
	// 1. 获取任务类详情。
	taskClass, err := ss.taskClassDAO.GetCompleteTaskClassByID(ctx, taskClassID, userID)
	if err != nil {
		return nil, nil, err
	}
	if taskClass == nil {
		return nil, nil, respond.WrongTaskClassID
	}
	if *taskClass.Mode != "auto" {
		return nil, nil, respond.TaskClassModeNotAuto
	}

	// 2. 获取时间范围内的全部日程。
	schedules, err := ss.scheduleDAO.GetUserSchedulesByTimeRange(ctx, userID, conv.CalculateFirstDayOfWeek(*taskClass.StartDate), conv.CalculateLastDayOfWeek(*taskClass.EndDate))
	if err != nil {
		return nil, nil, err
	}

	// 3. 执行粗排算法，拿到已分配的 items（EmbeddedTime 已回填）。
	allocatedItems, err := logic.SmartPlanningRawItems(schedules, taskClass)
	if err != nil {
		return nil, nil, err
	}

	// 4. 同时生成展示结构，供 SSE 阶段推送给前端预览。
	displayResult := conv.PlanningResultToUserWeekSchedules(schedules, allocatedItems)
	return displayResult, allocatedItems, nil
}

// HybridScheduleWithPlan 构建"混合日程"：将既有日程与粗排建议合并为统一结构。
//
// 职责边界：
// 1) 获取 TaskClass 时间范围内的既有日程（课程 + 已落库任务）；
// 2) 调用粗排算法获取建议分配；
// 3) 将两者合并为 []HybridScheduleEntry，供 ReAct 精排引擎在内存中操作。
//
// 返回值：
// - entries：混合日程条目（existing + suggested）
// - allocatedItems：粗排已分配的任务项（用于后续落库）
// - error
func (ss *ScheduleService) HybridScheduleWithPlan(
	ctx context.Context, userID, taskClassID int,
) ([]model.HybridScheduleEntry, []model.TaskClassItem, error) {
	// 1. 获取任务类详情。
	taskClass, err := ss.taskClassDAO.GetCompleteTaskClassByID(ctx, taskClassID, userID)
	if err != nil {
		return nil, nil, err
	}
	if taskClass == nil {
		return nil, nil, respond.WrongTaskClassID
	}
	if *taskClass.Mode != "auto" {
		return nil, nil, respond.TaskClassModeNotAuto
	}

	// 2. 获取时间范围内的既有日程。
	schedules, err := ss.scheduleDAO.GetUserSchedulesByTimeRange(
		ctx, userID,
		conv.CalculateFirstDayOfWeek(*taskClass.StartDate),
		conv.CalculateLastDayOfWeek(*taskClass.EndDate),
	)
	if err != nil {
		return nil, nil, err
	}

	// 3. 执行粗排算法。
	allocatedItems, err := logic.SmartPlanningRawItems(schedules, taskClass)
	if err != nil {
		return nil, nil, err
	}

	// 4. 合并为 HybridScheduleEntry 切片。
	entries := make([]model.HybridScheduleEntry, 0, len(schedules)/2+len(allocatedItems))

	// 4.1 既有日程：按 EventID+Week+DayOfWeek 分组，合并连续节次。
	type eventGroupKey struct {
		EventID   int
		Week      int
		DayOfWeek int
	}
	type eventGroup struct {
		Key      eventGroupKey
		Name     string
		Type     string
		Sections []int
	}
	groupMap := make(map[eventGroupKey]*eventGroup)
	for _, s := range schedules {
		key := eventGroupKey{EventID: s.EventID, Week: s.Week, DayOfWeek: s.DayOfWeek}
		g, ok := groupMap[key]
		if !ok {
			name := "未知"
			typ := "course"
			if s.Event != nil {
				name = s.Event.Name
				typ = s.Event.Type
			}
			g = &eventGroup{Key: key, Name: name, Type: typ}
			groupMap[key] = g
		}
		g.Sections = append(g.Sections, s.Section)
	}
	for _, g := range groupMap {
		if len(g.Sections) == 0 {
			continue
		}
		// 排序后取首尾作为 SectionFrom/SectionTo
		minS, maxS := g.Sections[0], g.Sections[0]
		for _, s := range g.Sections[1:] {
			if s < minS {
				minS = s
			}
			if s > maxS {
				maxS = s
			}
		}
		entries = append(entries, model.HybridScheduleEntry{
			Week:        g.Key.Week,
			DayOfWeek:   g.Key.DayOfWeek,
			SectionFrom: minS,
			SectionTo:   maxS,
			Name:        g.Name,
			Type:        g.Type,
			Status:      "existing",
			EventID:     g.Key.EventID,
		})
	}

	// 4.2 粗排建议：每个已分配的 TaskClassItem 转为一条 suggested 条目。
	for _, item := range allocatedItems {
		if item.EmbeddedTime == nil {
			continue
		}
		name := "未命名任务"
		if item.Content != nil && strings.TrimSpace(*item.Content) != "" {
			name = strings.TrimSpace(*item.Content)
		}
		entries = append(entries, model.HybridScheduleEntry{
			Week:        item.EmbeddedTime.Week,
			DayOfWeek:   item.EmbeddedTime.DayOfWeek,
			SectionFrom: item.EmbeddedTime.SectionFrom,
			SectionTo:   item.EmbeddedTime.SectionTo,
			Name:        name,
			Type:        "task",
			Status:      "suggested",
			TaskItemID:  item.ID,
		})
	}

	return entries, allocatedItems, nil
}
