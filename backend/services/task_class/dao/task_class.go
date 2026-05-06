package dao

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
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

func (dao *TaskClassDAO) WithTx(tx *gorm.DB) *TaskClassDAO {
	return &TaskClassDAO{
		db: tx,
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

	// 1. 先显式校验任务类归属，避免“更新值与库内完全相同”时 RowsAffected=0 被误判成越权。
	// 2. 这里只负责校验 id/user_id 是否匹配，不负责判断具体字段有没有变化。
	// 3. 若确实不存在或不属于当前用户，统一返回 UserTaskClassForbidden。
	if err := dao.ensureTaskClassOwned(userID, taskClass.ID); err != nil {
		return 0, err
	}

	// 1. 更新语义是“前端提交什么，就以什么覆盖数据库当前值”。
	// 2. 因此这里不能再直接用 struct Updates，否则 nil/空切片 等零值字段会被 GORM 跳过，刷新后看起来像“没更新”。
	// 3. 统一改成显式字段映射，保证可选字段清空、排除数组清空都能真正落库。
	tx := dao.db.Model(&model.TaskClass{UserID: &userID}).
		Where("id = ? AND user_id = ?", taskClass.ID, userID).
		Updates(buildTaskClassUpdateMap(taskClass))
	if tx.Error != nil {
		return 0, tx.Error
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

	// 2. 收集本次要更新的已有 item，先做一次归属校验。
	// 2.1 这里单独校验是为了避免“只更新成原值”时 RowsAffected=0 被误判为越权。
	// 2.2 校验通过后，后面的 UPDATE 只关心数据库错误，不再用 RowsAffected 判权限。
	// 2.3 若请求里混入了不属于这些 task_class 的 item_id，统一返回 UserTaskClassForbidden。
	existingItemIDs := make([]int, 0, len(items))
	existingItemIDSet := make(map[int]struct{}, len(items))
	for _, it := range items {
		if it.ID == 0 {
			continue
		}
		if _, exists := existingItemIDSet[it.ID]; exists {
			continue
		}
		existingItemIDSet[it.ID] = struct{}{}
		existingItemIDs = append(existingItemIDs, it.ID)
	}
	if err := dao.ensureTaskClassItemsOwnedByCategories(existingItemIDs, categoryIDs); err != nil {
		return err
	}

	// 3) 新增与更新分开处理：新增直接插入；已有 item 按现有契约更新可编辑字段。
	var toCreate []model.TaskClassItem
	for _, it := range items {
		if it.ID == 0 {
			toCreate = append(toCreate, it)
			continue
		}

		tx := dao.db.Model(&model.TaskClassItem{}).
			Where("id = ? AND category_id IN ?", it.ID, categoryIDs).
			Updates(map[string]any{
				"category_id":   it.CategoryID,
				"order":         it.Order,
				"content":       it.Content,
				"embedded_time": it.EmbeddedTime,
			})
		if tx.Error != nil {
			return tx.Error
		}
	}

	if len(toCreate) > 0 {
		if err := dao.db.Create(&toCreate).Error; err != nil {
			return err
		}
	}
	return nil
}

// ensureTaskClassOwned 只负责校验 task_class 是否属于当前用户。
func (dao *TaskClassDAO) ensureTaskClassOwned(userID int, taskClassID int) error {
	var count int64
	if err := dao.db.Model(&model.TaskClass{}).
		Where("id = ? AND user_id = ?", taskClassID, userID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return respond.UserTaskClassForbidden
	}
	return nil
}

// ensureTaskClassItemsOwnedByCategories 只负责校验一批 item 是否都挂在允许的 task_class 下。
func (dao *TaskClassDAO) ensureTaskClassItemsOwnedByCategories(itemIDs []int, categoryIDs []int) error {
	if len(itemIDs) == 0 {
		return nil
	}

	var count int64
	if err := dao.db.Model(&model.TaskClassItem{}).
		Where("id IN ? AND category_id IN ?", itemIDs, categoryIDs).
		Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(itemIDs)) {
		return respond.UserTaskClassForbidden
	}
	return nil
}

// buildTaskClassUpdateMap 负责把“全量更新”请求转换成显式列更新。
func buildTaskClassUpdateMap(taskClass *model.TaskClass) map[string]any {
	return map[string]any{
		"name":                  nullableStringValue(taskClass.Name),
		"mode":                  nullableStringValue(taskClass.Mode),
		"start_date":            nullableTimeValue(taskClass.StartDate),
		"end_date":              nullableTimeValue(taskClass.EndDate),
		"subject_type":          nullableStringValue(taskClass.SubjectType),
		"difficulty_level":      nullableStringValue(taskClass.DifficultyLevel),
		"cognitive_intensity":   nullableStringValue(taskClass.CognitiveIntensity),
		"total_slots":           nullableIntValue(taskClass.TotalSlots),
		"allow_filler_course":   nullableBoolValue(taskClass.AllowFillerCourse),
		"strategy":              nullableStringValue(taskClass.Strategy),
		"excluded_slots":        taskClass.ExcludedSlots,
		"excluded_days_of_week": taskClass.ExcludedDaysOfWeek,
	}
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableIntValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableBoolValue(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTimeValue(value *time.Time) any {
	if value == nil {
		return nil
	}
	return *value
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

// GetCompleteTaskClassesByIDs 批量获取“完整任务类”（含 Items）。
//
// 职责边界：
// 1. 负责按 user_id + ids 过滤，保证数据归属安全；
// 2. 负责预加载 Items，供智能粗排直接使用；
// 3. 不负责排序策略，返回结果顺序由 service 层决定；
// 4. 若存在任一 id 不存在或不属于该用户，返回 WrongTaskClassID。
func (dao *TaskClassDAO) GetCompleteTaskClassesByIDs(ctx context.Context, userID int, ids []int) ([]model.TaskClass, error) {
	if len(ids) == 0 {
		return []model.TaskClass{}, nil
	}

	// 1. 先做去重与合法值过滤，避免无效 ID 放大数据库压力。
	uniqueIDs := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		uniqueIDs = append(uniqueIDs, id)
	}
	if len(uniqueIDs) == 0 {
		return nil, respond.WrongTaskClassID
	}

	// 2. 批量查询并预加载任务项。
	var taskClasses []model.TaskClass
	err := dao.db.WithContext(ctx).
		Preload("Items").
		Where("user_id = ? AND id IN ?", userID, uniqueIDs).
		Find(&taskClasses).Error
	if err != nil {
		return nil, err
	}

	// 3. 数量校验：少一条都视为“存在非法/越权 ID”，统一按业务错误返回。
	if len(taskClasses) != len(uniqueIDs) {
		return nil, respond.WrongTaskClassID
	}
	return taskClasses, nil
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
	res := dao.db.WithContext(ctx).
		Select("category_id").
		Where("id = ?", itemID).
		First(&item)
	if res.Error != nil {
		if errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return 0, respond.TaskClassItemNotFound
		}
		return 0, res.Error
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

func (dao *TaskClassDAO) DeleteTaskClassItemEmbeddedTime(ctx context.Context, taskID int) error {
	err := dao.db.WithContext(ctx).
		Model(&model.TaskClassItem{}).
		Where("id = ?", taskID).
		Update("embedded_time", nil).Error
	return err
}

func (dao *TaskClassDAO) IfTaskClassItemArranged(ctx context.Context, taskID int) (bool, error) {
	var item model.TaskClassItem
	err := dao.db.WithContext(ctx).
		Select("embedded_time").
		Where("id = ?", taskID).
		First(&item).Error
	if err != nil {
		return false, err
	}
	return item.EmbeddedTime != nil, nil
}

func (dao *TaskClassDAO) BatchCheckIfTaskClassItemsArranged(ctx context.Context, itemIDs []int) (bool, error) {
	if len(itemIDs) == 0 {
		return false, nil
	}
	var count int64
	err := dao.db.WithContext(ctx).
		Model(&model.TaskClassItem{}).
		Where("id IN ? AND embedded_time IS NOT NULL", itemIDs).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (dao *TaskClassDAO) DeleteTaskClassItemByID(ctx context.Context, id int) error {
	err := dao.db.WithContext(ctx).
		Where("id = ?", id).
		Delete(&model.TaskClassItem{}).Error
	return err
}

func (dao *TaskClassDAO) DeleteTaskClassByID(ctx context.Context, id int, userID int) error {
	// 1. 删除时显式把 user_id 挂到 Model 上，供 GORM 缓存失效插件读取。
	// 2. 业务层已经完成归属校验，这里仍带上 user_id 条件，避免极端并发下误删其它用户数据。
	// 3. 若仍存在 task_items 外键依赖，GORM 会返回数据库错误并回滚，本函数不吞掉该错误。
	res := dao.db.WithContext(ctx).
		Model(&model.TaskClass{UserID: &userID}).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.TaskClass{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return respond.WrongTaskClassID
	}
	return nil
}

func (dao *TaskClassDAO) BatchUpdateTaskClassItemEmbeddedTime(ctx context.Context, itemIDs []int, updates []*model.TargetTime) error {
	if len(itemIDs) == 0 {
		return nil
	}
	if len(itemIDs) != len(updates) {
		return errors.New("itemIDs length mismatch updates length")
	}

	// 单条 SQL 批量更新：UPDATE ... SET embedded_time = CASE id WHEN ? THEN ? ... END WHERE id IN (?)
	caseSQL := "CASE id"
	args := make([]any, 0, len(itemIDs)*2)
	for i, id := range itemIDs {
		caseSQL += " WHEN ? THEN ?"
		args = append(args, id, updates[i])
	}
	caseSQL += " END"

	res := dao.db.WithContext(ctx).
		Model(&model.TaskClassItem{}).
		Where("id IN ?", itemIDs).
		Update("embedded_time", gorm.Expr(caseSQL, args...))

	return res.Error
}

func (dao *TaskClassDAO) ValidateTaskItemIDsBelongToTaskClass(ctx context.Context, taskClassID int, itemIDs []int) (bool, error) {
	if len(itemIDs) == 0 {
		return true, nil
	}

	var count int64
	err := dao.db.WithContext(ctx).
		Model(&model.TaskClassItem{}).
		Where("id IN ? AND category_id = ?", itemIDs, taskClassID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count == int64(len(itemIDs)), nil
}

func (dao *TaskClassDAO) GetTaskClassItemsByIDs(ctx context.Context, itemIDs []int) ([]model.TaskClassItem, error) {
	var items []model.TaskClassItem
	err := dao.db.WithContext(ctx).
		Where("id IN ?", itemIDs).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}
