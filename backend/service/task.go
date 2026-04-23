package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

const (
	// taskUrgencyPromoteDedupeTTL 是"同一任务平移请求"的去重锁有效期。
	//
	// 设计考虑：
	// 1. 太短会导致消费稍慢时被重复投递；
	// 2. 太长会导致首次投递失败后恢复变慢；
	// 3. 这里先取 120 秒作为折中值，后续可按线上观测再调优。
	taskUrgencyPromoteDedupeTTL = 120 * time.Second
	// taskUrgencyPromoteDedupeKeyFmt 是任务平移去重键模板。
	taskUrgencyPromoteDedupeKeyFmt = "smartflow:task:promote:pending:%d:%d"
)

type TaskService struct {
	// dao 负责任务表读写。
	dao *dao.TaskDAO
	// cache 负责任务列表缓存与 Redis 去重锁能力。
	cache *dao.CacheDAO
	// eventPublisher 负责发布 outbox 事件（可能为空：例如未启用 Kafka/总线时）。
	eventPublisher outboxinfra.EventPublisher
}

// NewTaskService 创建 TaskService 实例。
//
// 职责边界：
// 1. 只做依赖注入，不做连接可用性探测；
// 2. 允许 eventPublisher 为空（用于本地降级场景）。
func NewTaskService(taskDAO *dao.TaskDAO, cacheDAO *dao.CacheDAO, eventPublisher outboxinfra.EventPublisher) *TaskService {
	return &TaskService{
		dao:            taskDAO,
		cache:          cacheDAO,
		eventPublisher: eventPublisher,
	}
}

// AddTask 新增任务。
//
// 职责边界：
// 1. 负责参数转换、优先级合法性校验与写库；
// 2. 不负责"紧急性自动平移"逻辑（该逻辑发生在任务读取时的懒触发链路）。
func (ts *TaskService) AddTask(ctx context.Context, req *model.UserAddTaskRequest, userID int) (*model.UserAddTaskResponse, error) {
	// 1. 把用户请求转换为内部模型，避免 API 层结构直接泄漏到 DAO。
	taskModel := conv.UserAddTaskRequestToModel(req, userID)
	// 2. 优先级范围校验：当前任务体系只允许 1~4。
	if taskModel.Priority < 1 || taskModel.Priority >= 5 {
		return nil, respond.InvalidPriority
	}
	// 3. 写库。
	createdTask, err := ts.dao.AddTask(taskModel)
	if err != nil {
		return nil, err
	}
	// 4. 返回对外响应 DTO。
	response := conv.ModelToUserAddTaskResponse(createdTask)
	return response, nil
}

// CompleteTask 将用户指定任务标记为"已完成"。
//
// 职责边界：
// 1. 负责入参校验与业务错误映射；
// 2. 负责调用 DAO 执行状态更新；
// 3. 不负责幂等键校验（幂等由中间件处理）；
// 4. 不负责缓存删除细节（缓存删除由 GORM cache_deleter 回调触发）。
func (ts *TaskService) CompleteTask(ctx context.Context, req *model.UserCompleteTaskRequest, userID int) (*model.UserCompleteTaskResponse, error) {
	// 1. 参数兜底：请求体为空、非法 user 或非法 task_id 直接返回业务错误。
	if req == nil || userID <= 0 || req.TaskID <= 0 {
		return nil, respond.WrongTaskID
	}

	// 2. 调用 DAO 执行"查询 + 必要时更新"。
	updatedTask, alreadyCompleted, err := ts.dao.CompleteTaskByID(ctx, userID, req.TaskID)
	if err != nil {
		// 2.1 任务不存在或不属于当前用户时，统一映射为 WrongTaskID。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongTaskID
		}
		// 2.2 其余数据库异常向上透传，交由统一错误处理器返回 500。
		return nil, err
	}
	if updatedTask == nil {
		// 3. 极端防御：DAO 不应返回 nil，若发生则视为内部异常。
		return nil, errors.New("complete task succeeded but task is nil")
	}

	// 4. 构造响应：
	// 4.1 already_completed=true 表示本次命中幂等，不影响最终成功状态；
	// 4.2 is_completed 始终为 true，便于前端直接刷新状态。
	resp := &model.UserCompleteTaskResponse{
		TaskID:           updatedTask.ID,
		IsCompleted:      true,
		AlreadyCompleted: alreadyCompleted,
		Status:           "completed",
	}
	return resp, nil
}

// UndoCompleteTask 取消用户任务的"已完成勾选"。
//
// 职责边界：
// 1. 负责入参校验与业务错误映射；
// 2. 负责调用 DAO 执行状态恢复；
// 3. 不负责幂等缓存（本接口按需求要求：任务未完成时必须报错）；
// 4. 不负责缓存删除细节（由 GORM cache_deleter 回调自动处理）。
func (ts *TaskService) UndoCompleteTask(ctx context.Context, req *model.UserUndoCompleteTaskRequest, userID int) (*model.UserUndoCompleteTaskResponse, error) {
	// 1. 参数兜底：请求体为空、非法 user 或非法 task_id 直接返回业务错误。
	if req == nil || userID <= 0 || req.TaskID <= 0 {
		return nil, respond.WrongTaskID
	}

	// 2. 调用 DAO 执行"恢复未完成"逻辑。
	updatedTask, err := ts.dao.UndoCompleteTaskByID(ctx, userID, req.TaskID)
	if err != nil {
		// 2.1 任务不存在或不属于当前用户，统一映射为 WrongTaskID。
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, respond.WrongTaskID
		}
		// 2.2 任务本来就未完成：按需求返回明确业务错误。
		if errors.Is(err, respond.TaskNotCompleted) {
			return nil, respond.TaskNotCompleted
		}
		// 2.3 其余数据库异常继续向上透传。
		return nil, err
	}
	if updatedTask == nil {
		// 3. 极端防御：DAO 成功但返回 nil，视为内部异常。
		return nil, errors.New("undo complete task succeeded but task is nil")
	}

	// 4. 组装响应：恢复成功后 is_completed 恒为 false。
	resp := &model.UserUndoCompleteTaskResponse{
		TaskID:      updatedTask.ID,
		IsCompleted: false,
		Status:      "uncompleted",
	}
	return resp, nil
}

// GetUserTasks 获取用户任务列表（含"读时紧急性派生"与"异步平移触发"）。
//
// 核心流程（步骤化）：
// 1. 先读缓存，未命中再回源 DB，并把"原始模型"回填缓存；
// 2. 在内存里做"读时派生"：仅用于本次返回给前端，不直接改库；
// 3. 收集"已到紧急分界线且仍处于非紧急象限"的任务 ID；
// 4. 通过 Redis SETNX 去重后，发布 outbox 事件异步落库；
// 5. 无论发布成功与否，都优先返回本次派生结果，保证用户读体验。
//
// 一致性策略：
// 1. 缓存里存的是原始任务，不是派生后的优先级；
// 2. 真实平移由异步消费者条件更新 DB；
// 3. DB 更新后由 cache_deleter 自动删缓存，下一次读取自然拿到新状态。
func (ts *TaskService) GetUserTasks(ctx context.Context, userID int) ([]model.GetUserTaskResp, error) {
	derivedTasks, err := ts.GetTasksWithUrgencyPromotion(ctx, userID)
	if err != nil {
		return nil, err
	}
	return conv.ModelToGetUserTasksResp(derivedTasks), nil
}

// GetTasksWithUrgencyPromotion 读取用户任务并应用读时紧急性提升 + 异步落库触发。
//
// 统一入口，供前端查询（GetUserTasks）和 LLM 工具查询（QueryTasksForTool）复用。
// 调用方不应假设 DB 已更新——持久化是异步的。
func (ts *TaskService) GetTasksWithUrgencyPromotion(ctx context.Context, userID int) ([]model.Task, error) {
	rawTasks, err := ts.getRawUserTasks(ctx, userID)
	if err != nil {
		return nil, err
	}
	derivedTasks, duePromoteTaskIDs := deriveTaskUrgencyForRead(rawTasks, time.Now())
	ts.tryEnqueueTaskUrgencyPromote(ctx, userID, duePromoteTaskIDs)
	return derivedTasks, nil
}

// getRawUserTasks 读取"原始任务模型"。
//
// 职责边界：
// 1. 负责缓存命中/回源 DB/回填缓存；
// 2. 不做优先级派生，不做异步事件投递；
// 3. 缓存写失败只记日志，不阻断主流程。
func (ts *TaskService) getRawUserTasks(ctx context.Context, userID int) ([]model.Task, error) {
	// 1. 先查缓存：命中则直接返回。
	cachedTasks, err := ts.cache.GetUserTasksFromCache(ctx, userID)
	if err == nil {
		return cachedTasks, nil
	}

	// 2. 非 redis.Nil 错误直接返回，避免掩盖真实故障。
	if !errors.Is(err, redis.Nil) {
		return nil, err
	}

	// 3. 缓存未命中回源 DB。
	dbTasks, err := ts.dao.GetTasksByUserID(userID)
	if err != nil {
		return nil, err
	}

	// 4. 回填缓存（失败不阻断主链路）。
	if setErr := ts.cache.SetUserTasksToCache(ctx, userID, dbTasks); setErr != nil {
		log.Printf("写入用户任务缓存失败: user_id=%d err=%v", userID, setErr)
	}
	return dbTasks, nil
}

// deriveTaskUrgencyForRead 对任务做"读时紧急性派生"，并收集需要异步落库的任务 ID。
//
// 职责边界：
// 1. 只在内存里改本次返回值，不写 DB；
// 2. 只做"到线且未完成任务"的优先级映射；
// 3. 不处理去重锁和事件发布。
//
// 返回语义：
// 1. 第一个返回值：可直接用于响应前端的派生任务切片；
// 2. 第二个返回值：需要发"异步平移事件"的任务 ID 列表（可能为空）。
func deriveTaskUrgencyForRead(tasks []model.Task, now time.Time) ([]model.Task, []int) {
	// 1. 拷贝切片，避免修改调用方持有的原始数据。
	derived := make([]model.Task, len(tasks))
	copy(derived, tasks)

	pendingPromoteTaskIDs := make([]int, 0, len(derived))

	// 2. 逐条判断是否满足"自动平移"条件。
	for idx := range derived {
		current := &derived[idx]

		// 2.1 已完成任务不参与平移。
		if current.IsCompleted {
			continue
		}
		// 2.2 没有分界线的任务不参与平移。
		if current.UrgencyThresholdAt == nil {
			continue
		}
		// 2.3 尚未到分界线，不平移。
		if current.UrgencyThresholdAt.After(now) {
			continue
		}

		// 2.4 到线后，仅把"不紧急象限"平移到对应"紧急象限"。
		// 2.4.1 重要不紧急(2) -> 重要且紧急(1)
		// 2.4.2 不简单不重要(4) -> 简单不重要(3)
		switch current.Priority {
		case 2:
			current.Priority = 1
			pendingPromoteTaskIDs = append(pendingPromoteTaskIDs, current.ID)
		case 4:
			current.Priority = 3
			pendingPromoteTaskIDs = append(pendingPromoteTaskIDs, current.ID)
		default:
			// 2.4.3 其他优先级不处理（包含已经是 1/3 的情况）。
		}
	}
	return derived, pendingPromoteTaskIDs
}

// tryEnqueueTaskUrgencyPromote 尝试发布"任务紧急性平移请求"事件。
//
// 职责边界：
// 1. 负责 Redis 去重锁 + outbox 发布；
// 2. 不负责真正落库（由消费者负责）；
// 3. 发布失败时要释放本次抢到的去重锁，避免任务被长时间"误判已投递"。
func (ts *TaskService) tryEnqueueTaskUrgencyPromote(ctx context.Context, userID int, taskIDs []int) {
	// 1. 基础兜底：无发布器或无候选任务时直接返回。
	if ts.eventPublisher == nil || userID <= 0 || len(taskIDs) == 0 {
		return
	}

	// 2. 先做任务 ID 清洗，避免无效 ID 参与去重与发布。
	validTaskIDs := compactPositiveUniqueTaskIDs(taskIDs)
	if len(validTaskIDs) == 0 {
		return
	}

	// 3. 逐个抢 SETNX 去重锁：
	// 3.1 抢到锁才允许进入本次发布；
	// 3.2 抢不到说明已有请求在途，本次跳过即可；
	// 3.3 抢锁失败只记录日志，不中断主流程。
	lockedTaskIDs := make([]int, 0, len(validTaskIDs))
	lockedKeys := make([]string, 0, len(validTaskIDs))
	for _, taskID := range validTaskIDs {
		lockKey := fmt.Sprintf(taskUrgencyPromoteDedupeKeyFmt, userID, taskID)
		locked, lockErr := ts.cache.AcquireLock(ctx, lockKey, taskUrgencyPromoteDedupeTTL)
		if lockErr != nil {
			log.Printf("任务平移去重锁获取失败: user_id=%d task_id=%d err=%v", userID, taskID, lockErr)
			continue
		}
		if !locked {
			continue
		}
		lockedTaskIDs = append(lockedTaskIDs, taskID)
		lockedKeys = append(lockedKeys, lockKey)
	}
	if len(lockedTaskIDs) == 0 {
		return
	}

	// 4. 发布 outbox 事件：这里只保证"成功入 outbox 或返回错误"，不等待消费者执行完成。
	publishErr := eventsvc.PublishTaskUrgencyPromoteRequested(ctx, ts.eventPublisher, model.TaskUrgencyPromoteRequestedPayload{
		UserID:      userID,
		TaskIDs:     lockedTaskIDs,
		TriggeredAt: time.Now(),
	})
	if publishErr != nil {
		// 4.1 失败回滚：释放本次抢到的去重锁，避免后续请求因误锁而无法再投递。
		ts.releaseTaskPromoteLocks(lockedKeys)
		log.Printf("任务平移事件发布失败: user_id=%d task_ids=%v err=%v", userID, lockedTaskIDs, publishErr)
		return
	}

	log.Printf("任务平移事件已发布: user_id=%d task_ids=%v", userID, lockedTaskIDs)
}

// releaseTaskPromoteLocks 释放任务平移去重锁。
//
// 说明：
// 1. 仅用于"发布失败回滚"场景；
// 2. 使用 Background 避免请求上下文已取消时导致锁释放失败。
func (ts *TaskService) releaseTaskPromoteLocks(lockKeys []string) {
	if len(lockKeys) == 0 {
		return
	}
	releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for _, key := range lockKeys {
		if err := ts.cache.ReleaseLock(releaseCtx, key); err != nil {
			log.Printf("任务平移去重锁释放失败: key=%s err=%v", key, err)
		}
	}
}

// compactPositiveUniqueTaskIDs 对任务 ID 做"过滤非正数 + 去重"。
//
// 职责边界：
// 1. 只做参数清洗；
// 2. 不承载业务规则判断。
func compactPositiveUniqueTaskIDs(taskIDs []int) []int {
	seen := make(map[int]struct{}, len(taskIDs))
	result := make([]int, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		if taskID <= 0 {
			continue
		}
		if _, exists := seen[taskID]; exists {
			continue
		}
		seen[taskID] = struct{}{}
		result = append(result, taskID)
	}
	return result
}

// UpdateTask 更新用户指定任务的属性（部分更新）。
//
// 职责边界：
// 1. 负责参数校验：task_id 合法性、priority_group 范围；
// 2. 负责将请求 DTO 转换为 DAO 层的 updates map；
// 3. 空请求体（无字段需要更新）返回明确业务错误；
// 4. 不负责缓存删除（由 GORM cache_deleter 回调自动处理）。
func (ts *TaskService) UpdateTask(ctx context.Context, req *model.UserUpdateTaskRequest, userID int) (model.GetUserTaskResp, error) {
	// 1. 参数兜底。
	if req == nil || userID <= 0 || req.TaskID <= 0 {
		return model.GetUserTaskResp{}, respond.WrongTaskID
	}

	// 2. 构造 updates map：只有非 nil 的字段才写入。
	updates := make(map[string]interface{})
	if req.Title != nil {
		updates["title"] = *req.Title
	}
	if req.PriorityGroup != nil {
		// 2.1 优先级范围校验：当前任务体系只允许 1~4。
		if *req.PriorityGroup < 1 || *req.PriorityGroup > 4 {
			return model.GetUserTaskResp{}, respond.InvalidPriority
		}
		// 2.2 JSON 字段名是 priority_group，数据库列名是 priority。
		updates["priority"] = *req.PriorityGroup
	}
	if req.DeadlineAt != nil {
		updates["deadline_at"] = *req.DeadlineAt
	}
	if req.UrgencyThresholdAt != nil {
		updates["urgency_threshold_at"] = *req.UrgencyThresholdAt
	}

	// 3. 空更新检测：至少需要一个可更新字段。
	if len(updates) == 0 {
		return model.GetUserTaskResp{}, respond.TaskUpdateNoFields
	}

	// 4. 调用 DAO 执行更新。
	updatedTask, err := ts.dao.UpdateTaskByID(ctx, userID, req.TaskID, updates)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.GetUserTaskResp{}, respond.WrongTaskID
		}
		return model.GetUserTaskResp{}, err
	}

	// 5. 转换为响应 DTO。
	return conv.ModelToGetUserTaskResp(updatedTask), nil
}

// DeleteTask 永久删除用户指定任务。
//
// 职责边界：
// 1. 负责入参校验与业务错误映射；
// 2. 负责调用 DAO 执行硬删除；
// 3. 任务不存在时返回幂等信息码（TaskAlreadyDeleted）；
// 4. 不负责缓存删除（由 GORM cache_deleter 回调自动处理）。
func (ts *TaskService) DeleteTask(ctx context.Context, req *model.UserCompleteTaskRequest, userID int) (int, error) {
	// 1. 参数兜底。
	if req == nil || userID <= 0 || req.TaskID <= 0 {
		return 0, respond.WrongTaskID
	}

	// 2. 调用 DAO 执行删除。
	deletedTask, err := ts.dao.DeleteTaskByID(ctx, userID, req.TaskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 2.1 任务不存在或不属于当前用户：按幂等语义返回信息码。
			return 0, respond.TaskAlreadyDeleted
		}
		return 0, err
	}

	return deletedTask.ID, nil
}
