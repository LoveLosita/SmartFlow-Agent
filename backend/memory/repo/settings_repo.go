package repo

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SettingsRepo 封装 memory_user_settings 的读写。
type SettingsRepo struct {
	db *gorm.DB
}

func NewSettingsRepo(db *gorm.DB) *SettingsRepo {
	return &SettingsRepo{db: db}
}

func (r *SettingsRepo) Upsert(ctx context.Context, setting model.MemoryUserSetting) error {
	if r == nil || r.db == nil {
		return errors.New("memory settings repo is nil")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"memory_enabled",
			"implicit_memory_enabled",
			"sensitive_memory_enabled",
			"updated_at",
		}),
	}).Create(&setting).Error
}
