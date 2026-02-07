package model

type ScheduleEvent struct {
	ID            int     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID        int     `gorm:"column:user_id;index:idx_user_events;not null" json:"user_id"`
	Name          string  `gorm:"column:name;type:varchar(255);not null;comment:课程或任务名称" json:"name"`
	Location      *string `gorm:"column:location;type:varchar(255);default:'';comment:地点 (教学楼/会议室)" json:"location"`
	Type          string  `gorm:"column:type;type:enum('course','task');not null;comment:日程类型" json:"type"`
	RelID         *int    `gorm:"column:rel_id;comment:关联原始数据ID (如教务系统的课程ID)" json:"rel_id"`
	CanBeEmbedded bool    `gorm:"column:can_be_embedded;not null;default:0;comment:是否允许在此时段嵌入其他任务" json:"can_be_embedded"`
}

type Schedule struct {
	ID             int    `gorm:"primaryKey;autoIncrement" json:"id"`
	EventID        int    `gorm:"column:event_id;index:idx_event_id;not null;comment:关联元数据ID" json:"event_id"`
	UserID         int    `gorm:"column:user_id;uniqueIndex:idx_user_slot_atomic,priority:1;not null;comment:冗余UID方便直接查询" json:"user_id"`
	Week           int    `gorm:"column:week;uniqueIndex:idx_user_slot_atomic,priority:2;not null;comment:周次 (1-25)" json:"week"`
	DayOfWeek      int    `gorm:"column:day_of_week;uniqueIndex:idx_user_slot_atomic,priority:3;not null;comment:星期 (1-7)" json:"day_of_week"`
	Section        int    `gorm:"column:section;uniqueIndex:idx_user_slot_atomic,priority:4;not null;comment:原子化节次 (1-12)" json:"section"`
	EmbeddedTaskID *int   `gorm:"column:embedded_task_id;comment:若为水课嵌入，记录具体的任务项ID" json:"embedded_task_id"`
	Status         string `gorm:"column:status;type:enum('normal','interrupted');default:'normal';comment:状态: 正常/因故中断" json:"status"`
	// 💡 必须加上这一行，告诉 GORM 如何关联元数据
	Event ScheduleEvent `gorm:"foreignKey:EventID" json:"event"`
}

type ScheduleConflictDetail struct {
	EventID       int                    `json:"event_id"`
	Name          string                 `json:"name"`
	Location      string                 `json:"location"`
	DayOfWeek     int                    `json:"day_of_week"`
	Week          int                    `json:"week"`
	Sections      []int                  `json:"sections"`
	StartSection  int                    `json:"start_section"`
	EndSection    int                    `json:"end_section"`
	Type          string                 `json:"type"`
	EmbeddedTasks []ScheduleEmbeddedTask `json:"embedded_tasks"`
}

type ScheduleEmbeddedTask struct {
	Section int `json:"section"`
	TaskID  int `json:"task_id"`
}

func (ScheduleEvent) TableName() string { return "schedule_events" }

func (Schedule) TableName() string { return "schedules" }
