// Package model 数据模型层
// 定义所有数据结构和模型
package model

import (
	"time"
)

// TableName 指定表名
// 确保与数据库表名一致
func (User) TableName() string {
	return "users"
}

// User 用户模型
// 对应数据库中的users表
type User struct {
	// 增加 autoIncrement 标签，对应 SQL 的 AUTO_INCREMENT
	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// 增加 unique 和 not null，确保与数据库约束一致
	Username string `gorm:"type:varchar(255);not null;unique" json:"username"`
	// Password 保持 json:"-" 是非常专业的做法，防止接口无意间泄露哈希值
	Password    string `gorm:"type:varchar(255);not null" json:"password"`
	PhoneNumber string `gorm:"type:varchar(255)" json:"phone_number"`
	// 设定默认值，确保 GORM 在插入时能正确处理初始配额
	TokenLimit int `gorm:"default:100000" json:"token_limit"`
	// 增加 default:0，防止出现 null 导致的解析问题
	TokenUsage int `gorm:"default:0" json:"token_usage"`
	// LastResetAt 映射 timestamp
	LastResetAt time.Time `gorm:"comment:上次周用量重置时间" json:"last_reset_at"`
}

type UserRegisterRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	PhoneNumber string `json:"phone_number"`
}

type UserRegisterResponse struct {
	ID uint `json:"id"`
}

type UserLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserLoginResponse struct {
	Tokens
}
