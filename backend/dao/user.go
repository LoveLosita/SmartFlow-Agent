package dao

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// UserDAO 用户数据访问对象
// 负责用户相关的数据库操作
type UserDAO struct {
	// 这是一个口袋，用来装数据库连接实例
	db *gorm.DB
}

// NewUserDAO 创建UserDAO实例
// NewUserDAO 接收一个 *gorm.DB，并把它塞进结构体的口袋里
func NewUserDAO(db *gorm.DB) *UserDAO {
	return &UserDAO{
		db: db,
	}
}

func (r *UserDAO) WithTx(tx *gorm.DB) *UserDAO {
	return &UserDAO{db: tx}
}

// Create 创建新用户
// 插入新用户信息到数据库
func (r *UserDAO) Create(username, phoneNumber, password string) (*model.User, error) {
	// 创建User实例
	user := &model.User{
		Username:    username,
		PhoneNumber: phoneNumber,
		Password:    password,   // 注意：实际项目中应该对密码进行加密处理
		TokenLimit:  100000,     // 默认值
		TokenUsage:  0,          // 初始使用量为0
		LastResetAt: time.Now(), // 设置为当前时间
	}

	// 插入数据
	if err := r.db.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func (r *UserDAO) IfUsernameExists(name string) (bool, error) {
	err := r.db.Where("username = ?", name).First(&model.User{}).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

func (r *UserDAO) GetUserHashedPasswordByName(name string) (string, error) {
	var user model.User
	err := r.db.Where("username = ?", name).First(&user).Error
	if err != nil {
		return "", err
	}
	return user.Password, nil
}

func (r *UserDAO) GetUserIDByName(name string) (int, error) {
	var user model.User
	err := r.db.Where("username = ?", name).First(&user).Error
	if err != nil {
		return -1, err
	}
	return int(user.ID), nil
}

func (r *UserDAO) GetUserByID(id int) (*model.User, error) {
	var user model.User
	err := r.db.Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUserTokenQuotaByID 查询用户 token 配额快照（仅查询配额相关字段）。
//
// 职责边界：
// 1. 只返回 token_limit / token_usage / last_reset_at 等“额度判断必需字段”；
// 2. 不负责做超额判断与重置判断（由中间件统一决策）；
// 3. 不返回密码等敏感字段，避免把无关信息带入鉴权链路。
func (r *UserDAO) GetUserTokenQuotaByID(ctx context.Context, id int) (*model.User, error) {
	var user model.User
	err := r.db.WithContext(ctx).
		Select("id", "token_limit", "token_usage", "last_reset_at").
		Where("id = ?", id).
		First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ResetUserTokenUsageIfDue 在“已到重置窗口”时执行懒重置。
//
// 输入输出语义：
// 1. dueBefore：判定“到期可重置”的截止时间（通常是 now-7d）；
// 2. resetAt：本次重置写入的时间戳；
// 3. 返回值 bool：
//   - true  表示本次调用实际执行了重置；
//   - false 表示条件未命中（尚未到期或记录不存在）。
//
// 并发与幂等说明：
// 1. 使用条件更新（WHERE last_reset_at <= dueBefore）保证并发下最多一次成功重置；
// 2. 重复调用是安全的，未命中条件时不会破坏现有统计。
func (r *UserDAO) ResetUserTokenUsageIfDue(ctx context.Context, id int, dueBefore time.Time, resetAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).
		Model(&model.User{}).
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
