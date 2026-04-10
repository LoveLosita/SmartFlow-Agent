package repo

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// ItemRepo 封装 memory_items 的数据访问（Day1 先占位）。
type ItemRepo struct {
	db *gorm.DB
}

func NewItemRepo(db *gorm.DB) *ItemRepo {
	return &ItemRepo{db: db}
}

// UpsertItems 预留给 Day2/Day3 的写入链路。
//
// Day1 约束：
// 1. 先完成任务入队与状态机闭环；
// 2. 不在本阶段引入复杂冲突消解与向量写入。
func (r *ItemRepo) UpsertItems(ctx context.Context, items []model.MemoryItem) error {
	if r == nil || r.db == nil {
		return errors.New("memory item repo is nil")
	}
	if len(items) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Create(&items).Error
}
