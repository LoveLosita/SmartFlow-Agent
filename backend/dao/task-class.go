package dao

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"gorm.io/gorm"
)

type TaskClassDAO struct {
	// 这是一个口袋，用来装数据库连接实例
	db *gorm.DB
}

// NewTaskClassDAO 创建TaskClassDAO实例
// NewTaskClassDAO 接收一个 *gorm.DB，并把它塞进结构体的口袋里
func NewTaskClassDAO(db *gorm.DB) *TaskClassDAO {
	return &TaskClassDAO{
		db: db,
	}
}

// AddOrUpdateTaskClass 为指定用户添加/更新任务类（防越权：更新时限定 user_id）
func (dao *TaskClassDAO) AddOrUpdateTaskClass(userID int, taskClass *model.TaskClass) (int, error) {
	// 不信任入参里的 UserID，强制使用当前登录用户
	taskClass.UserID = &userID

	// 新增：ID == 0 直接插入
	if taskClass.ID == 0 {
		if err := dao.db.Create(taskClass).Error; err != nil {
			return 0, err
		}
		return taskClass.ID, nil
	}
	// 更新：必须同时匹配 id + user_id，否则不会更新任何行（避免覆盖他人数据）
	tx := dao.db.Model(&model.TaskClass{}).
		Where("id = ? AND user_id = ?", taskClass.ID, userID).
		Updates(taskClass)
	if tx.Error != nil {
		return 0, tx.Error
	}
	if tx.RowsAffected == 0 {
		// 未匹配到记录：要么不存在，要么不属于该用户
		return 0, respond.UserTaskClassForbidden
	}
	return taskClass.ID, nil
}

func (dao *TaskClassDAO) AddOrUpdateTaskClassItems(userID int, items []model.TaskClassItem) error {
	if len(items) == 0 {
		return nil
	}

	// 1) 校验这些 items 关联的 task_class（category_id）都属于当前用户
	categoryIDSet := make(map[int]struct{}, len(items))
	var categoryIDs []int
	for _, it := range items {
		if *it.CategoryID == 0 {
			return gorm.ErrRecordNotFound
		}
		if _, ok := categoryIDSet[*it.CategoryID]; !ok {
			categoryIDSet[*it.CategoryID] = struct{}{}
			categoryIDs = append(categoryIDs, *it.CategoryID)
		}
	}

	var count int64
	if err := dao.db.Model(&model.TaskClass{}).
		Where("id IN ? AND user_id = ?", categoryIDs, userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(categoryIDs)) {
		return respond.UserTaskClassForbidden
	}

	// 2) 新增与更新分开处理：新增不受影响；更新时限定 category_id（防越权）
	var toCreate []model.TaskClassItem
	for _, it := range items {
		if it.ID == 0 {
			toCreate = append(toCreate, it)
			continue
		}

		tx := dao.db.Model(&model.TaskClassItem{}).
			Where("id = ? AND category_id IN ?", it.ID, categoryIDs).
			Updates(map[string]any{
				"category_id": it.CategoryID,
			})
		if tx.Error != nil {
			return tx.Error
		}
		if tx.RowsAffected == 0 {
			return respond.UserTaskClassForbidden
		}
	}

	if len(toCreate) > 0 {
		if err := dao.db.Create(&toCreate).Error; err != nil {
			return err
		}
	}
	return nil
}

// Transaction 在一个事务中执行传入的函数，供 service 层复用（自动提交/回滚）
// 规则：fn 返回 nil -> commit；fn 返回 error 或 panic -> rollback
func (dao *TaskClassDAO) Transaction(fn func(txDAO *TaskClassDAO) error) error {
	return dao.db.Transaction(func(tx *gorm.DB) error {
		return fn(NewTaskClassDAO(tx))
	})
}

func (dao *TaskClassDAO) GetUserTaskClasses(userID int) ([]model.TaskClass, error) {
	var taskClasses []model.TaskClass
	err := dao.db.Where("user_id = ?", userID).Find(&taskClasses).Error
	if err != nil {
		return nil, err
	}
	return taskClasses, nil
}

// GetCompleteTaskClassByID 带着 ID 和 UserID 去取，防越权
func (dao *TaskClassDAO) GetCompleteTaskClassByID(ctx context.Context, id int, userID int) (*model.TaskClass, error) {
	var taskClass model.TaskClass

	// 1. 使用 Preload("Items") 自动执行两条 SQL 并组装
	// SQL A: SELECT * FROM task_classes WHERE id = ? AND user_id = ?
	// SQL B: SELECT * FROM task_class_items WHERE category_id = (SQL A 的 ID)
	err := dao.db.WithContext(ctx).
		Preload("Items").
		Where("id = ? AND user_id = ?", id, userID).
		First(&taskClass).Error

	if err != nil {
		return nil, err
	}
	return &taskClass, nil
}

func (dao *TaskClassDAO) GetTaskClassItemByID(ctx context.Context, id int) (*model.TaskClassItem, error) {
	var item model.TaskClassItem
	err := dao.db.WithContext(ctx).
		Where("id = ?", id).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (dao *TaskClassDAO) GetTaskClassIDByTaskItemID(ctx context.Context, itemID int) (int, error) {
	var item model.TaskClassItem
	err := dao.db.WithContext(ctx).
		Select("category_id").
		Where("id = ?", itemID).
		First(&item).Error
	if err != nil {
		return 0, err
	}
	return *item.CategoryID, nil
}

func (dao *TaskClassDAO) GetTaskClassUserIDByID(ctx context.Context, taskClassID int) (int, error) {
	var taskClass model.TaskClass
	err := dao.db.WithContext(ctx).
		Select("user_id").
		Where("id = ?", taskClassID).
		First(&taskClass).Error
	if err != nil {
		return 0, err
	}
	return *taskClass.UserID, nil
}

func (dao *TaskClassDAO) UpdateTaskClassItemEmbeddedTime(ctx context.Context, taskID int, embeddedTime *model.TargetTime) error {
	err := dao.db.WithContext(ctx).
		Model(&model.TaskClassItem{}).
		Where("id = ?", taskID).
		Update("embedded_time", embeddedTime).Error
	return err
}
