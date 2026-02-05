package model

type Schedule struct {
	ID             int    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         int    `gorm:"column:user_id;index" json:"user_id"`
	Type           string `gorm:"type:enum('course','task');comment:course / task" json:"type"`
	RelID          int    `gorm:"column:rel_id;comment:关联 course_id 或 task_item_id" json:"rel_id"`
	EmbeddedTaskID *int   `gorm:"column:embedded_task_id;index;comment:若为水课嵌入，记录任务ID" json:"embedded_task_id"`
	Week           int    `gorm:"column:week;uniqueIndex:idx_user_slot_atomic,priority:2;comment:周次 (1-25)" json:"week"`
	DayOfWeek      int    `gorm:"column:day_of_week;uniqueIndex:idx_user_slot_atomic,priority:3;comment:星期 (1-7)" json:"day_of_week"`
	Section        int    `gorm:"column:section;uniqueIndex:idx_user_slot_atomic,priority:4;comment:原子化节次 (1-12)" json:"section"`
	Status         string `gorm:"type:enum('normal','interrupted');default:normal" json:"status"`
	CanBeEmbedded  bool   `gorm:"column:can_be_embedded;not null;comment:是否允许嵌入任务" json:"can_be_embedded"`
}
