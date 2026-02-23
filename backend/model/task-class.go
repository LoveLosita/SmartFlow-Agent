package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// TaskClass 用于和数据库中的 task_classes 表进行映射
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
	ExcludedSlots     IntSlice        `gorm:"column:excluded_slots;type:json;comment:不想要的时段切片"`
	Items             []TaskClassItem `gorm:"foreignKey:CategoryID;references:ID"` // 一对多关联：一个 TaskClass 有多个 TaskClassItem
}

// IntSlice 用于把 []int 以 JSON 形式存入/读出数据库 json 字段
type IntSlice []int

func (s IntSlice) Value() (driver.Value, error) {
	// nil -> NULL；空切片 -> "[]"
	if s == nil {
		return nil, nil
	}
	return json.Marshal([]int(s))
}

func (s *IntSlice) Scan(value any) error {
	if value == nil {
		*s = nil
		return nil
	}

	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("IntSlice: 不支持的扫描类型: %T", value)
	}

	var out []int
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	*s = IntSlice(out)
	return nil
}

// TaskClassItem 用于和数据库中的 task_items 表进行映射
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

// UserAddTaskClassRequest 用于处理用户添加任务类别的请求
type UserAddTaskClassRequest struct {
	Name      string                        `json:"name" binding:"required"`
	StartDate string                        `json:"start_date" binding:"required"` // YYYY-MM-DD
	EndDate   string                        `json:"end_date" binding:"required"`   // YYYY-MM-DD
	Mode      string                        `json:"mode" binding:"required,oneof=auto manual"`
	Config    UserAddTaskClassConfig        `json:"config" binding:"required"`
	Items     []UserAddTaskClassItemRequest `json:"items" binding:"required"`
}

// UserAddTaskClassConfig 用于处理用户添加任务类别时的配置部分
type UserAddTaskClassConfig struct {
	TotalSlots        int    `json:"total_slots" binding:"required,min=1"`
	AllowFillerCourse bool   `json:"allow_filler_course"`
	Strategy          string `json:"strategy" binding:"required,oneof=steady rapid"`
	ExcludedSlots     []int  `json:"excluded_slots"`
}

// UserAddTaskClassItemRequest 用于处理用户添加任务类别时的任务块部分
type UserAddTaskClassItemRequest struct {
	Order        int         `json:"order" binding:"required,min=1"`
	Content      string      `json:"content" binding:"required"`
	EmbeddedTime *TargetTime `json:"embedded_time"` // 例: 2025-12-22 1-2节; nil 表示未安排
}

// TargetTime 表示任务块的目标时间
type TargetTime struct {
	Week        int `json:"week"`         // 周次
	DayOfWeek   int `json:"day_of_week"`  // 星期几
	SectionFrom int `json:"section_from"` // 起始节次
	SectionTo   int `json:"section_to"`   // 结束节次
}

// UserGetTaskClassesResponse 用于返回用户的任务类列表，展示简要信息
type UserGetTaskClassesResponse struct {
	TaskClasses []TaskClassSummary `json:"task_classes"`
}

// TaskClassSummary 提供任务类别的简要信息
type TaskClassSummary struct {
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Mode       string    `json:"mode"`
	Strategy   string    `json:"strategy"`
	StartDate  time.Time `json:"start_date"`
	EndDate    time.Time `json:"end_date"`
	TotalSlots int       `json:"total_slots"`
}

type UserInsertTaskClassItemToScheduleRequest struct {
	Week               int `json:"week" binding:"required,min=1"`
	DayOfWeek          int `json:"day_of_week" binding:"required,min=1,max=7"`
	StartSection       int `json:"start_section" binding:"required,min=1"`
	EndSection         int `json:"end_section" binding:"required,min=1,gtefield=StartSection"`
	EmbedCourseEventID int `json:"embed_course_event_id"` // 可选，嵌入的课程日程事件 ID
}

// Value 实现 driver.Valuer 接口，负责将 TargetTime 转换为数据库存储的格式
func (t *TargetTime) Value() (driver.Value, error) {
	if t == nil {
		return nil, nil
	}
	// 💡 关键：调用 json.Marshal 将结构体转为 []byte
	// 这样 GORM 就能把这一串 JSON 存进数据库的 text/json 字段了
	return json.Marshal(t)
}

// Scan 实现 sql.Scanner 接口，负责将数据库中的值转换为 TargetTime 结构体
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

// TableName 指定 TaskClass 对应的数据库表名
func (TaskClass) TableName() string {
	return "task_classes"
}

// TableName 指定 TaskClassItem 对应的数据库表名
func (TaskClassItem) TableName() string {
	return "task_items"
}

// 任务块状态常量
const (
	TaskItemStatusUnscheduled = 1 // 未安排
	TaskItemStatusApplied     = 2 // 已应用
)
