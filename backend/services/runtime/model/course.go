package model

type UserImportCoursesRequest struct {
	Courses []UserCheckCourseRequest `json:"courses"`
}

type UserCheckCourseRequest struct {
	CourseName   string `json:"course_name"`
	Location     string `json:"location"`
	IsAllowTasks bool   `json:"is_allow_tasks"`
	Arrangements []struct {
		StartWeek    int    `json:"start_week"`
		EndWeek      int    `json:"end_week"`
		DayOfWeek    int    `json:"day_of_week"`
		StartSection int    `json:"start_section"`
		EndSection   int    `json:"end_section"`
		WeekType     string `json:"week_type"`
	} `json:"arrangements"`
}
