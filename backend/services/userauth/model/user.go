package model

import "time"

// User 是 user/auth 服务内部拥有的 users 表模型。
// 职责边界：只覆盖 user/auth 需要维护的字段，不承载 gateway 或其他领域规则。
type User struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Username    string    `gorm:"type:varchar(255);not null;unique" json:"username"`
	Password    string    `gorm:"type:varchar(255);not null" json:"-"`
	PhoneNumber string    `gorm:"type:varchar(255)" json:"phone_number"`
	TokenLimit  int       `gorm:"default:100000" json:"token_limit"`
	TokenUsage  int       `gorm:"default:0" json:"token_usage"`
	LastResetAt time.Time `json:"last_reset_at"`
}

func (User) TableName() string {
	return "users"
}
