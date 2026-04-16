package repo

import (
	"context"
	"errors"
	"strings"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// ItemRepo 封装 memory_items 的数据访问。
//
// 职责边界：
// 1. 只负责表级读写，不承载注入、重排、审计决策；
// 2. 查询条件统一由 ItemQuery 表达，避免 service 层拼装 SQL；
// 3. 软删除、访问时间刷新等状态变更也收敛到这里。
type ItemRepo struct {
	db *gorm.DB
}

func NewItemRepo(db *gorm.DB) *ItemRepo {
	return &ItemRepo{db: db}
}

func (r *ItemRepo) WithTx(tx *gorm.DB) *ItemRepo {
	return &ItemRepo{db: tx}
}

// UpsertItems 批量写入记忆条目。
func (r *ItemRepo) UpsertItems(ctx context.Context, items []model.MemoryItem) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if len(items) == 0 {
		return nil
	}

	for i := range items {
		if err := r.db.WithContext(ctx).Create(&items[i]).Error; err != nil {
			return err
		}
	}
	return nil
}

// FindByQuery 按统一过滤条件读取记忆条目。
//
// 步骤化说明：
// 1. 先强制 user_id 过滤，避免跨用户串记忆；
// 2. 再按会话/助手/run 维度补充过滤，IncludeGlobal=true 时允许读取对应全局条目；
// 3. 最后补状态、类型、过期时间和 limit，返回稳定排序结果。
func (r *ItemRepo) FindByQuery(ctx context.Context, query memorymodel.ItemQuery) ([]model.MemoryItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("memory item repo is nil")
	}
	if query.UserID <= 0 {
		return nil, errors.New("memory item query user_id is invalid")
	}

	db := r.db.WithContext(ctx).Model(&model.MemoryItem{}).Where("user_id = ?", query.UserID)
	db = applyScopedEquality(db, "conversation_id", query.ConversationID, query.IncludeGlobal)
	db = applyScopedEquality(db, "assistant_id", query.AssistantID, query.IncludeGlobal)
	db = applyScopedEquality(db, "run_id", query.RunID, query.IncludeGlobal)

	if len(query.Statuses) > 0 {
		db = db.Where("status IN ?", query.Statuses)
	}
	if len(query.MemoryTypes) > 0 {
		db = db.Where("memory_type IN ?", query.MemoryTypes)
	}
	if query.OnlyUnexpired {
		now := query.Now
		if now.IsZero() {
			now = time.Now()
		}
		db = db.Where("(ttl_at IS NULL OR ttl_at > ?)", now)
	}
	if query.Limit > 0 {
		db = db.Limit(query.Limit)
	}

	var items []model.MemoryItem
	err := db.
		Order("is_explicit DESC").
		Order("importance DESC").
		Order("updated_at DESC").
		Find(&items).Error
	return items, err
}

// GetByIDForUser 读取某个用户的一条记忆条目。
func (r *ItemRepo) GetByIDForUser(ctx context.Context, userID int, memoryID int64) (*model.MemoryItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("memory item repo is nil")
	}
	if userID <= 0 || memoryID <= 0 {
		return nil, errors.New("memory item query params is invalid")
	}

	var item model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("id = ? AND user_id = ?", memoryID, userID).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// UpdateStatusByID 更新某条记忆的状态。
func (r *ItemRepo) UpdateStatusByID(ctx context.Context, userID int, memoryID int64, status string) error {
	return r.UpdateStatusByIDAt(ctx, userID, memoryID, status, time.Now())
}

// UpdateStatusByIDAt 更新某条记忆的状态，并允许上层显式指定更新时间。
//
// 这样做的原因：
// 1. 管理侧删除时，需要让“库内更新时间”和“审计 after 快照时间”保持一致；
// 2. 读取侧若只是刷新 last_access_at，不应该误改 updated_at；
// 3. 因此把“更新时间来源”收口到 repo，避免 service 层自己拼 SQL。
func (r *ItemRepo) UpdateStatusByIDAt(
	ctx context.Context,
	userID int,
	memoryID int64,
	status string,
	updatedAt time.Time,
) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if userID <= 0 || memoryID <= 0 {
		return errors.New("memory item update params is invalid")
	}

	status = strings.TrimSpace(status)
	if status == "" {
		return errors.New("memory item status is empty")
	}
	if updatedAt.IsZero() {
		updatedAt = time.Now()
	}

	return r.db.WithContext(ctx).
		Model(&model.MemoryItem{}).
		Where("id = ? AND user_id = ?", memoryID, userID).
		Updates(map[string]any{
			"status":     status,
			"updated_at": updatedAt,
		}).Error
}

// TouchLastAccessAt 批量刷新记忆访问时间。
//
// 说明：
// 1. 这里只更新 last_access_at，不更新 updated_at；
// 2. 因为 updated_at 代表“内容被修改”的时间，不能被一次普通读取污染；
// 3. 否则后续读取重排会把“最近被读过的旧记忆”误判成“最近被更新的记忆”。
func (r *ItemRepo) TouchLastAccessAt(ctx context.Context, ids []int64, accessedAt time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if len(ids) == 0 {
		return nil
	}
	if accessedAt.IsZero() {
		accessedAt = time.Now()
	}

	return r.db.WithContext(ctx).
		Model(&model.MemoryItem{}).
		Where("id IN ?", ids).
		Updates(map[string]any{
			"last_access_at": accessedAt,
		}).Error
}

// UpdateVectorStateByID 更新单条记忆的向量同步桥接状态。
//
// 说明：
// 1. 这里只更新 vector_status/vector_id，不更新 updated_at；
// 2. 因为向量同步属于索引层状态，不代表记忆内容本身被修改；
// 3. 若误改 updated_at，会污染读取侧的时间排序语义。
func (r *ItemRepo) UpdateVectorStateByID(
	ctx context.Context,
	memoryID int64,
	vectorStatus string,
	vectorID *string,
) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if memoryID <= 0 {
		return errors.New("memory item vector update id is invalid")
	}

	vectorStatus = strings.TrimSpace(vectorStatus)
	if vectorStatus == "" {
		return errors.New("memory item vector status is empty")
	}

	return r.db.WithContext(ctx).
		Model(&model.MemoryItem{}).
		Where("id = ?", memoryID).
		UpdateColumns(map[string]any{
			"vector_status": vectorStatus,
			"vector_id":     vectorID,
		}).Error
}

// FindActiveByHash 按用户和内容哈希精确查找活跃记忆。
//
// 用途：
// 1. 决策层 Step 1 的 Hash 精确命中检查；
// 2. 利用 idx_memory_items_user_type_hash 联合索引，避免全表扫描；
// 3. 只返回 status=active 的记录，软删除记录不参与去重。
func (r *ItemRepo) FindActiveByHash(ctx context.Context, userID int, contentHash string) ([]model.MemoryItem, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("memory item repo is nil")
	}
	if userID <= 0 || strings.TrimSpace(contentHash) == "" {
		return nil, errors.New("memory item find by hash params is invalid")
	}

	var items []model.MemoryItem
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND content_hash = ? AND status = ?", userID, contentHash, model.MemoryItemStatusActive).
		Find(&items).Error
	return items, err
}

// UpdateContentByID 更新指定记忆的内容相关字段。
//
// 步骤化说明：
// 1. 只改 title/content/normalized_content/content_hash/confidence/importance 六个字段；
// 2. 不改 status/user_id/memory_type 等身份字段，保证更新操作不改变记忆归属；
// 3. updated_at 由 GORM AutoUpdateTime 自动维护。
func (r *ItemRepo) UpdateContentByID(ctx context.Context, memoryID int64, fields memorymodel.UpdateContentFields) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if memoryID <= 0 {
		return errors.New("memory item update content id is invalid")
	}

	return r.db.WithContext(ctx).
		Model(&model.MemoryItem{}).
		Where("id = ?", memoryID).
		Updates(map[string]any{
			"title":              fields.Title,
			"content":            fields.Content,
			"normalized_content": fields.NormalizedContent,
			"content_hash":       fields.ContentHash,
			"confidence":         fields.Confidence,
			"importance":         fields.Importance,
		}).Error
}

// SoftDeleteByID 软删除指定用户的某条记忆。
//
// 说明：
// 1. 复用 UpdateStatusByIDAt 的逻辑模式，把 status 改为 deleted；
// 2. 同时把 vector_status 重置为 pending，确保向量侧也能感知删除；
// 3. 必须带 user_id 条件，避免跨用户误删。
func (r *ItemRepo) SoftDeleteByID(ctx context.Context, userID int, memoryID int64) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if userID <= 0 || memoryID <= 0 {
		return errors.New("memory item soft delete params is invalid")
	}

	return r.db.WithContext(ctx).
		Model(&model.MemoryItem{}).
		Where("id = ? AND user_id = ?", memoryID, userID).
		Updates(map[string]any{
			"status":        model.MemoryItemStatusDeleted,
			"vector_status": "pending",
			"updated_at":    time.Now(),
		}).Error
}

func applyScopedEquality(db *gorm.DB, column, value string, includeGlobal bool) *gorm.DB {
	value = strings.TrimSpace(value)
	if value == "" {
		return db
	}
	if includeGlobal {
		return db.Where("("+column+" = ? OR "+column+" IS NULL)", value)
	}
	return db.Where(column+" = ?", value)
}
