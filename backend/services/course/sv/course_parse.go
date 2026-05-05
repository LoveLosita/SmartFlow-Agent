package sv

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	defaultCourseImageMaxBytes  = 5 * 1024 * 1024
	defaultCourseImageMaxTokens = 16384
	maxCourseImageDraftRows     = 256
	courseImageParseTemperature = 0.1
)

var (
	ErrCourseImageParserUnavailable = errors.New("course image parser is not configured")
	ErrCourseImageTooLarge          = errors.New("course image is too large")
	ErrCourseImageUnsupportedMIME   = errors.New("course image mime type is not supported")
	ErrCourseImageEmpty             = errors.New("course image is empty")
)

type CourseImageParseConfig struct {
	MaxImageBytes int64
	MaxTokens     int
}

func NewCourseImageParseConfig(maxImageBytes int64, maxTokens int) CourseImageParseConfig {
	if maxImageBytes <= 0 {
		maxImageBytes = defaultCourseImageMaxBytes
	}
	if maxTokens <= 0 {
		maxTokens = defaultCourseImageMaxTokens
	}
	return CourseImageParseConfig{
		MaxImageBytes: maxImageBytes,
		MaxTokens:     maxTokens,
	}
}

func normalizeCourseImageParseRequest(req model.CourseImageParseRequest, cfg CourseImageParseConfig) (*model.CourseImageParseRequest, error) {
	req.Filename = strings.TrimSpace(req.Filename)
	req.MIMEType = strings.TrimSpace(strings.ToLower(req.MIMEType))
	if len(req.ImageBytes) == 0 {
		return nil, ErrCourseImageEmpty
	}
	if int64(len(req.ImageBytes)) > cfg.MaxImageBytes {
		return nil, ErrCourseImageTooLarge
	}

	detected := strings.ToLower(strings.TrimSpace(http.DetectContentType(req.ImageBytes)))
	if req.MIMEType == "" || req.MIMEType == "application/octet-stream" {
		req.MIMEType = detected
	}
	if !isSupportedCourseImageMIME(req.MIMEType) {
		if isSupportedCourseImageMIME(detected) {
			req.MIMEType = detected
		} else {
			return nil, ErrCourseImageUnsupportedMIME
		}
	}

	if req.Filename == "" {
		req.Filename = "course-table"
	}
	return &req, nil
}

func isSupportedCourseImageMIME(mimeType string) bool {
	switch strings.TrimSpace(strings.ToLower(mimeType)) {
	case "image/jpeg", "image/png", "image/webp":
		return true
	default:
		return false
	}
}

func normalizeCourseImageParseResponse(resp *model.CourseImageParseResponse) (*model.CourseImageParseResponse, error) {
	if resp == nil {
		return nil, errors.New("course image parse response is nil")
	}

	resp.DraftStatus = model.CourseImageParseDraftStatus(strings.ToLower(strings.TrimSpace(string(resp.DraftStatus))))
	resp.Message = strings.TrimSpace(resp.Message)
	resp.Warnings = normalizeWarningList(resp.Warnings)
	resp.Rows = normalizeCourseImageParseRows(resp.Rows, &resp.Warnings)

	switch resp.DraftStatus {
	case model.CourseImageParseDraftStatusSuccess:
		if len(resp.Rows) == 0 {
			return nil, errors.New("course image parse response has no rows in success status")
		}
		for idx := range resp.Rows {
			if err := validateCourseImageParseRow(&resp.Rows[idx], true); err != nil {
				return nil, fmt.Errorf("course image parse success row %d invalid: %w", idx+1, err)
			}
		}
	case model.CourseImageParseDraftStatusPartial:
		if len(resp.Rows) == 0 {
			return nil, errors.New("course image parse response has no rows in partial status")
		}
		for idx := range resp.Rows {
			if err := validateCourseImageParseRow(&resp.Rows[idx], false); err != nil {
				return nil, fmt.Errorf("course image parse partial row %d invalid: %w", idx+1, err)
			}
		}
	case model.CourseImageParseDraftStatusReject:
		resp.Rows = make([]model.CourseImageParseRow, 0)
	default:
		return nil, fmt.Errorf("unsupported draft_status: %s", resp.DraftStatus)
	}

	if resp.Message == "" {
		resp.Message = defaultCourseImageParseMessage(resp.DraftStatus, len(resp.Rows))
	}
	return resp, nil
}

func normalizeCourseImageParseRows(rows []model.CourseImageParseRow, warnings *[]string) []model.CourseImageParseRow {
	if len(rows) == 0 {
		return make([]model.CourseImageParseRow, 0)
	}
	if len(rows) > maxCourseImageDraftRows {
		rows = rows[:maxCourseImageDraftRows]
		appendUniqueWarning(warnings, "识别结果行数超过上限，后端已截断为 256 行，请重点核对。")
	}

	normalized := make([]model.CourseImageParseRow, 0, len(rows))
	for idx := range rows {
		row := rows[idx]
		row.RowID = strings.TrimSpace(row.RowID)
		if row.RowID == "" {
			row.RowID = fmt.Sprintf("row_%03d", idx+1)
		}
		row.CourseName = strings.TrimSpace(row.CourseName)
		row.Location = strings.TrimSpace(row.Location)
		row.WeekType = normalizeCourseImageWeekType(row.WeekType)
		row.RawText = strings.TrimSpace(row.RawText)
		row.RowWarnings = normalizeWarningList(row.RowWarnings)
		normalizeOptionalPositiveInt(&row.StartWeek)
		normalizeOptionalPositiveInt(&row.EndWeek)
		normalizeOptionalPositiveInt(&row.DayOfWeek)
		normalizeOptionalPositiveInt(&row.StartSection)
		normalizeOptionalPositiveInt(&row.EndSection)
		if row.Confidence < 0 {
			row.Confidence = 0
		}
		if row.Confidence > 1 {
			row.Confidence = 1
		}
		if row.CourseName == "" &&
			row.StartWeek == nil &&
			row.EndWeek == nil &&
			row.DayOfWeek == nil &&
			row.StartSection == nil &&
			row.EndSection == nil &&
			row.RawText == "" {
			appendUniqueWarning(warnings, fmt.Sprintf("存在空白草稿行，后端已自动忽略：%s", row.RowID))
			continue
		}
		normalized = append(normalized, row)
	}

	return normalized
}

func validateCourseImageParseRow(row *model.CourseImageParseRow, strict bool) error {
	if row == nil {
		return errors.New("row is nil")
	}
	if strict && row.CourseName == "" {
		return errors.New("course_name is empty")
	}
	if strict && row.WeekType == "" {
		return errors.New("week_type is empty")
	}
	if row.WeekType != "" && row.WeekType != "all" && row.WeekType != "odd" && row.WeekType != "even" {
		return fmt.Errorf("week_type is invalid: %s", row.WeekType)
	}

	if err := validateOptionalCourseIntPair(row.StartWeek, row.EndWeek, 1, 24, "week", strict); err != nil {
		return err
	}
	if err := validateOptionalCourseIntPair(row.StartSection, row.EndSection, 1, 12, "section", strict); err != nil {
		return err
	}
	if strict && row.DayOfWeek == nil {
		return errors.New("day_of_week is empty")
	}
	if row.DayOfWeek != nil && (*row.DayOfWeek < 1 || *row.DayOfWeek > 7) {
		return fmt.Errorf("day_of_week out of range: %d", *row.DayOfWeek)
	}
	return nil
}

func validateOptionalCourseIntPair(start *int, end *int, min int, max int, field string, strict bool) error {
	if strict {
		if start == nil || end == nil {
			return fmt.Errorf("%s range is incomplete", field)
		}
	}
	if start == nil && end == nil {
		return nil
	}
	if start == nil || end == nil {
		return fmt.Errorf("%s range is incomplete", field)
	}
	if *start < min || *start > max {
		return fmt.Errorf("%s start out of range: %d", field, *start)
	}
	if *end < min || *end > max {
		return fmt.Errorf("%s end out of range: %d", field, *end)
	}
	if *start > *end {
		return fmt.Errorf("%s start is greater than end: %d > %d", field, *start, *end)
	}
	return nil
}

func normalizeOptionalPositiveInt(target **int) {
	if target == nil || *target == nil {
		return
	}
	if **target <= 0 {
		*target = nil
	}
}

func normalizeCourseImageWeekType(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch normalized {
	case "", "unknown", "null":
		return ""
	case "all", "every", "weekly", "each week", "每周", "全周", "全部":
		return "all"
	case "odd", "single", "单", "单周":
		return "odd"
	case "even", "double", "双", "双周":
		return "even"
	default:
		return normalized
	}
}

func normalizeWarningList(items []string) []string {
	if len(items) == 0 {
		return make([]string, 0)
	}
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func appendUniqueWarning(target *[]string, warningText string) {
	if target == nil {
		return
	}
	trimmed := strings.TrimSpace(warningText)
	if trimmed == "" {
		return
	}
	for _, existing := range *target {
		if strings.TrimSpace(existing) == trimmed {
			return
		}
	}
	*target = append(*target, trimmed)
}

func defaultCourseImageParseMessage(status model.CourseImageParseDraftStatus, rowCount int) string {
	switch status {
	case model.CourseImageParseDraftStatusSuccess:
		return fmt.Sprintf("已识别 %d 条课程安排，请重点核对周次、星期和节次。", rowCount)
	case model.CourseImageParseDraftStatusPartial:
		return fmt.Sprintf("已识别 %d 条课程安排，但仍存在不确定字段，请结合 warning 逐项核对。", rowCount)
	case model.CourseImageParseDraftStatusReject:
		return "图片信息不足，建议重新上传完整、清晰、包含表头和节次栏的总课表截图。"
	default:
		return "课程表图片识别已完成，请人工核对后再导入。"
	}
}
