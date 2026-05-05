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

func (r *SettingsRepo) WithTx(tx *gorm.DB) *SettingsRepo {
	return &SettingsRepo{db: tx}
}

// GetByUserID 读取用户记忆设置。
//
// 返回语义：
// 1. 命中时返回真实记录；
// 2. 未命中时返回 nil,nil，由上层决定是否走默认开关；
// 3. 不在仓储层偷偷补默认值，避免写路径和读路径语义不一致。
func (r *SettingsRepo) GetByUserID(ctx context.Context, userID int) (*model.MemoryUserSetting, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("memory settings repo is nil")
	}
	if userID <= 0 {
		return nil, errors.New("memory settings user_id is invalid")
	}

	var setting model.MemoryUserSetting
	query := r.db.WithContext(ctx).Where("user_id = ?", userID).Limit(1).Find(&setting)
	if query.Error != nil {
		return nil, query.Error
	}
	if query.RowsAffected == 0 {
		return nil, nil
	}
	return &setting, nil
}

// Upsert 写入用户记忆设置。
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
