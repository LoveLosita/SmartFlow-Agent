package model

import "time"

// UserSettingDTO 是用户记忆开关领域对象。
type UserSettingDTO struct {
	UserID                 int
	MemoryEnabled          bool
	ImplicitMemoryEnabled  bool
	SensitiveMemoryEnabled bool
	UpdatedAt              *time.Time
}

// UpdateUserSettingRequest 描述记忆开关写入请求。
type UpdateUserSettingRequest struct {
	UserID                 int
	MemoryEnabled          bool
	ImplicitMemoryEnabled  bool
	SensitiveMemoryEnabled bool
}
