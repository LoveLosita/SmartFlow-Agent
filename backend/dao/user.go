package dao

import (
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
func (dao *UserDAO) Create(username, phoneNumber, password string) (*model.User, error) {
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
	if err := dao.db.Create(user).Error; err != nil {
		return nil, err
	}

	return user, nil
}

func (dao *UserDAO) IfUsernameExists(name string) (bool, error) {
	err := dao.db.Where("username = ?", name).First(&model.User{}).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return true, err
	}
	return true, nil
}

func (dao *UserDAO) GetUserHashedPasswordByName(name string) (string, error) {
	var user model.User
	err := dao.db.Where("username = ?", name).First(&user).Error
	if err != nil {
		return "", err
	}
	return user.Password, nil
}

func (dao *UserDAO) GetUserIDByName(name string) (int, error) {
	var user model.User
	err := dao.db.Where("username = ?", name).First(&user).Error
	if err != nil {
		return -1, err
	}
	return int(user.ID), nil
}
