package dao

import (
	"context"
	"errors"
	"strings"
	"time"

	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ForumDAO 承载计划广场私有表的持久化访问。
//
// 职责边界：
// 1. 只访问 forum_* 表，不直接读写旧 task_classes / task_items；
// 2. 只做查询、事务和基础状态更新，不组装前端 DTO；
// 3. 业务规则由 sv 层控制，DAO 仅提供必要的数据原子操作。
type ForumDAO struct {
	db *gorm.DB
}

func NewForumDAO(db *gorm.DB) *ForumDAO {
	return &ForumDAO{db: db}
}

func (dao *ForumDAO) WithTx(tx *gorm.DB) *ForumDAO {
	return &ForumDAO{db: tx}
}

// Transaction 在一个数据库事务内执行计划广场写操作。
func (dao *ForumDAO) Transaction(ctx context.Context, fn func(txDAO *ForumDAO) error) error {
	return dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(dao.WithTx(tx))
	})
}

type ListPostsQuery struct {
	Page     int
	PageSize int
	Sort     string
	Keyword  string
	Tag      string
}

// CreatePostSnapshot 在同一事务中写帖子、模板和模板条目。
func (dao *ForumDAO) CreatePostSnapshot(ctx context.Context, post *forummodel.ForumPost, template *forummodel.ForumPostTemplate, items []forummodel.ForumPostTemplateItem) error {
	return dao.Transaction(ctx, func(txDAO *ForumDAO) error {
		if err := txDAO.db.Create(post).Error; err != nil {
			return err
		}
		template.PostID = post.ID
		if err := txDAO.db.Create(template).Error; err != nil {
			return err
		}
		for i := range items {
			items[i].PostID = post.ID
			items[i].TemplateID = template.ID
		}
		if len(items) > 0 {
			if err := txDAO.db.Create(&items).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (dao *ForumDAO) FindPostByIdempotencyKey(ctx context.Context, userID uint64, key string) (*forummodel.ForumPost, error) {
	var post forummodel.ForumPost
	err := dao.db.WithContext(ctx).
		Where("author_user_id = ? AND idempotency_key = ?", userID, key).
		First(&post).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (dao *ForumDAO) ListPosts(ctx context.Context, query ListPostsQuery) ([]forummodel.ForumPost, int64, error) {
	db := dao.db.WithContext(ctx).
		Model(&forummodel.ForumPost{}).
		Where("status = ? AND deleted_at IS NULL", forummodel.ForumPostStatusPublished)
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		db = db.Where("title LIKE ? OR summary LIKE ?", like, like)
	}
	if tag := strings.TrimSpace(query.Tag); tag != "" {
		db = db.Where("JSON_CONTAINS(tags_json, JSON_QUOTE(?))", tag)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	orderBy := "created_at DESC"
	switch strings.TrimSpace(query.Sort) {
	case "likes":
		orderBy = "like_count DESC, created_at DESC"
	case "imports":
		orderBy = "import_count DESC, created_at DESC"
	}

	var posts []forummodel.ForumPost
	err := db.Order(orderBy).
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&posts).Error
	if err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}

func (dao *ForumDAO) ListPublishedTagJSONs(ctx context.Context) ([]string, error) {
	var rows []struct {
		TagsJSON string
	}
	err := dao.db.WithContext(ctx).
		Model(&forummodel.ForumPost{}).
		Select("tags_json").
		Where("status = ? AND deleted_at IS NULL", forummodel.ForumPostStatusPublished).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(rows))
	for _, row := range rows {
		result = append(result, row.TagsJSON)
	}
	return result, nil
}

func (dao *ForumDAO) FindPublishedPost(ctx context.Context, postID uint64) (*forummodel.ForumPost, error) {
	var post forummodel.ForumPost
	err := dao.db.WithContext(ctx).
		Where("id = ? AND status = ? AND deleted_at IS NULL", postID, forummodel.ForumPostStatusPublished).
		First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (dao *ForumDAO) LockPublishedPost(ctx context.Context, postID uint64) (*forummodel.ForumPost, error) {
	var post forummodel.ForumPost
	err := dao.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND status = ? AND deleted_at IS NULL", postID, forummodel.ForumPostStatusPublished).
		First(&post).Error
	if err != nil {
		return nil, err
	}
	return &post, nil
}

func (dao *ForumDAO) FindTemplateByPostID(ctx context.Context, postID uint64) (*forummodel.ForumPostTemplate, error) {
	var template forummodel.ForumPostTemplate
	err := dao.db.WithContext(ctx).Where("post_id = ?", postID).First(&template).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (dao *ForumDAO) ListTemplateItemsByPostID(ctx context.Context, postID uint64) ([]forummodel.ForumPostTemplateItem, error) {
	var items []forummodel.ForumPostTemplateItem
	err := dao.db.WithContext(ctx).
		Where("post_id = ?", postID).
		Order("item_order ASC").
		Find(&items).Error
	return items, err
}

func (dao *ForumDAO) FindTemplatesByPostIDs(ctx context.Context, postIDs []uint64) (map[uint64]forummodel.ForumPostTemplate, error) {
	var templates []forummodel.ForumPostTemplate
	err := dao.db.WithContext(ctx).Where("post_id IN ?", postIDs).Find(&templates).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]forummodel.ForumPostTemplate, len(templates))
	for _, template := range templates {
		result[template.PostID] = template
	}
	return result, nil
}

func (dao *ForumDAO) CountTemplateItemsByPostIDs(ctx context.Context, postIDs []uint64) (map[uint64]int, error) {
	var rows []struct {
		PostID uint64
		Count  int
	}
	err := dao.db.WithContext(ctx).
		Model(&forummodel.ForumPostTemplateItem{}).
		Select("post_id, COUNT(*) AS count").
		Where("post_id IN ?", postIDs).
		Group("post_id").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]int, len(rows))
	for _, row := range rows {
		result[row.PostID] = row.Count
	}
	return result, nil
}

func (dao *ForumDAO) LikedPostIDSet(ctx context.Context, userID uint64, postIDs []uint64) (map[uint64]bool, error) {
	var likes []forummodel.ForumLike
	err := dao.db.WithContext(ctx).
		Select("post_id").
		Where("user_id = ? AND post_id IN ? AND status = ?", userID, postIDs, forummodel.ForumLikeStatusActive).
		Find(&likes).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]bool, len(likes))
	for _, like := range likes {
		result[like.PostID] = true
	}
	return result, nil
}

func (dao *ForumDAO) ImportedPostIDSet(ctx context.Context, userID uint64, postIDs []uint64) (map[uint64]bool, error) {
	var imports []forummodel.ForumImport
	err := dao.db.WithContext(ctx).
		Select("post_id").
		Where("user_id = ? AND post_id IN ? AND status = ?", userID, postIDs, forummodel.ForumImportStatusImported).
		Find(&imports).Error
	if err != nil {
		return nil, err
	}
	result := make(map[uint64]bool, len(imports))
	for _, item := range imports {
		result[item.PostID] = true
	}
	return result, nil
}

func (dao *ForumDAO) FindLike(ctx context.Context, postID uint64, userID uint64) (*forummodel.ForumLike, error) {
	var like forummodel.ForumLike
	err := dao.db.WithContext(ctx).Where("post_id = ? AND user_id = ?", postID, userID).First(&like).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &like, nil
}

func (dao *ForumDAO) CreateLike(ctx context.Context, like *forummodel.ForumLike) error {
	return dao.db.WithContext(ctx).Create(like).Error
}

func (dao *ForumDAO) ActivateLike(ctx context.Context, likeID uint64) error {
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumLike{}).
		Where("id = ?", likeID).
		Updates(map[string]any{
			"status":      forummodel.ForumLikeStatusActive,
			"canceled_at": nil,
			"updated_at":  time.Now(),
		}).Error
}

func (dao *ForumDAO) CancelLike(ctx context.Context, likeID uint64, now time.Time) error {
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumLike{}).
		Where("id = ?", likeID).
		Updates(map[string]any{
			"status":      forummodel.ForumLikeStatusCanceled,
			"canceled_at": &now,
			"updated_at":  now,
		}).Error
}

func (dao *ForumDAO) AddPostCounter(ctx context.Context, postID uint64, column string, delta int64) error {
	expr := "CASE WHEN " + column + " + ? < 0 THEN 0 ELSE " + column + " + ? END"
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumPost{}).
		Where("id = ?", postID).
		UpdateColumn(column, gorm.Expr(expr, delta, delta)).Error
}

func (dao *ForumDAO) FindCommentByID(ctx context.Context, commentID uint64) (*forummodel.ForumComment, error) {
	var comment forummodel.ForumComment
	err := dao.db.WithContext(ctx).Where("id = ?", commentID).First(&comment).Error
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (dao *ForumDAO) LockCommentByID(ctx context.Context, commentID uint64) (*forummodel.ForumComment, error) {
	var comment forummodel.ForumComment
	err := dao.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", commentID).
		First(&comment).Error
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (dao *ForumDAO) FindCommentByIdempotencyKey(ctx context.Context, userID uint64, key string) (*forummodel.ForumComment, error) {
	var comment forummodel.ForumComment
	err := dao.db.WithContext(ctx).
		Where("user_id = ? AND idempotency_key = ?", userID, key).
		First(&comment).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &comment, nil
}

func (dao *ForumDAO) CreateComment(ctx context.Context, comment *forummodel.ForumComment) error {
	return dao.db.WithContext(ctx).Create(comment).Error
}

func (dao *ForumDAO) CountRootComments(ctx context.Context, postID uint64) (int64, error) {
	var total int64
	err := dao.db.WithContext(ctx).
		Model(&forummodel.ForumComment{}).
		Where("post_id = ? AND parent_comment_id IS NULL", postID).
		Count(&total).Error
	return total, err
}

func (dao *ForumDAO) ListRootComments(ctx context.Context, postID uint64, page int, pageSize int, sort string) ([]forummodel.ForumComment, error) {
	orderBy := "created_at ASC"
	if strings.TrimSpace(sort) == "latest" {
		orderBy = "created_at DESC"
	}
	var comments []forummodel.ForumComment
	err := dao.db.WithContext(ctx).
		Where("post_id = ? AND parent_comment_id IS NULL", postID).
		Order(orderBy).
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&comments).Error
	return comments, err
}

func (dao *ForumDAO) ListCommentsByPostID(ctx context.Context, postID uint64) ([]forummodel.ForumComment, error) {
	var comments []forummodel.ForumComment
	err := dao.db.WithContext(ctx).
		Where("post_id = ?", postID).
		Order("created_at ASC").
		Find(&comments).Error
	return comments, err
}

func (dao *ForumDAO) SoftDeleteComment(ctx context.Context, commentID uint64, now time.Time) error {
	tx := dao.db.WithContext(ctx).
		Model(&forummodel.ForumComment{}).
		Where("id = ? AND status = ?", commentID, forummodel.ForumCommentStatusVisible).
		Updates(map[string]any{
			"status":     forummodel.ForumCommentStatusDeleted,
			"deleted_at": &now,
			"updated_at": now,
		})
	return tx.Error
}

func (dao *ForumDAO) FindImport(ctx context.Context, postID uint64, userID uint64) (*forummodel.ForumImport, error) {
	var item forummodel.ForumImport
	err := dao.db.WithContext(ctx).Where("post_id = ? AND user_id = ?", postID, userID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (dao *ForumDAO) FindImportByIdempotencyKey(ctx context.Context, userID uint64, key string) (*forummodel.ForumImport, error) {
	var item forummodel.ForumImport
	err := dao.db.WithContext(ctx).
		Where("user_id = ? AND idempotency_key = ?", userID, key).
		First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (dao *ForumDAO) CreateImport(ctx context.Context, item *forummodel.ForumImport) error {
	return dao.db.WithContext(ctx).Create(item).Error
}

func (dao *ForumDAO) UpdateImportProcessing(ctx context.Context, importID uint64, title string, now time.Time) error {
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumImport{}).
		Where("id = ?", importID).
		Updates(map[string]any{
			"target_title": title,
			"status":       forummodel.ForumImportStatusPending,
			"last_error":   nil,
			"updated_at":   now,
		}).Error
}

func (dao *ForumDAO) FinalizeImport(ctx context.Context, importID uint64, newTaskClassID uint64, targetTitle string, now time.Time) error {
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumImport{}).
		Where("id = ?", importID).
		Updates(map[string]any{
			"new_task_class_id": &newTaskClassID,
			"target_title":      targetTitle,
			"status":            forummodel.ForumImportStatusImported,
			"last_error":        nil,
			"updated_at":        now,
		}).Error
}

func (dao *ForumDAO) MarkImportFailed(ctx context.Context, importID uint64, message string, now time.Time) error {
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumImport{}).
		Where("id = ?", importID).
		Updates(map[string]any{
			"status":     forummodel.ForumImportStatusFailed,
			"last_error": &message,
			"updated_at": now,
		}).Error
}

func (dao *ForumDAO) MarkImportFailedAfterTaskClassCreated(ctx context.Context, importID uint64, newTaskClassID uint64, targetTitle string, message string, now time.Time) error {
	return dao.db.WithContext(ctx).
		Model(&forummodel.ForumImport{}).
		Where("id = ?", importID).
		Updates(map[string]any{
			"new_task_class_id": &newTaskClassID,
			"target_title":      targetTitle,
			"status":            forummodel.ForumImportStatusFailed,
			"last_error":        &message,
			"updated_at":        now,
		}).Error
}
