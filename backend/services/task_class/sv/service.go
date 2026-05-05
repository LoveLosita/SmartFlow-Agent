package sv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/LoveLosita/smartflow/backend/conv"
	rootdao "github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	taskclassdao "github.com/LoveLosita/smartflow/backend/services/task_class/dao"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type TaskClassService struct {
	// 这里可以添加数据库连接或其他依赖
	taskClassRepo *taskclassdao.TaskClassDAO
	cacheRepo     *rootdao.CacheDAO
	scheduleRepo  *rootdao.ScheduleDAO
	repoManager   *rootdao.RepoManager // 统一管理多个 DAO 的事务
}

func NewTaskClassService(taskClassRepo *taskclassdao.TaskClassDAO, cacheRepo *rootdao.CacheDAO, scheduleRepo *rootdao.ScheduleDAO, manager *rootdao.RepoManager) *TaskClassService {
	return &TaskClassService{
		taskClassRepo: taskClassRepo,
		cacheRepo:     cacheRepo,
		scheduleRepo:  scheduleRepo,
		repoManager:   manager,
	}
}

// AddOrUpdateTaskClass 为指定用户添加任务类
func (sv *TaskClassService) AddOrUpdateTaskClass(ctx context.Context, req *model.UserAddTaskClassRequest, userID int, method int, targetTaskClassID int) error {
	//1.先校验参数
	if req.Mode == "auto" {
		if req.StartDate == "" || req.EndDate == "" {
			return respond.MissingParamForAutoScheduling
		}
		st, err := time.Parse("2006-01-02", req.StartDate)
		if err != nil {
			return respond.WrongParamType
		}
		ed, err := time.Parse("2006-01-02", req.EndDate)
		if err != nil {
			return respond.WrongParamType
		}
		if st.After(ed) {
			return respond.InvalidDateRange
		}
	}
	if req.Mode == "" || req.Name == "" || len(req.Items) == 0 {
		return respond.MissingParam
	}
	// 1. excluded_slots 属于“半天块索引”，每个索引映射 2 节（1->1-2，...，6->11-12）；
	// 2. 若允许 7~12，会在粗排网格展开时产生越界节次，触发运行时 panic；
	// 3. 这里统一在写入入口拦截，避免脏数据落库后污染后续排程链路。
	for _, slot := range req.Config.ExcludedSlots {
		if slot < 1 || slot > 6 {
			return respond.WrongParamType
		}
	}
	// 1. excluded_days_of_week 表示“整天不可排”的硬约束，粗排时会直接整天屏蔽；
	// 2. 只允许 1~7，对应周一到周日；
	// 3. 若写入非法值，会导致粗排过滤口径和前端展示口径不一致，因此入口直接拦截。
	for _, dayOfWeek := range req.Config.ExcludedDaysOfWeek {
		if dayOfWeek < 1 || dayOfWeek > 7 {
			return respond.WrongParamType
		}
	}
	//2.写数据库（事务内）
	if err := sv.taskClassRepo.Transaction(func(txDAO *taskclassdao.TaskClassDAO) error {
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

	return nil
}

func (sv *TaskClassService) GetUserTaskClassInfos(ctx context.Context, userID int) (*model.UserGetTaskClassesResponse, error) {
	//1.先查询redis
	list, err := sv.cacheRepo.GetTaskClassList(ctx, userID)
	if err == nil {
		//命中缓存
		return list, nil
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
	schedules, scheduleEvent, err := conv.UserInsertTaskItemRequestToModel(req, taskItem, nil, userID, req.StartSection, req.EndSection)
	if err != nil {
		return err
	}
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
	// 5. 写入数据库（通过 RepoManager 统一管理事务）
	// 这里的 sv.daoManager 是你在初始化 Service 时注入的全局 RepoManager 实例
	if err := sv.repoManager.Transaction(ctx, func(txM *rootdao.RepoManager) error {
		// 5.1 使用事务中的 ScheduleRepo 插入 Event
		// 💡 这里的 txM.Schedule 已经注入了事务句柄
		//此处要将req中的起始section以及第几周、星期几转换成绝对时间，存入scheduleEvent的StartTime和EndTime字段中，方便后续查询和冲突检查
		st, ed, err := conv.RelativeTimeToRealTime(req.Week, req.DayOfWeek, req.StartSection, req.EndSection)
		if err != nil {
			return err
		}
		scheduleEvent.StartTime = st
		scheduleEvent.EndTime = ed
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

func (sv *TaskClassService) DeleteTaskClassItem(ctx context.Context, userID int, taskItemID int) error {
	//1.先验证任务块归属
	taskClassID, err := sv.taskClassRepo.GetTaskClassIDByTaskItemID(ctx, taskItemID) //通过任务块ID获取所属任务类ID
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
	//2.如果该任务块已经被安排了，先解除安排，再删除任务块（事务）
	if err := sv.repoManager.Transaction(ctx, func(txM *rootdao.RepoManager) error {
		//2.1.先检查该任务块是否已经被安排了
		arranged, err := txM.TaskClass.IfTaskClassItemArranged(ctx, taskItemID)
		if err != nil {
			return err
		}
		if arranged {
			//2.2.如果已经被安排了，先解除安排
			//先扫schedules找到该task_item_id并删除
			_, txErr := txM.Schedule.FindEmbeddedTaskIDAndDeleteIt(ctx, taskItemID)
			//2.3.再将task_items表的embedded_time字段设置为null
			txErr = txM.TaskClass.DeleteTaskClassItemEmbeddedTime(ctx, taskItemID)
			if txErr != nil {
				return txErr
			}
			//再删除schedule_event表中对应的事件
			txErr = txM.Schedule.DeleteScheduleEventByTaskItemID(ctx, taskItemID)
			if txErr != nil {
				return txErr
			}
		}
		//2.4.最后删除任务块
		err = txM.TaskClass.DeleteTaskClassItemByID(ctx, taskItemID)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (sv *TaskClassService) DeleteTaskClass(ctx context.Context, userID int, taskClassID int) error {
	//1.先验证任务类归属
	ownerID, err := sv.taskClassRepo.GetTaskClassUserIDByID(ctx, taskClassID) //通过任务类ID获取所属用户ID
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return respond.WrongTaskClassID
		}
		return err
	}
	if ownerID != userID {
		return respond.TaskClassNotBelongToUser
	}
	//2.删除任务类（事务）
	err = sv.taskClassRepo.DeleteTaskClassByID(ctx, taskClassID, userID)
	if err != nil {
		return err
	}
	return nil
}

// GetCompleteTaskClassByID 获取任务类完整详情（含关联的 TaskClassItem 列表）。
//
// 职责边界：
// 1) 直接委托 DAO 层查询，不做额外业务逻辑；
// 2) 主要供 Agent 排程链路使用，获取 Items 用于 materialize 节点映射。
func (sv *TaskClassService) GetCompleteTaskClassByID(ctx context.Context, taskClassID, userID int) (*model.TaskClass, error) {
	return sv.taskClassRepo.GetCompleteTaskClassByID(ctx, taskClassID, userID)
}

func (sv *TaskClassService) BatchApplyPlans(ctx context.Context, taskClassID int, userID int, plans *model.UserInsertTaskClassItemToScheduleRequestBatch) error {
	//1.通过任务类id获取任务类详情
	taskClass, err := sv.taskClassRepo.GetCompleteTaskClassByID(ctx, taskClassID, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return respond.WrongTaskClassID
		}
		return err
	}
	//2.校验任务类的参数是否合法
	if taskClass == nil {
		return respond.WrongTaskClassID
	}
	if *taskClass.Mode != "auto" {
		return respond.TaskClassModeNotAuto
	}
	//3.获取任务类安排的时间范围内的全部周数信息(左右边界不足一周的情况也要算作一周)，用于下方冲突检查
	startWeekTime := conv.CalculateFirstDayOfWeek(*taskClass.StartDate)
	endWeekTime := conv.CalculateLastDayOfWeek(*taskClass.EndDate)
	schedules, err := sv.scheduleRepo.GetUserSchedulesByTimeRange(ctx, userID, startWeekTime, endWeekTime)
	if err != nil {
		return err
	}
	startWeek, _, err := conv.RealDateToRelativeDate(startWeekTime.Format("2006-01-02"))
	if err != nil {
		return err
	}
	endWeek, _, err := conv.RealDateToRelativeDate(endWeekTime.Format("2006-01-02"))
	if err != nil {
		return err
	}
	//4.统一检查冲突（避免逐条查库）
	//先将日程放入一个map中，key是"周-星期-节次"，value是课程信息，方便后续检查冲突
	courseMap := make(map[string]model.Schedule)
	for _, schedule := range schedules {
		key := fmt.Sprintf("%d-%d-%d", schedule.Week, schedule.DayOfWeek, schedule.Section)
		courseMap[key] = schedule
	}
	//再遍历每个任务块的安排时间，检查是否和课程冲突（目前逻辑是只要有一个时段冲突就算冲突，后续可以优化为统计冲突的时段数量，或者提供具体的冲突时段信息）
	for _, plan := range plans.Items {
		if plan.Week < startWeek || plan.Week > endWeek {
			return respond.TaskClassItemTryingToInsertOutOfTimeRange
		}
		for section := plan.StartSection; section <= plan.EndSection; section++ {
			key := fmt.Sprintf("%d-%d-%d", plan.Week, plan.DayOfWeek, section)
			// 如果课程存在，并且满足以下任一条件则认为冲突：
			// 1. 课程时段已经被其他任务块嵌入了（不允许多个任务块嵌入同一课程）
			// 2. 当前时段的课的EventID与用户计划中指定的EmbedCourseEventID不匹配（说明用户计划要嵌入的课程和当前时段的课不是同一节）
			// 3. 用户计划中没有指定EmbedCourseEventID（即EmbedCourseEventID为0），但当前时段有课（不允许在有课的时段安排任务块）
			// 4. 当前时段的课不允许被嵌入（即使用户计划中指定了EmbedCourseEventID，但如果课程本身不允许被嵌入了，也算冲突）
			if course, exists := courseMap[key]; exists && ((plan.EmbedCourseEventID != 0 && course.EmbeddedTask != nil) ||
				(plan.EmbedCourseEventID != course.EventID) || plan.EmbedCourseEventID == 0 || !course.Event.CanBeEmbedded) {
				return respond.ScheduleConflict
			}
		}
	}
	//5.分流批量写入数据库（通过 RepoManager 统一管理事务）
	//先分流
	toEmbed := make([]model.SingleTaskClassItem, 0)  //需要嵌入课程的任务块
	toNormal := make([]model.SingleTaskClassItem, 0) //需要新建日程的任务块
	for _, item := range plans.Items {
		if item.EmbedCourseEventID != 0 {
			toEmbed = append(toEmbed, item)
		} else {
			toNormal = append(toNormal, item)
		}
	}
	//再开事务批量写库
	if err := sv.repoManager.Transaction(ctx, func(txM *rootdao.RepoManager) error {
		//5.1 先处理需要嵌入课程的任务块
		//先提取出需要嵌入的课程ID和TaskItemID列表
		courseIDs := make([]int, 0, len(toEmbed))
		for _, item := range toEmbed {
			courseIDs = append(courseIDs, item.EmbedCourseEventID)
		}
		itemIDs := make([]int, 0, len(toEmbed))
		for _, item := range toEmbed {
			itemIDs = append(itemIDs, item.TaskItemID)
		}
		//检查任务块本身是否已经被安排
		result, err := sv.taskClassRepo.BatchCheckIfTaskClassItemsArranged(ctx, itemIDs)
		if err != nil {
			return err
		}
		if result {
			return respond.TaskClassItemAlreadyArranged
		}
		//验证一下plans中的taskItemID确实都属于这个用户和这个任务类（避免用户恶意构造请求把别的用户的任务块或者不属于任何任务类的任务块也安排了）
		//同时也能检查是否重复
		result, err = sv.taskClassRepo.ValidateTaskItemIDsBelongToTaskClass(ctx, taskClassID, itemIDs)
		if err != nil {
			return err
		}
		if !result {
			return respond.TaskClassItemNotBelongToTaskClass
		}
		//批量更新日程表中对应课程的embedded_task_id字段（目前业务限制：一个课程只能被一个任务块嵌入了，所以直接批量更新，不用担心覆盖问题）
		err = txM.Schedule.BatchEmbedTaskIntoSchedule(ctx, courseIDs, itemIDs)
		if err != nil {
			return err
		}
		//批量更新任务块的embedded_time字段
		targetTimes := make([]*model.TargetTime, 0, len(toEmbed))
		for _, item := range toEmbed {
			targetTimes = append(targetTimes, &model.TargetTime{
				DayOfWeek:   item.DayOfWeek,
				Week:        item.Week,
				SectionFrom: item.StartSection,
				SectionTo:   item.EndSection,
			})
		}
		err = txM.TaskClass.BatchUpdateTaskClassItemEmbeddedTime(ctx, itemIDs, targetTimes)
		if err != nil {
			return err
		}
		//5.2 再处理需要新建日程的任务块
		//先提取出需要新建日程的任务块ID列表
		normalItemIDs := make([]int, 0, len(toNormal))
		for _, item := range toNormal {
			normalItemIDs = append(normalItemIDs, item.TaskItemID)
		}
		//验证一下plans中的taskItemID确实都属于这个任务类（避免用户恶意构造请求把别的用户的任务块或者不属于任何任务类的任务块也安排了）
		result, err = sv.taskClassRepo.ValidateTaskItemIDsBelongToTaskClass(ctx, taskClassID, normalItemIDs)
		if err != nil {
			return err
		}
		if !result {
			return respond.TaskClassItemNotBelongToTaskClass
		}
		//批量提取TaskItems
		taskItems, err := txM.TaskClass.GetTaskClassItemsByIDs(ctx, normalItemIDs)
		if err != nil {
			return err
		}
		if len(taskItems) != len(normalItemIDs) {
			log.Printf("警告：批量提取任务块时，返回的任务块数量与请求中的任务块ID数量不匹配，可能存在数据问题。请求ID数量：%d，返回任务块数量：%d", len(normalItemIDs), len(taskItems))
			return respond.InternalError(errors.New("返回的任务块数量与请求中的任务块ID数量不匹配，可能存在数据问题"))
		}
		//将toNormal按照TaskItemID升序排序，将taskItems也按照ID升序排序，保证一一对应关系（上面已经检查过重复）
		//如果请求中的任务块ID有重复，这里就无法保证一一对应关系了，后续可以考虑在请求层面加一个校验，拒绝包含重复任务块ID的请求
		sort.SliceStable(toNormal, func(i, j int) bool {
			return toNormal[i].TaskItemID < toNormal[j].TaskItemID
		})
		sort.SliceStable(taskItems, func(i, j int) bool {
			return taskItems[i].ID < taskItems[j].ID
		})
		//开始构建event和schedules
		finalSchedules := make([]model.Schedule, 0)           //最终要插入数据库的Schedule切片
		finalScheduleEvents := make([]model.ScheduleEvent, 0) //最终要插入数据库的ScheduleEvent切片
		pos := make([]int, 0)                                 //记录每个任务块对应的Schedule在finalSchedules中的位置，方便后续批量插入数据库后回填EventID
		for i := 0; i < len(toNormal); i++ {
			item := toNormal[i]
			taskItem := taskItems[i]
			if item.StartSection < 1 || item.EndSection > 12 || item.StartSection > item.EndSection {
				return respond.InvalidSectionRange
			}
			schedules, scheduleEvent, err := conv.UserInsertTaskItemRequestToModel(&model.UserInsertTaskClassItemToScheduleRequest{
				Week:               item.Week,
				DayOfWeek:          item.DayOfWeek,
				StartSection:       item.StartSection,
				EndSection:         item.EndSection,
				EmbedCourseEventID: 0, //不嵌入课程
			}, &taskItem, nil, userID, item.StartSection, item.EndSection)
			if err != nil {
				return err
			}
			finalScheduleEvents = append(finalScheduleEvents, *scheduleEvent)
			for range schedules {
				pos = append(pos, len(finalScheduleEvents)-1)
			}
			finalSchedules = append(finalSchedules, schedules...)
		}
		//最后批量插入数据库
		//先插入ScheduleEvent表，获取生成的EventID，再批量插入Schedule表，最后批量更新TaskClassItem的embedded_time字段
		ids, err := txM.Schedule.InsertScheduleEvents(ctx, finalScheduleEvents)
		if err != nil {
			return err
		}
		// 将生成的 ScheduleEvent ID 赋值给对应的 Schedule 的 EventID 字段
		for i := range finalSchedules {
			finalSchedules[i].EventID = ids[pos[i]]
		}
		if _, err = txM.Schedule.AddSchedules(finalSchedules); err != nil {
			return err
		}
		//批量更新任务块的embedded_time字段
		targetTimes = make([]*model.TargetTime, 0, len(toEmbed))
		for _, item := range toNormal {
			targetTimes = append(targetTimes, &model.TargetTime{
				DayOfWeek:   item.DayOfWeek,
				Week:        item.Week,
				SectionFrom: item.StartSection,
				SectionTo:   item.EndSection,
			})
		}
		//提取出所有需要更新的任务块ID
		itemIDs = make([]int, 0, len(toNormal))
		for _, item := range toNormal {
			itemIDs = append(itemIDs, item.TaskItemID)
		}
		err = txM.TaskClass.BatchUpdateTaskClassItemEmbeddedTime(ctx, itemIDs, targetTimes)
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}
