package newagentshared

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	deadlineLayouts = []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006/01/02 15:04:05",
		"2006/01/02 15:04",
		"2006.01.02 15:04:05",
		"2006.01.02 15:04",
		"2006-01-02",
		"2006/01/02",
		"2006.01.02",
	}
	deadlineDateOnlyLayouts = map[string]struct{}{
		"2006-01-02": {},
		"2006/01/02": {},
		"2006.01.02": {},
	}

	clockHMRegex   = regexp.MustCompile(`(\d{1,2})\s*[:：]\s*(\d{1,2})`)
	clockCNRegex   = regexp.MustCompile(`(\d{1,2})\s*点\s*(半|(\d{1,2})\s*分?)?`)
	ymdRegex       = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?`)
	mdRegex        = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?`)
	dateSepRegex   = regexp.MustCompile(`\d{1,4}\s*[-/.]\s*\d{1,2}(\s*[-/.]\s*\d{1,2})?`)
	weekdayRegex   = regexp.MustCompile(`(下周|下星期|下礼拜|本周|这周|本星期|这星期|周|星期|礼拜)([一二三四五六日天])`)
	relativeTokens = []string{
		"今天", "今日", "今晚", "今早", "今晨", "明天", "明日", "后天", "大后天", "昨天", "昨日",
		"早上", "早晨", "上午", "中午", "下午", "晚上", "傍晚", "夜里", "凌晨",
	}
)

// ParseOptionalDeadline 解析工具输入中的可选截止时间。
func ParseOptionalDeadline(raw string) (*time.Time, error) {
	value := normalizeDeadlineInput(raw)
	if value == "" {
		return nil, nil
	}

	deadline, hasHint, err := parseDeadlineFromText(value, NowToMinute())
	if err != nil {
		return nil, err
	}
	if deadline == nil {
		if !hasHint {
			return nil, fmt.Errorf("deadline_at 格式不支持: %s", value)
		}
		return nil, fmt.Errorf("deadline_at 无法解析: %s", value)
	}
	return deadline, nil
}

// ParseOptionalDeadlineWithNow 在给定时间基准下解析 deadline。
func ParseOptionalDeadlineWithNow(raw string, now time.Time) (*time.Time, error) {
	value := normalizeDeadlineInput(raw)
	if value == "" {
		return nil, nil
	}

	deadline, _, err := parseDeadlineFromText(value, now)
	if err != nil {
		return nil, err
	}
	if deadline == nil {
		return nil, fmt.Errorf("deadline_at 格式不支持: %s", value)
	}
	return deadline, nil
}

func parseDeadlineFromText(value string, now time.Time) (*time.Time, bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, false, nil
	}

	loc := ShanghaiLocation()
	now = now.In(loc)
	hasHint := hasDeadlineHint(value)

	if abs, ok := tryParseAbsoluteDeadline(value, loc); ok {
		return abs, true, nil
	}
	if rel, recognized, err := tryParseRelativeDeadline(value, now, loc); recognized {
		if err != nil {
			return nil, true, err
		}
		return rel, true, nil
	}
	if hasHint {
		return nil, true, fmt.Errorf("deadline_at 格式不支持: %s", value)
	}
	return nil, false, nil
}

func normalizeDeadlineInput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"：", ":",
		"，", ",",
		"。", ".",
		"　", " ",
	)
	return strings.TrimSpace(replacer.Replace(trimmed))
}

func hasDeadlineHint(value string) bool {
	if clockHMRegex.MatchString(value) ||
		clockCNRegex.MatchString(value) ||
		ymdRegex.MatchString(value) ||
		mdRegex.MatchString(value) ||
		dateSepRegex.MatchString(value) ||
		weekdayRegex.MatchString(value) {
		return true
	}
	for _, token := range relativeTokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func tryParseAbsoluteDeadline(value string, loc *time.Location) (*time.Time, bool) {
	for _, layout := range deadlineLayouts {
		var (
			parsed time.Time
			err    error
		)
		if layout == time.RFC3339 {
			parsed, err = time.Parse(layout, value)
			if err == nil {
				parsed = parsed.In(loc)
			}
		} else {
			parsed, err = time.ParseInLocation(layout, value, loc)
		}
		if err != nil {
			continue
		}

		if _, dateOnly := deadlineDateOnlyLayouts[layout]; dateOnly {
			parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 0, 0, loc)
		} else {
			parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), parsed.Hour(), parsed.Minute(), 0, 0, loc)
		}
		return &parsed, true
	}
	return nil, false
}

func tryParseRelativeDeadline(value string, now time.Time, loc *time.Location) (*time.Time, bool, error) {
	baseDate, recognized := inferBaseDate(value, now, loc)
	if !recognized {
		return nil, false, nil
	}

	hour, minute, hasExplicitClock, err := extractClock(value)
	if err != nil {
		return nil, true, err
	}
	if !hasExplicitClock {
		hour, minute = defaultClockByHint(value)
	}

	deadline := time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), hour, minute, 0, 0, loc)
	return &deadline, true, nil
}

func inferBaseDate(value string, now time.Time, loc *time.Location) (time.Time, bool) {
	if matched := ymdRegex.FindStringSubmatch(value); len(matched) == 4 {
		year, _ := strconv.Atoi(matched[1])
		month, _ := strconv.Atoi(matched[2])
		day, _ := strconv.Atoi(matched[3])
		if isValidDate(year, month, day) {
			return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc), true
		}
	}

	if matched := mdRegex.FindStringSubmatch(value); len(matched) == 3 {
		month, _ := strconv.Atoi(matched[1])
		day, _ := strconv.Atoi(matched[2])
		year := now.Year()
		if !isValidDate(year, month, day) {
			return time.Time{}, false
		}
		candidate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
		if candidate.Before(startOfDay(now)) {
			year++
			if !isValidDate(year, month, day) {
				return time.Time{}, false
			}
			candidate = time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
		}
		return candidate, true
	}

	if matched := weekdayRegex.FindStringSubmatch(value); len(matched) == 3 {
		prefix := matched[1]
		target, ok := toWeekday(matched[2])
		if ok {
			return resolveWeekdayDate(now, prefix, target), true
		}
	}

	today := startOfDay(now)
	switch {
	case strings.Contains(value, "大后天"):
		return today.AddDate(0, 0, 3), true
	case strings.Contains(value, "后天"):
		return today.AddDate(0, 0, 2), true
	case strings.Contains(value, "明天") || strings.Contains(value, "明日"):
		return today.AddDate(0, 0, 1), true
	case strings.Contains(value, "今天") || strings.Contains(value, "今日") || strings.Contains(value, "今晚") || strings.Contains(value, "今早") || strings.Contains(value, "今晨"):
		return today, true
	case strings.Contains(value, "昨天") || strings.Contains(value, "昨日"):
		return today.AddDate(0, 0, -1), true
	default:
		return time.Time{}, false
	}
}

func extractClock(value string) (int, int, bool, error) {
	hour := 0
	minute := 0
	hasClock := false

	if matched := clockHMRegex.FindStringSubmatch(value); len(matched) == 3 {
		h, errH := strconv.Atoi(matched[1])
		m, errM := strconv.Atoi(matched[2])
		if errH != nil || errM != nil {
			return 0, 0, true, fmt.Errorf("deadline_at 时间解析失败: %s", value)
		}
		hour = h
		minute = m
		hasClock = true
	} else if matched := clockCNRegex.FindStringSubmatch(value); len(matched) >= 2 {
		h, errH := strconv.Atoi(matched[1])
		if errH != nil {
			return 0, 0, true, fmt.Errorf("deadline_at 时间解析失败: %s", value)
		}
		hour = h
		minute = 0
		hasClock = true
		if len(matched) >= 3 {
			if matched[2] == "半" {
				minute = 30
			} else if len(matched) >= 4 && strings.TrimSpace(matched[3]) != "" {
				m, errM := strconv.Atoi(strings.TrimSpace(matched[3]))
				if errM != nil {
					return 0, 0, true, fmt.Errorf("deadline_at 时间解析失败: %s", value)
				}
				minute = m
			}
		}
	}

	if !hasClock {
		return 0, 0, false, nil
	}

	if isPMHint(value) && hour < 12 {
		hour += 12
	}
	if isNoonHint(value) && hour >= 1 && hour <= 10 {
		hour += 12
	}
	if strings.Contains(value, "凌晨") && hour == 12 {
		hour = 0
	}

	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, true, fmt.Errorf("deadline_at 时间超出范围: %s", value)
	}
	return hour, minute, true, nil
}

func defaultClockByHint(value string) (int, int) {
	switch {
	case strings.Contains(value, "凌晨"):
		return 1, 0
	case strings.Contains(value, "早上") || strings.Contains(value, "早晨") || strings.Contains(value, "上午") || strings.Contains(value, "今早") || strings.Contains(value, "明早"):
		return 9, 0
	case strings.Contains(value, "中午"):
		return 12, 0
	case strings.Contains(value, "下午"):
		return 15, 0
	case strings.Contains(value, "晚上") || strings.Contains(value, "今晚") || strings.Contains(value, "傍晚") || strings.Contains(value, "夜里"):
		return 20, 0
	default:
		return 23, 59
	}
}

func isPMHint(value string) bool {
	return strings.Contains(value, "下午") || strings.Contains(value, "晚上") || strings.Contains(value, "今晚") || strings.Contains(value, "傍晚")
}

func isNoonHint(value string) bool {
	return strings.Contains(value, "中午")
}

func startOfDay(t time.Time) time.Time {
	loc := t.Location()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func isValidDate(year, month, day int) bool {
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return false
	}
	candidate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return candidate.Year() == year && int(candidate.Month()) == month && candidate.Day() == day
}

func toWeekday(chinese string) (time.Weekday, bool) {
	switch chinese {
	case "一":
		return time.Monday, true
	case "二":
		return time.Tuesday, true
	case "三":
		return time.Wednesday, true
	case "四":
		return time.Thursday, true
	case "五":
		return time.Friday, true
	case "六":
		return time.Saturday, true
	case "日", "天":
		return time.Sunday, true
	default:
		return time.Sunday, false
	}
}

func resolveWeekdayDate(now time.Time, prefix string, target time.Weekday) time.Time {
	today := startOfDay(now)
	weekdayOffset := (int(today.Weekday()) + 6) % 7
	weekStart := today.AddDate(0, 0, -weekdayOffset)
	targetOffset := (int(target) + 6) % 7
	candidateThisWeek := weekStart.AddDate(0, 0, targetOffset)

	switch {
	case strings.HasPrefix(prefix, "下"):
		return candidateThisWeek.AddDate(0, 0, 7)
	case strings.HasPrefix(prefix, "本"), strings.HasPrefix(prefix, "这"):
		return candidateThisWeek
	default:
		if candidateThisWeek.Before(today) {
			return candidateThisWeek.AddDate(0, 0, 7)
		}
		return candidateThisWeek
	}
}
