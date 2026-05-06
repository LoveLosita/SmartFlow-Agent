package model

type CourseImageParseDraftStatus string

const (
	CourseImageParseDraftStatusSuccess CourseImageParseDraftStatus = "success"
	CourseImageParseDraftStatusPartial CourseImageParseDraftStatus = "partial"
	CourseImageParseDraftStatusReject  CourseImageParseDraftStatus = "reject"
)

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
	DraftStatus CourseImageParseDraftStatus `json:"draft_status"`
	Message     string                      `json:"message"`
	Warnings    []string                    `json:"warnings"`
	Rows        []CourseImageParseRow       `json:"rows"`
}

type CourseImageParseRequest struct {
	UserID     int
	Filename   string
	MIMEType   string
	ImageBytes []byte
}
