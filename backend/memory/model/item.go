package model

import "time"

// ItemDTO 是记忆条目对外读写 DTO。
//
// 职责边界：
// 1. 面向 memory 模块内部服务层使用；
// 2. 不直接绑定 GORM 标签，避免传输结构与存储结构强耦合。
type ItemDTO struct {
	ID               int64
	UserID           int
	ConversationID   string
	AssistantID      string
	RunID            string
	MemoryType       string
	Title            string
	Content          string
	Confidence       float64
	Importance       float64
	SensitivityLevel int
	IsExplicit       bool
	Status           string
	TTLAt            *time.Time
	CreatedAt        *time.Time
	UpdatedAt        *time.Time
}
