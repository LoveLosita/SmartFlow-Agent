package dao

import (
	"context"
	"errors"
	"strings"
	"time"

	userauthmodel "github.com/LoveLosita/smartflow/backend/services/userauth/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// UserDAO 是 user/auth 服务内部的 users 表访问层。
// 职责边界：只提供注册、登录和额度治理需要的最小读写能力，不暴露整张 users 表给 gateway。
type UserDAO struct {
	db *gorm.DB
}

func NewUserDAO(db *gorm.DB) *UserDAO {
	return &UserDAO{db: db}
}

// Create 创建新用户并初始化 token 额度字段。
func (r *UserDAO) Create(ctx context.Context, username, phoneNumber, password string) (*userauthmodel.User, error) {
	user := &userauthmodel.User{
		Username:    username,
		PhoneNumber: phoneNumber,
		Password:    password,
		TokenLimit:  100000,
		TokenUsage:  0,
		LastResetAt: time.Now(),
	}
	if err := r.db.WithContext(ctx).Create(user).Error; err != nil {
		return nil, err
	}
	return user, nil
}

func (r *UserDAO) IfUsernameExists(ctx context.Context, name string) (bool, error) {
	err := r.db.WithContext(ctx).Where("username = ?", name).First(&userauthmodel.User{}).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

func (r *UserDAO) GetUserHashedPasswordByName(ctx context.Context, name string) (string, error) {
	var user userauthmodel.User
	if err := r.db.WithContext(ctx).Where("username = ?", name).First(&user).Error; err != nil {
		return "", err
	}
	return user.Password, nil
}

func (r *UserDAO) GetUserIDByName(ctx context.Context, name string) (int, error) {
	var user userauthmodel.User
	if err := r.db.WithContext(ctx).Where("username = ?", name).First(&user).Error; err != nil {
		return -1, err
	}
	return int(user.ID), nil
}

// GetUserTokenQuotaByID 只读取额度判断需要的字段。
func (r *UserDAO) GetUserTokenQuotaByID(ctx context.Context, id int) (*userauthmodel.User, error) {
	var user userauthmodel.User
	err := r.db.WithContext(ctx).
		Select("id", "token_limit", "token_usage", "last_reset_at").
		Where("id = ?", id).
		First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ResetUserTokenUsageIfDue 使用条件更新实现幂等懒重置。
func (r *UserDAO) ResetUserTokenUsageIfDue(ctx context.Context, id int, dueBefore time.Time, resetAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&userauthmodel.User{}).
		Where("id = ? AND (last_reset_at IS NULL OR last_reset_at <= ?)", id, dueBefore).
		Updates(map[string]interface{}{
			"token_usage":   0,
			"last_reset_at": resetAt,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// AddTokenUsage 为用户 token 账本做增量累加。
// 职责边界：
// 1. 只做数据库累加，不负责额度判断与缓存刷新；
// 2. delta<=0 视为无操作，直接返回成功；
// 3. 由 service 层决定是否需要先做懒重置和后续 cache 回填。
func (r *UserDAO) AddTokenUsage(ctx context.Context, id int, delta int) (bool, error) {
	if delta <= 0 {
		return true, nil
	}
	result := r.db.WithContext(ctx).
		Model(&userauthmodel.User{}).
		Where("id = ?", id).
		Update("token_usage", gorm.Expr("token_usage + ?", delta))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// AdjustTokenUsageOnce 在同一个 MySQL 事务里完成“幂等占位 + token 用量增量”。
//
// 职责边界：
// 1. eventID 非空时先写入 user_token_usage_adjustments，依赖主键冲突判断是否重复事件；
// 2. 只有幂等占位写入成功后才更新 users.token_usage，保证并发重放不会重复记账；
// 3. 不负责 Redis 快照和封禁键维护，这些缓存语义仍由 service 层在事务成功后刷新。
func (r *UserDAO) AdjustTokenUsageOnce(ctx context.Context, eventID string, id int, delta int, dueBefore time.Time, resetAt time.Time) (*userauthmodel.User, bool, error) {
	var quota userauthmodel.User
	duplicated := false
	trimmedEventID := strings.TrimSpace(eventID)

	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if trimmedEventID != "" {
			marker := userauthmodel.TokenUsageAdjustment{
				EventID:    trimmedEventID,
				UserID:     id,
				TokenDelta: delta,
			}
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&marker)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				duplicated = true
				return nil
			}
		}

		resetResult := tx.Model(&userauthmodel.User{}).
			Where("id = ? AND (last_reset_at IS NULL OR last_reset_at <= ?)", id, dueBefore).
			Updates(map[string]interface{}{
				"token_usage":   0,
				"last_reset_at": resetAt,
			})
		if resetResult.Error != nil {
			return resetResult.Error
		}

		updateResult := tx.Model(&userauthmodel.User{}).
			Where("id = ?", id).
			Update("token_usage", gorm.Expr("token_usage + ?", delta))
		if updateResult.Error != nil {
			return updateResult.Error
		}
		if updateResult.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}

		return tx.Select("id", "token_limit", "token_usage", "last_reset_at").
			Where("id = ?", id).
			First(&quota).Error
	})
	if err != nil {
		return nil, false, err
	}
	if duplicated {
		return nil, true, nil
	}
	return &quota, false, nil
}
