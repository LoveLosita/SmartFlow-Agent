package course

import "encoding/json"

// CourseArrangement 是课程导入中单个上课时间片的跨进程契约。
type CourseArrangement struct {
	StartWeek    int    `json:"start_week"`
	EndWeek      int    `json:"end_week"`
	DayOfWeek    int    `json:"day_of_week"`
	StartSection int    `json:"start_section"`
	EndSection   int    `json:"end_section"`
	WeekType     string `json:"week_type"`
}

// UserCheckCourseRequest 是 course validate / import 共用的课程输入契约。
//
// 职责边界：
// 1. 只描述 HTTP 与 course 服务之间稳定传递的字段；
// 2. 不承载课程冲突检测、时间换算或 schedule 写入逻辑；
// 3. UserID 由 gateway 在 import 场景补齐，不信任前端传入。
type UserCheckCourseRequest struct {
	CourseName   string              `json:"course_name"`
	Location     string              `json:"location"`
	IsAllowTasks bool                `json:"is_allow_tasks"`
	Arrangements []CourseArrangement `json:"arrangements"`
}

type UserImportCoursesRequest struct {
	UserID  int                      `json:"user_id"`
	Courses []UserCheckCourseRequest `json:"courses"`
}

// ImportCoursesResult 用来保留旧 HTTP 在冲突时返回 conflicts 数据的语义。
type ImportCoursesResult struct {
	Conflict  bool            `json:"conflict"`
	Conflicts json.RawMessage `json:"conflicts,omitempty"`
}

type CourseImageParseRequest struct {
	Filename   string `json:"filename"`
	MIMEType   string `json:"mime_type"`
	ImageBytes []byte `json:"image_bytes"`
}

type CourseImageParseRow struct {
	RowID        string   `json:"row_id"`
	CourseName   string   `json:"course_name"`
	Location     string   `json:"location"`
	IsAllowTasks bool     `json:"is_allow_tasks"`
	StartWeek    *int     `json:"start_week"`
	EndWeek      *int     `json:"end_week"`
	DayOfWeek    *int     `json:"day_of_week"`
	StartSection *int     `json:"start_section"`
	EndSection   *int     `json:"end_section"`
	WeekType     string   `json:"week_type"`
	Confidence   float64  `json:"confidence"`
	RawText      string   `json:"raw_text"`
	RowWarnings  []string `json:"row_warnings"`
}

type CourseImageParseResponse struct {
	DraftStatus string                `json:"draft_status"`
	Message     string                `json:"message"`
	Warnings    []string              `json:"warnings"`
	Rows        []CourseImageParseRow `json:"rows"`
}
