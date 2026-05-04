package dao

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"gorm.io/gorm"
)

type TaskDAO struct {
	// 这是一个口袋，用来装数据库连接实例
	db *gorm.DB
}

// NewTaskDAO 创建TaskDAO实例
// NewTaskDAO 接收一个 *gorm.DB，并把它塞进结构体的口袋里
func NewTaskDAO(db *gorm.DB) *TaskDAO {
	return &TaskDAO{
		db: db,
	}
}

func (r *TaskDAO) WithTx(tx *gorm.DB) *TaskDAO {
	return &TaskDAO{db: tx}
}

// AddTask 为指定用户添加任务
func (dao *TaskDAO) AddTask(req *model.Task) (*model.Task, error) {
	if err := dao.db.Create(req).Error; err != nil {
		return nil, err
	}
	return req, nil
}

func (dao *TaskDAO) GetTasksByUserID(userID int) ([]model.Task, error) {
	var tasks []model.Task
	if err := dao.db.Where("user_id = ?", userID).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 { // 如果没有任务，返回自定义错误
		return nil, respond.UserTasksEmpty
	}
	return tasks, nil
}

// GetTaskByUserAndID 读取当前用户拥有的单个任务快照。
//
// 职责边界：
// 1. 只按 user_id + task_id 做所有权限定查询；
// 2. 不做主动调度事实转换，也不处理 found=false 语义；
// 3. gorm.ErrRecordNotFound 由调用方按业务场景映射。
func (dao *TaskDAO) GetTaskByUserAndID(ctx context.Context, userID int, taskID int) (*model.Task, error) {
	if userID <= 0 || taskID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var task model.Task
	if err := dao.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// CompleteTaskByID 将指定任务标记为"已完成"。
//
// 职责边界：
// 1. 只负责"当前用户 + 指定 task_id"的完成状态更新；
// 2. 不负责幂等中间件（由路由层统一挂载）；
// 3. 不负责业务层响应包装（由 Service 层处理）。
//
// 返回语义：
//  1. 第一个返回值 *model.Task：返回更新后的任务快照（至少含 ID/UserID/IsCompleted）；
//  2. 第二个返回值 bool：
//     2.1 true：任务原本就已完成，本次属于幂等命中；
//     2.2 false：本次从未完成成功更新为已完成；
//  3. error：
//     3.1 gorm.ErrRecordNotFound：任务不存在或不属于当前用户；
//     3.2 其他 error：数据库异常。
func (dao *TaskDAO) CompleteTaskByID(ctx context.Context, userID int, taskID int) (*model.Task, bool, error) {
	// 1. 基础兜底：非法参数直接返回"记录不存在"语义，避免下游误写。
	if userID <= 0 || taskID <= 0 {
		return nil, false, gorm.ErrRecordNotFound
	}

	// 2. 先查询目标任务，明确区分"已完成"与"不存在"。
	var target model.Task
	findErr := dao.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&target).Error
	if findErr != nil {
		return nil, false, findErr
	}

	// 3. 若任务已完成，直接按幂等成功返回，不再写库。
	if target.IsCompleted {
		return &target, true, nil
	}

	// 4. 若任务未完成，执行状态更新。
	//
	// 4.1 使用 Model(&model.Task{UserID:userID}) 的目的：
	//     让 cache_deleter 在 GORM Update 回调里拿到 user_id，从而正确删除任务缓存。
	// 4.2 更新条件继续限定 user_id + id，避免误更新其他用户数据。
	updateResult := dao.db.WithContext(ctx).
		Model(&model.Task{UserID: userID}).
		Where("id = ? AND user_id = ?", taskID, userID).
		Update("is_completed", true)
	if updateResult.Error != nil {
		return nil, false, updateResult.Error
	}

	// 5. 极端并发兜底：
	// 5.1 若 RowsAffected=0，可能是并发请求已先一步更新；
	// 5.2 此时二次读取任务状态，若已完成则按幂等成功返回，否则视为不存在/异常。
	if updateResult.RowsAffected == 0 {
		var check model.Task
		checkErr := dao.db.WithContext(ctx).
			Where("id = ? AND user_id = ?", taskID, userID).
			First(&check).Error
		if checkErr != nil {
			return nil, false, checkErr
		}
		if check.IsCompleted {
			return &check, true, nil
		}
		return nil, false, errors.New("任务状态更新失败")
	}

	// 6. 返回更新后的快照给 Service 层组装响应。
	target.IsCompleted = true
	return &target, false, nil
}

// UndoCompleteTaskByID 将指定任务从"已完成"恢复为"未完成"。
//
// 职责边界：
// 1. 只负责当前用户(user_id)下指定 task_id 的状态恢复；
// 2. 若任务本就未完成，按业务要求返回明确错误，不做幂等成功；
// 3. 不负责响应文案拼装（由 Service 层处理）。
//
// 返回语义：
//  1. *model.Task：恢复后的任务快照；
//  2. error：
//     2.1 gorm.ErrRecordNotFound：任务不存在或不属于当前用户；
//     2.2 respond.TaskNotCompleted：任务当前不是"已完成"状态，不能执行取消勾选；
//     2.3 其他 error：数据库异常。
func (dao *TaskDAO) UndoCompleteTaskByID(ctx context.Context, userID int, taskID int) (*model.Task, error) {
	// 1. 参数兜底：非法 user/task 参数统一按"记录不存在"处理，避免误写。
	if userID <= 0 || taskID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}

	// 2. 先读取目标任务，明确区分"不存在"和"状态不允许恢复"。
	var target model.Task
	findErr := dao.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&target).Error
	if findErr != nil {
		return nil, findErr
	}

	// 3. 严格业务约束：若任务当前未完成，直接返回业务错误。
	// 3.1 这是本接口和"标记完成"接口的关键差异：这里不做幂等成功。
	if !target.IsCompleted {
		return nil, respond.TaskNotCompleted
	}

	// 4. 执行状态恢复（is_completed=true -> false）。
	//
	// 4.1 使用 Model(&model.Task{UserID:userID}) 的目的是让 cache_deleter 拿到 user_id，
	//     从而在回调中正确删除该用户任务缓存。
	updateResult := dao.db.WithContext(ctx).
		Model(&model.Task{UserID: userID}).
		Where("id = ? AND user_id = ?", taskID, userID).
		Update("is_completed", false)
	if updateResult.Error != nil {
		return nil, updateResult.Error
	}

	// 5. 并发兜底：
	// 5.1 若 RowsAffected=0，说明可能被并发请求先一步恢复；
	// 5.2 重新读取当前状态，若已是未完成则按业务规则返回"任务未完成"错误。
	if updateResult.RowsAffected == 0 {
		var check model.Task
		checkErr := dao.db.WithContext(ctx).
			Where("id = ? AND user_id = ?", taskID, userID).
			First(&check).Error
		if checkErr != nil {
			return nil, checkErr
		}
		if !check.IsCompleted {
			return nil, respond.TaskNotCompleted
		}
		return nil, errors.New("取消任务完成状态失败")
	}

	// 6. 回填恢复后状态并返回。
	target.IsCompleted = false
	return &target, nil
}

// PromoteTaskUrgencyByIDs 批量执行"任务紧急性平移"。
//
// 职责边界：
//  1. 只负责把满足条件的任务从"不紧急象限"平移到"紧急象限"：
//     1.1 priority=2 -> 1（重要不紧急 -> 重要且紧急）；
//     1.2 priority=4 -> 3（不简单不重要 -> 简单不重要）；
//  2. 只更新本次指定 user_id + task_ids 范围内的数据；
//  3. 不负责事件发布、重试去重和缓存策略（由 Service/Outbox 负责）。
//
// 幂等与一致性说明：
// 1. SQL 条件会限制 `is_completed=0`、`urgency_threshold_at<=now`、`priority IN (2,4)`；
// 2. 同一批任务重复调用时，已经平移过的记录不会再次更新（幂等）；
// 3. 使用 `Model(&model.Task{UserID:userID})` 是为了让 GORM 回调拿到 user_id，从而触发 cache_deleter 删除任务缓存。
func (dao *TaskDAO) PromoteTaskUrgencyByIDs(ctx context.Context, userID int, taskIDs []int, now time.Time) (int64, error) {
	// 1. 基础兜底：非法 user 或空任务列表直接无操作返回。
	if userID <= 0 || len(taskIDs) == 0 {
		return 0, nil
	}

	// 2. 去重并过滤非正数 ID，避免无效 where in 条件放大 SQL 噪音。
	validTaskIDs := compactPositiveIntIDs(taskIDs)
	if len(validTaskIDs) == 0 {
		return 0, nil
	}

	// 3. 条件更新：只更新"已到紧急分界线且仍处于非紧急象限"的任务。
	result := dao.db.WithContext(ctx).
		Model(&model.Task{UserID: userID}).
		Where("user_id = ?", userID).
		Where("id IN ?", validTaskIDs).
		Where("is_completed = ?", false).
		Where("urgency_threshold_at IS NOT NULL AND urgency_threshold_at <= ?", now).
		Where("priority IN ?", []int{2, 4}).
		Update("priority", gorm.Expr("CASE WHEN priority = 2 THEN 1 WHEN priority = 4 THEN 3 ELSE priority END"))

	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// UpdateTaskByID 按 task_id + user_id 更新指定字段。
//
// 职责边界：
// 1. 只负责按 updates map 执行 SET 子句更新；
// 2. 不负责业务规则（如优先级范围校验），由 Service 层处理；
// 3. 使用 Model(&model.Task{UserID: userID}) 让 cache_deleter 回调拿到 user_id。
//
// 返回语义：
//  1. *model.Task：更新后的完整任务快照；
//  2. error：
//     2.1 gorm.ErrRecordNotFound：任务不存在或不属于当前用户；
//     2.2 其他 error：数据库异常。
func (dao *TaskDAO) UpdateTaskByID(ctx context.Context, userID int, taskID int, updates map[string]interface{}) (*model.Task, error) {
	// 1. 参数兜底：非法参数直接返回"记录不存在"语义。
	if userID <= 0 || taskID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}

	// 2. 先查询目标任务，确认存在且归属当前用户。
	var target model.Task
	findErr := dao.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&target).Error
	if findErr != nil {
		return nil, findErr
	}

	// 3. 执行部分字段更新。
	// 3.1 使用 Model(&model.Task{UserID: userID}) 触发 cache_deleter。
	// 3.2 限定 id + user_id 条件，避免误更新。
	updateResult := dao.db.WithContext(ctx).
		Model(&model.Task{UserID: userID}).
		Where("id = ? AND user_id = ?", taskID, userID).
		Updates(updates)
	if updateResult.Error != nil {
		return nil, updateResult.Error
	}

	// 4. 更新后重新读取，保证返回完整且一致的快照。
	var updated model.Task
	if err := dao.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&updated).Error; err != nil {
		return nil, err
	}

	return &updated, nil
}

// DeleteTaskByID 永久删除指定任务（硬删除）。
//
// 职责边界：
// 1. 只负责删除 user_id + task_id 对应的记录；
// 2. 使用 Model(&model.Task{UserID: userID}) 触发 cache_deleter 删除用户任务缓存；
// 3. 不负责级联清理日程（tasks 与 schedule_events 无直接外键关联）。
//
// 返回语义：
//  1. *model.Task：被删除的任务快照（用于响应前端）；
//  2. error：
//     2.1 gorm.ErrRecordNotFound：任务不存在或不属于当前用户；
//     2.2 其他 error：数据库异常。
func (dao *TaskDAO) DeleteTaskByID(ctx context.Context, userID int, taskID int) (*model.Task, error) {
	// 1. 参数兜底。
	if userID <= 0 || taskID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}

	// 2. 先查询目标任务，确认存在且归属当前用户，同时获取快照用于响应。
	var target model.Task
	findErr := dao.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", taskID, userID).
		First(&target).Error
	if findErr != nil {
		return nil, findErr
	}

	// 3. 执行硬删除。
	// 3.1 使用 Model(&model.Task{UserID: userID}) 触发 cache_deleter。
	deleteResult := dao.db.WithContext(ctx).
		Model(&model.Task{UserID: userID}).
		Where("id = ? AND user_id = ?", taskID, userID).
		Delete(&model.Task{})
	if deleteResult.Error != nil {
		return nil, deleteResult.Error
	}

	// 4. 并发兜底：RowsAffected=0 说明被并发请求先一步删除。
	if deleteResult.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	return &target, nil
}

// compactPositiveIntIDs 对 int 切片做"去重 + 过滤非正数"。
//
// 说明：
// 1. 该函数是 DAO 内部参数清洗工具，不参与任何业务判定；
// 2. 返回结果不保证稳定顺序，对当前 SQL where in 场景无影响。
func compactPositiveIntIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
