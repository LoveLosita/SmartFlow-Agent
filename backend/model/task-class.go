package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

type TaskClass struct {
	//section 1
	ID     int  `gorm:"column:id;primaryKey;autoIncrement"`
	UserID *int `gorm:"column:user_id;index:idx_task_classes_user_id"`
	//section 2
	Name      *string    `gorm:"column:name;size:255"`
	Mode      *string    `gorm:"column:mode;type:enum('auto','manual')"`
	StartDate *time.Time `gorm:"column:start_date"`
	EndDate   *time.Time `gorm:"column:end_date"`
	//section 3
	TotalSlots        *int            `gorm:"column:total_slots;comment:分配的总节数"`
	AllowFillerCourse *bool           `gorm:"column:allow_filler_course;default:true"`
	Strategy          *string         `gorm:"column:strategy;type:enum('steady','rapid')"`
	ExcludedSlots     *string         `gorm:"column:excluded_slots;type:json;comment:不想要的时段切片"`
	Items             []TaskClassItem `gorm:"foreignKey:CategoryID;references:ID"` // 一对多关联：一个 TaskClass 有多个 TaskClassItem
}

// TableName 设定 TaskClass 的表名为 task_classes
func (TaskClass) TableName() string {
	return "task_classes"
}

type UserAddTaskClassRequest struct {
	Name      string                        `json:"name" binding:"required"`
	StartDate string                        `json:"start_date" binding:"required"` // YYYY-MM-DD
	EndDate   string                        `json:"end_date" binding:"required"`   // YYYY-MM-DD
	Mode      string                        `json:"mode" binding:"required,oneof=auto manual"`
	Config    UserAddTaskClassConfig        `json:"config" binding:"required"`
	Items     []UserAddTaskClassItemRequest `json:"items" binding:"required"`
}

type UserAddTaskClassConfig struct {
	TotalSlots        int    `json:"total_slots" binding:"required,min=1"`
	AllowFillerCourse bool   `json:"allow_filler_course"`
	Strategy          string `json:"strategy" binding:"required,oneof=steady rapid"`
	ExcludedSlots     []int  `json:"excluded_slots"`
}

type UserAddTaskClassItemRequest struct {
	Order        int         `json:"order" binding:"required,min=1"`
	Content      string      `json:"content" binding:"required"`
	EmbeddedTime *TargetTime `json:"embedded_time"` // 例: 2025-12-22 1-2节; nil 表示未安排
}

type TaskClassItem struct {
	//section 1
	ID         int  `gorm:"column:id;primaryKey;autoIncrement"`
	CategoryID *int `gorm:"column:category_id"` //对应 TaskClass 的 ID
	//section 2
	Order        *int        `gorm:"column:order"`
	Content      *string     `gorm:"column:content;type:text"`
	EmbeddedTime *TargetTime `gorm:"column:embedded_time;type:json;comment:目标时间{date,section_from,section_to}"`
	Status       *int        `gorm:"column:status;comment:1:未安排, 2:已应用"`
}

type TargetTime struct {
	Date        string `json:"date"`         // 例: 2025-12-22
	SectionFrom int    `json:"section_from"` // 起始节次
	SectionTo   int    `json:"section_to"`   // 结束节次
}

func (t *TargetTime) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	// 💡 关键：调用 json.Marshal 将结构体转为 []byte
	// 这样 GORM 就能把这一串 JSON 存进数据库的 text/json 字段了
	return json.Marshal(t)
}

func (t *TargetTime) Scan(value any) error {
	if value == nil {
		// 如果数据库是 NULL，保持指针对应的对象为零值即可
		// 或者在业务层判断 nil
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("TargetTime: 不支持的扫描类型: %T", value)
	}

	return json.Unmarshal(data, t)
}

func (TaskClassItem) TableName() string {
	return "task_items"
}

const (
	TaskItemStatusUnscheduled = 1
	TaskItemStatusApplied     = 2
)

type UserGetTaskClassesResponse struct {
	TaskClasses []TaskClassSummary `json:"task_classes"`
}

type TaskClassSummary struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Mode       string    `json:"mode"`
	Strategy   string    `json:"strategy"`
	StartDate  time.Time `json:"start_date"`
	EndDate    time.Time `json:"end_date"`
	TotalSlots int       `json:"total_slots"`
}
