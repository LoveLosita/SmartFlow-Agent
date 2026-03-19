package model

import "time"

type ScheduleEvent struct {
	ID            int       `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID        int       `gorm:"column:user_id;index:idx_user_events;not null" json:"user_id"`
	Name          string    `gorm:"column:name;type:varchar(255);not null;comment:课程或任务名称" json:"name"`
	Location      *string   `gorm:"column:location;type:varchar(255);default:'';comment:地点 (教学楼/会议室)" json:"location"`
	Type          string    `gorm:"column:type;type:enum('course','task');not null;comment:日程类型" json:"type"`
	RelID         *int      `gorm:"column:rel_id;comment:关联原始数据ID (如教务系统的课程ID)" json:"rel_id"`
	CanBeEmbedded bool      `gorm:"column:can_be_embedded;not null;default:0;comment:是否允许在此时段嵌入其他任务" json:"can_be_embedded"`
	StartTime     time.Time `gorm:"column:start_time;type:time;comment:开始时间" json:"start_time"`
	EndTime       time.Time `gorm:"column:end_time;type:time;comment:结束时间" json:"end_time"`
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
	Event        *ScheduleEvent `gorm:"foreignKey:EventID" json:"event"`
	EmbeddedTask *TaskClassItem `gorm:"foreignKey:EmbeddedTaskID" json:"embedded_task"`
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

type UserTodaySchedule struct {
	DayOfWeek int          `json:"day_of_week"`
	Week      int          `json:"week"`
	Events    []EventBrief `json:"events"`
}

type EventBrief struct {
	ID               int       `json:"id"`    // 这个 ID 是 ScheduleEvent 的 ID，不是 Schedule 的 ID
	Order            int       `json:"order"` // order 用于区分它们的显示顺序
	Name             string    `json:"name"`
	StartTime        string    `json:"start_time"`
	EndTime          string    `json:"end_time"`
	Location         string    `json:"location"`
	Type             string    `json:"type"`
	Span             int       `json:"span"` // 跨越的节数，给前端用来渲染宽度/高度
	EmbeddedTaskInfo TaskBrief `json:"embedded_task_info,omitempty"`
}

type TaskBrief struct {
	ID   int    `json:"id"` // 这个 ID 是 ScheduleEvent 的 ID，不是 Schedule 的 ID
	Name string `json:"name"`
	/*StartTime          string `json:"start_time"`
	EndTime            string `json:"end_time"`*/
	Type string `json:"type"`
}

type UserWeekSchedule struct {
	Week   int                `json:"week"`
	Events []WeeklyEventBrief `json:"events"`
}

type WeeklyEventBrief struct {
	ID               int       `json:"id"`    // 这个 ID 是 ScheduleEvent 的 ID，不是 Schedule 的 ID
	Order            int       `json:"order"` // order 用于区分它们在一天中的显示顺序
	DayOfWeek        int       `json:"day_of_week"`
	Name             string    `json:"name"`
	StartTime        string    `json:"start_time"`
	EndTime          string    `json:"end_time"`
	Location         string    `json:"location"`
	Type             string    `json:"type"`
	Span             int       `json:"span"` // 跨越的节数，给前端用来渲染宽度/高度
	Status           string    `json:"status"`
	EmbeddedTaskInfo TaskBrief `json:"embedded_task_info,omitempty"`
}

type UserDeleteScheduleEvent struct {
	ID                 int  `json:"id"` // 这个 ID 是 ScheduleEvent 的 ID，不是 Schedule 的 ID
	DeleteCourse       bool `json:"delete_course"`
	DeleteEmbeddedTask bool `json:"delete_embedded_task"`
}

type UserRecentCompletedScheduleResponse struct {
	Events []RecentCompletedEventBrief `json:"events"`
}

type RecentCompletedEventBrief struct {
	ID            int    `json:"id"` //如果是嵌入的任务事件，这个ID是TaskClassItem的ID；如果是课程事件，这个ID是ScheduleEvent的ID
	Name          string `json:"name"`
	Type          string `json:"type"`
	CompletedTime string `json:"completed_time"`
}

type OngoingSchedule struct {
	ID         int       `json:"id"` // 这个 ID 是 ScheduleEvent 的 ID，不是 Schedule 的 ID
	Name       string    `json:"name"`
	Location   string    `json:"location"`
	Type       string    `json:"type"`
	TimeStatus string    `json:"time_status"` // "upcoming", "ongoing"
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
}

// HybridScheduleEntry 表示"混合日程"中的一个时间块。
//
// 设计目标：
// 将既有日程（课程/已落库任务）与粗排建议的任务统一到同一结构中，
// 供 ReAct 精排引擎在内存中操作。
//
// Status 语义：
// - "existing"：已确定的日程，LLM 不可移动；
// - "suggested"：粗排建议的任务，LLM 可通过 Tool 调整时间。
type HybridScheduleEntry struct {
	Week        int    `json:"week"`
	DayOfWeek   int    `json:"day_of_week"`
	SectionFrom int    `json:"section_from"`
	SectionTo   int    `json:"section_to"`
	Name        string `json:"name"`
	Type        string `json:"type"`                   // "course" | "task"
	Status      string `json:"status"`                 // "existing" | "suggested"
	TaskItemID  int    `json:"task_item_id,omitempty"` // 仅 suggested 的 task 有值
	EventID     int    `json:"event_id,omitempty"`     // 仅 existing 有值
}

func (ScheduleEvent) TableName() string { return "schedule_events" }

func (Schedule) TableName() string { return "schedules" }
