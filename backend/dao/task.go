package dao

import (
	"context"
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

// PromoteTaskUrgencyByIDs 批量执行“任务紧急性平移”。
//
// 职责边界：
//  1. 只负责把满足条件的任务从“不紧急象限”平移到“紧急象限”：
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

	// 3. 条件更新：只更新“已到紧急分界线且仍处于非紧急象限”的任务。
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

// compactPositiveIntIDs 对 int 切片做“去重 + 过滤非正数”。
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
