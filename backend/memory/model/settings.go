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
