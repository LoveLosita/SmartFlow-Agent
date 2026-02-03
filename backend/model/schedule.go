package model

type Schedule struct {
	ID             int    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID         int    `gorm:"column:user_id;index" json:"user_id"`
	Type           string `gorm:"type:enum('course','task');comment:'course / task'" json:"type"`
	RelID          int    `gorm:"column:rel_id;comment:'关联 course_id 或 task_item_id'" json:"rel_id"`
	CanBeEmbedded  bool   `gorm:"column:can_be_embedded;comment:'是否允许嵌入水课'" json:"can_be_embedded"`
	EmbeddedTaskID *uint  `gorm:"column:embedded_task_id;comment:'若为水课嵌入，记录任务ID'" json:"embedded_task_id"`
	Week           int    `gorm:"column:week" json:"week"`
	DayOfWeek      int    `gorm:"column:day_of_week" json:"day_of_week"`
	Sections       string `gorm:"type:varchar(255)" json:"sections"`
	Status         string `gorm:"type:enum('normal','interrupted');default:'normal'" json:"status"`
}

type UserImportCoursesRequest struct {
	Courses []UserCheckCourseRequest `json:"courses"`
}

type UserCheckCourseRequest struct {
	CourseName   string `json:"course_name"`
	Location     string `json:"location"`
	IsAllowTasks bool   `json:"is_allow_tasks"`
	Arrangements []struct {
		StartWeek    int `json:"start_week"`
		EndWeek      int `json:"end_week"`
		DayOfWeek    int `json:"day_of_week"`
		StartSection int `json:"start_section"`
		EndSection   int `json:"end_section"`
	} `json:"arrangements"`
}
