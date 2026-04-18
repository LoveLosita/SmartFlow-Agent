package agentnode

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent/model"
	agentshared "github.com/LoveLosita/smartflow/backend/agent/shared"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	// ToolNameQuickNoteCreateTask 是“AI随口记”写库工具的稳定名称。
	ToolNameQuickNoteCreateTask = "quick_note_create_task"
	// ToolDescQuickNoteCreateTask 是给大模型看的工具职责说明。
	ToolDescQuickNoteCreateTask = "把用户随口提到的事项落库为任务，支持可选截止时间与优先级"
)

var (
	quickNoteDeadlineLayouts = []string{
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
	quickNoteDateOnlyLayouts = map[string]struct{}{
		"2006-01-02": {},
		"2006/01/02": {},
		"2006.01.02": {},
	}

	quickNoteClockHMRegex   = regexp.MustCompile(`(\d{1,2})\s*[:：]\s*(\d{1,2})`)
	quickNoteClockCNRegex   = regexp.MustCompile(`(\d{1,2})\s*点\s*(半|(\d{1,2})\s*分?)?`)
	quickNoteYMDRegex       = regexp.MustCompile(`(\d{4})\s*年\s*(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?`)
	quickNoteMDRegex        = regexp.MustCompile(`(\d{1,2})\s*月\s*(\d{1,2})\s*[日号]?`)
	quickNoteDateSepRegex   = regexp.MustCompile(`\d{1,4}\s*[-/.]\s*\d{1,2}(\s*[-/.]\s*\d{1,2})?`)
	quickNoteWeekdayRegex   = regexp.MustCompile(`(下周|下星期|下礼拜|本周|这周|本星期|这星期|周|星期|礼拜)([一二三四五六日天])`)
	quickNoteRelativeTokens = []string{
		"今天", "今日", "今晚", "今早", "今晨", "明天", "明日", "后天", "大后天", "昨天", "昨日",
		"早上", "早晨", "上午", "中午", "下午", "晚上", "傍晚", "夜里", "凌晨",
	}
)

// QuickNoteToolDeps 描述随口记工具所需的外部依赖。
type QuickNoteToolDeps struct {
	ResolveUserID func(ctx context.Context) (int, error)
	CreateTask    func(ctx context.Context, req QuickNoteCreateTaskRequest) (*QuickNoteCreateTaskResult, error)
}

func (d QuickNoteToolDeps) Validate() error {
	if d.ResolveUserID == nil {
		return errors.New("quick note tool deps: ResolveUserID is nil")
	}
	if d.CreateTask == nil {
		return errors.New("quick note tool deps: CreateTask is nil")
	}
	return nil
}

// QuickNoteToolBundle 是随口记工具集合。
type QuickNoteToolBundle struct {
	Tools     []tool.BaseTool
	ToolInfos []*schema.ToolInfo
}

// QuickNoteCreateTaskRequest 是工具层传给业务层的内部请求。
type QuickNoteCreateTaskRequest struct {
	UserID             int
	Title              string
	PriorityGroup      int
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
}

// QuickNoteCreateTaskResult 是业务层回给工具层的结构化结果。
type QuickNoteCreateTaskResult struct {
	TaskID             int
	Title              string
	PriorityGroup      int
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
}

// QuickNoteCreateTaskToolInput 是暴露给模型的工具入参。
type QuickNoteCreateTaskToolInput struct {
	Title string `json:"title" jsonschema:"required,description=任务标题，简洁明确"`
	// PriorityGroup 与 tasks.priority 保持一致，取值 1~4。
	PriorityGroup int `json:"priority_group" jsonschema:"required,enum=1,enum=2,enum=3,enum=4,description=优先级分组(1重要且紧急,2重要不紧急,3简单不重要,4复杂不重要)"`
	// DeadlineAt 支持绝对时间与常见中文相对时间。
	DeadlineAt string `json:"deadline_at,omitempty" jsonschema:"description=可选截止时间，支持RFC3339、yyyy-MM-dd HH:mm:ss、yyyy-MM-dd HH:mm 以及常见中文相对时间"`
	// UrgencyThresholdAt 表示何时自动进入紧急象限。
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty" jsonschema:"description=可选紧急分界时间，支持与deadline_at相同格式"`
}

// QuickNoteCreateTaskToolOutput 是返回给模型的结构化结果。
type QuickNoteCreateTaskToolOutput struct {
	TaskID        int    `json:"task_id"`
	Title         string `json:"title"`
	PriorityGroup int    `json:"priority_group"`
	PriorityLabel string `json:"priority_label"`
	DeadlineAt    string `json:"deadline_at,omitempty"`
	Message       string `json:"message"`
}

// BuildQuickNoteToolBundle 构建随口记工具包。
func BuildQuickNoteToolBundle(ctx context.Context, deps QuickNoteToolDeps) (*QuickNoteToolBundle, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}

	createTaskTool, err := toolutils.InferTool(
		ToolNameQuickNoteCreateTask,
		ToolDescQuickNoteCreateTask,
		func(ctx context.Context, input *QuickNoteCreateTaskToolInput) (*QuickNoteCreateTaskToolOutput, error) {
			if input == nil {
				return nil, errors.New("工具参数不能为空")
			}

			title := strings.TrimSpace(input.Title)
			if title == "" {
				return nil, errors.New("title 不能为空")
			}
			if !agentmodel.IsValidTaskPriority(input.PriorityGroup) {
				return nil, fmt.Errorf("priority_group=%d 非法，必须在 1~4", input.PriorityGroup)
			}

			deadline, err := ParseOptionalDeadline(input.DeadlineAt)
			if err != nil {
				return nil, err
			}
			urgencyThresholdAt, err := ParseOptionalDeadline(input.UrgencyThresholdAt)
			if err != nil {
				return nil, err
			}

			userID, err := deps.ResolveUserID(ctx)
			if err != nil {
				return nil, fmt.Errorf("解析用户身份失败: %w", err)
			}
			if userID <= 0 {
				return nil, fmt.Errorf("非法 user_id=%d", userID)
			}

			result, err := deps.CreateTask(ctx, QuickNoteCreateTaskRequest{
				UserID:             userID,
				Title:              title,
				PriorityGroup:      input.PriorityGroup,
				DeadlineAt:         deadline,
				UrgencyThresholdAt: urgencyThresholdAt,
			})
			if err != nil {
				return nil, err
			}
			if result == nil || result.TaskID <= 0 {
				return nil, errors.New("写入任务后返回结果异常")
			}

			finalTitle := title
			if strings.TrimSpace(result.Title) != "" {
				finalTitle = strings.TrimSpace(result.Title)
			}
			finalPriority := input.PriorityGroup
			if agentmodel.IsValidTaskPriority(result.PriorityGroup) {
				finalPriority = result.PriorityGroup
			}

			deadlineStr := ""
			if result.DeadlineAt != nil {
				deadlineStr = result.DeadlineAt.In(QuickNoteLocation()).Format(time.RFC3339)
			} else if deadline != nil {
				deadlineStr = deadline.In(QuickNoteLocation()).Format(time.RFC3339)
			}

			return &QuickNoteCreateTaskToolOutput{
				TaskID:        result.TaskID,
				Title:         finalTitle,
				PriorityGroup: finalPriority,
				PriorityLabel: agentmodel.PriorityLabelCN(finalPriority),
				DeadlineAt:    deadlineStr,
				Message:       fmt.Sprintf("已记录：%s（%s）", finalTitle, agentmodel.PriorityLabelCN(finalPriority)),
			}, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("构建随口记工具失败: %w", err)
	}

	tools := []tool.BaseTool{createTaskTool}
	infos, err := collectToolInfos(ctx, tools)
	if err != nil {
		return nil, err
	}

	return &QuickNoteToolBundle{
		Tools:     tools,
		ToolInfos: infos,
	}, nil
}

// GetInvokableToolByName 通过工具名提取可执行工具实例。
func GetInvokableToolByName(bundle *QuickNoteToolBundle, name string) (tool.InvokableTool, error) {
	if bundle == nil {
		return nil, errors.New("tool bundle is nil")
	}
	return getInvokableToolByName(bundle.Tools, bundle.ToolInfos, name)
}

// ParseOptionalDeadline 解析工具输入中的可选截止时间。
// 调用目的：新链路 quick_note_create 工具复用旧链路成熟的时间解析能力，支持中文相对时间。
func ParseOptionalDeadline(raw string) (*time.Time, error) {
	value := normalizeDeadlineInput(raw)
	if value == "" {
		return nil, nil
	}

	deadline, hasHint, err := parseOptionalDeadlineFromText(value, quickNoteNowToMinute())
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
// 调用目的：旧链路 intent/priority 节点在已知 now 基准下解析时间，供新链路复用。
func ParseOptionalDeadlineWithNow(raw string, now time.Time) (*time.Time, error) {
	value := normalizeDeadlineInput(raw)
	if value == "" {
		return nil, nil
	}

	deadline, _, err := parseOptionalDeadlineFromText(value, now)
	if err != nil {
		return nil, err
	}
	if deadline == nil {
		return nil, fmt.Errorf("deadline_at 格式不支持: %s", value)
	}
	return deadline, nil
}

// parseOptionalDeadlineFromUserInput 是“用户原句解析”的宽松入口。
func parseOptionalDeadlineFromUserInput(raw string, now time.Time) (*time.Time, bool, error) {
	value := normalizeDeadlineInput(raw)
	if value == "" {
		return nil, false, nil
	}

	deadline, hasHint, err := parseOptionalDeadlineFromText(value, now)
	if err != nil {
		if hasHint {
			return nil, true, err
		}
		return nil, false, nil
	}
	if deadline == nil {
		if hasHint {
			return nil, true, fmt.Errorf("deadline_at 无法解析: %s", value)
		}
		return nil, false, nil
	}
	return deadline, true, nil
}

// parseOptionalDeadlineFromText 是内部通用时间解析器。
func parseOptionalDeadlineFromText(value string, now time.Time) (*time.Time, bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, false, nil
	}

	loc := QuickNoteLocation()
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
	if quickNoteClockHMRegex.MatchString(value) ||
		quickNoteClockCNRegex.MatchString(value) ||
		quickNoteYMDRegex.MatchString(value) ||
		quickNoteMDRegex.MatchString(value) ||
		quickNoteDateSepRegex.MatchString(value) ||
		quickNoteWeekdayRegex.MatchString(value) {
		return true
	}
	for _, token := range quickNoteRelativeTokens {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func tryParseAbsoluteDeadline(value string, loc *time.Location) (*time.Time, bool) {
	for _, layout := range quickNoteDeadlineLayouts {
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

		if _, dateOnly := quickNoteDateOnlyLayouts[layout]; dateOnly {
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
	if matched := quickNoteYMDRegex.FindStringSubmatch(value); len(matched) == 4 {
		year, _ := strconv.Atoi(matched[1])
		month, _ := strconv.Atoi(matched[2])
		day, _ := strconv.Atoi(matched[3])
		if isValidDate(year, month, day) {
			return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc), true
		}
	}

	if matched := quickNoteMDRegex.FindStringSubmatch(value); len(matched) == 3 {
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

	if matched := quickNoteWeekdayRegex.FindStringSubmatch(value); len(matched) == 3 {
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

	if matched := quickNoteClockHMRegex.FindStringSubmatch(value); len(matched) == 3 {
		h, errH := strconv.Atoi(matched[1])
		m, errM := strconv.Atoi(matched[2])
		if errH != nil || errM != nil {
			return 0, 0, true, fmt.Errorf("deadline_at 时间解析失败: %s", value)
		}
		hour = h
		minute = m
		hasClock = true
	} else if matched := quickNoteClockCNRegex.FindStringSubmatch(value); len(matched) >= 2 {
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

// QuickNoteLocation 返回随口记使用的时区（Asia/Shanghai）。
// 调用目的：新链路 quick_note_create 工具格式化时间输出时复用。
func QuickNoteLocation() *time.Location {
	loc, err := time.LoadLocation(agentmodel.QuickNoteTimezoneName)
	if err != nil {
		return time.Local
	}
	return loc
}

func quickNoteNowToMinute() time.Time {
	return agentshared.NowToMinute()
}

func formatQuickNoteTimeToMinute(t time.Time) string {
	return agentshared.FormatMinute(t.In(QuickNoteLocation()))
}
