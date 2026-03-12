package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	// ToolNameQuickNoteCreateTask 是“AI随口记”写库工具的标准名称。
	// 该名称会直接暴露给大模型，因此建议保持稳定，避免后续提示词和历史上下文失配。
	ToolNameQuickNoteCreateTask = "quick_note_create_task"
	// ToolDescQuickNoteCreateTask 是工具的简要职责说明。
	ToolDescQuickNoteCreateTask = "把用户随口提到的事项落库为任务，支持可选截止时间与优先级"
)

var (
	// quickNoteDeadlineLayouts 是“绝对时间”白名单格式。
	// 只要命中任意一个 layout，就会被归一化为分钟级时间并进入写库流程。
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

	// 正则区：
	// 1) 用于解析明确时间表达；
	// 2) 用于“是否存在时间线索”的判定（即使格式错误，也会触发校验失败而非静默忽略）。
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

// QuickNoteToolDeps 描述“随口记工具包”需要的外部依赖。
// 这里采用函数注入的方式，避免 agent 包和 service/dao 强耦合，后续更容易演进为 mock 测试或多实现切换。
type QuickNoteToolDeps struct {
	// ResolveUserID 从上下文中解析当前登录用户 ID。
	ResolveUserID func(ctx context.Context) (int, error)
	// CreateTask 执行真实写库动作。
	CreateTask func(ctx context.Context, req QuickNoteCreateTaskRequest) (*QuickNoteCreateTaskResult, error)
}

func (d QuickNoteToolDeps) validate() error {
	if d.ResolveUserID == nil {
		return errors.New("quick note tool deps: ResolveUserID is nil")
	}
	if d.CreateTask == nil {
		return errors.New("quick note tool deps: CreateTask is nil")
	}
	return nil
}

// QuickNoteToolBundle 是随口记工具集合的打包结果。
// - Tools: 给 ToolsNode 使用
// - ToolInfos: 给 ChatModel 绑定工具 schema 使用
// 两者分开返回，可以适配你后面用 chain、graph、react 的不同挂载姿势。
type QuickNoteToolBundle struct {
	Tools     []tool.BaseTool
	ToolInfos []*schema.ToolInfo
}

// QuickNoteCreateTaskRequest 是工具层到业务层的内部请求结构。
// 与模型输入解耦，避免模型字段变化直接影响业务签名。
type QuickNoteCreateTaskRequest struct {
	UserID        int
	Title         string
	PriorityGroup int
	DeadlineAt    *time.Time
}

// QuickNoteCreateTaskResult 是业务层返回给工具层的结构化结果。
type QuickNoteCreateTaskResult struct {
	TaskID        int
	Title         string
	PriorityGroup int
	DeadlineAt    *time.Time
}

// QuickNoteCreateTaskToolInput 是提供给大模型的工具参数定义。
// 注意：user_id 不对模型暴露，统一从鉴权上下文提取，避免越权写入。
type QuickNoteCreateTaskToolInput struct {
	Title string `json:"title" jsonschema:"required,description=任务标题，简洁明确"`
	// PriorityGroup 使用 1~4，和后端 tasks.priority 保持一致。
	PriorityGroup int `json:"priority_group" jsonschema:"required,enum=1,enum=2,enum=3,enum=4,description=优先级分组(1重要且紧急,2重要不紧急,3简单不重要,4不简单不重要)"`
	// DeadlineAt 支持绝对时间与常见相对时间（如明天/后天/下周一/今晚），内部会归一化为绝对时间。
	DeadlineAt string `json:"deadline_at,omitempty" jsonschema:"description=可选截止时间，支持RFC3339、yyyy-MM-dd HH:mm:ss、yyyy-MM-dd HH:mm 以及常见中文相对时间"`
}

// QuickNoteCreateTaskToolOutput 是返回给大模型的工具结果。
// 该结构可直接给模型用于“向用户解释已记录到哪个优先级”。
type QuickNoteCreateTaskToolOutput struct {
	TaskID        int    `json:"task_id"`
	Title         string `json:"title"`
	PriorityGroup int    `json:"priority_group"`
	PriorityLabel string `json:"priority_label"`
	DeadlineAt    string `json:"deadline_at,omitempty"`
	Message       string `json:"message"`
}

// BuildQuickNoteToolBundle 构建“AI随口记”工具包。
// 这是 agent 目录给上层编排层（chain/graph/react）提供的统一入口。
func BuildQuickNoteToolBundle(ctx context.Context, deps QuickNoteToolDeps) (*QuickNoteToolBundle, error) {
	if err := deps.validate(); err != nil {
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
			if !IsValidTaskPriority(input.PriorityGroup) {
				return nil, fmt.Errorf("priority_group=%d 非法，必须在 1~4", input.PriorityGroup)
			}

			// 这里对 deadline_at 做“强校验”：
			// - 空值允许（代表没有截止时间）；
			// - 非空但无法解析直接报错，避免把有问题的时间静默写成 NULL。
			deadline, err := parseOptionalDeadline(input.DeadlineAt)
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
				UserID:        userID,
				Title:         title,
				PriorityGroup: input.PriorityGroup,
				DeadlineAt:    deadline,
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
			if IsValidTaskPriority(result.PriorityGroup) {
				finalPriority = result.PriorityGroup
			}

			deadlineStr := ""
			if result.DeadlineAt != nil {
				deadlineStr = result.DeadlineAt.In(quickNoteLocation()).Format(time.RFC3339)
			} else if deadline != nil {
				deadlineStr = deadline.In(quickNoteLocation()).Format(time.RFC3339)
			}

			return &QuickNoteCreateTaskToolOutput{
				TaskID:        result.TaskID,
				Title:         finalTitle,
				PriorityGroup: finalPriority,
				PriorityLabel: PriorityLabelCN(finalPriority),
				DeadlineAt:    deadlineStr,
				Message:       fmt.Sprintf("已记录：%s（%s）", finalTitle, PriorityLabelCN(finalPriority)),
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

func collectToolInfos(ctx context.Context, tools []tool.BaseTool) ([]*schema.ToolInfo, error) {
	infos := make([]*schema.ToolInfo, 0, len(tools))
	for _, t := range tools {
		info, err := t.Info(ctx)
		if err != nil {
			return nil, fmt.Errorf("读取工具信息失败: %w", err)
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// parseOptionalDeadline 解析工具输入中的可选截止时间。
// 该入口用于“工具参数强校验”：只要调用方给了非空 deadline_at，就必须能被解析。
func parseOptionalDeadline(raw string) (*time.Time, error) {
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

// parseOptionalDeadlineWithNow 在给定时间基准下解析 deadline。
// 该函数保持“严格模式”：非空字符串无法解析时会直接返回 error。
func parseOptionalDeadlineWithNow(raw string, now time.Time) (*time.Time, error) {
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
// 返回值说明：
// - deadline != nil：成功解析出时间；
// - hasHint=false 且 err=nil：文本里没有明显时间线索，应视为“用户没给时间”；
// - hasHint=true 且 err!=nil：用户给了时间但格式非法，应提示用户修正，不应落库。
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

// parseOptionalDeadlineFromText 是内部通用解析器。
// 解析顺序：
// 1) 绝对时间（明确年月日时分）；
// 2) 相对时间（明天/下周一/今晚）；
// 3) 若识别到时间线索但仍失败，返回 hasHint=true + error，交给上层决定是否拦截。
func parseOptionalDeadlineFromText(value string, now time.Time) (*time.Time, bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, false, nil
	}

	loc := quickNoteLocation()
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

// normalizeDeadlineInput 把中文标点和空白先归一化，降低格式解析的噪声。
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

// hasDeadlineHint 判断文本里是否存在“时间相关线索”。
// 该函数的意义是区分两种情况：
// 1) 用户根本没给时间（允许 deadline 为空）；
// 2) 用户给了时间但写错（必须提示修正，不能静默写 NULL）。
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

// tryParseAbsoluteDeadline 尝试按绝对时间格式解析。
// 若只提供日期（无时分），默认归一到当天 23:59，表示“当日截止”。
func tryParseAbsoluteDeadline(value string, loc *time.Location) (*time.Time, bool) {
	for _, layout := range quickNoteDeadlineLayouts {
		var (
			t   time.Time
			err error
		)
		if layout == time.RFC3339 {
			t, err = time.Parse(layout, value)
			if err == nil {
				t = t.In(loc)
			}
		} else {
			t, err = time.ParseInLocation(layout, value, loc)
		}
		if err != nil {
			continue
		}

		if _, dateOnly := quickNoteDateOnlyLayouts[layout]; dateOnly {
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 0, 0, loc)
		} else {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), 0, 0, loc)
		}
		return &t, true
	}
	return nil, false
}

// tryParseRelativeDeadline 尝试解析“相对时间 + 可选时刻”。
// 例子：
// - 明天交报告（默认 23:59）
// - 下周一上午9点开会（解析为下周一 09:00）
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

// inferBaseDate 负责先确定“哪一天”。
// 解析优先级：
// 1) 明确年月日；
// 2) 月日（自动推断年份）；
// 3) 周几表达（本周/下周）；
// 4) 明天/后天/今晚等相对词。
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

// extractClock 从文本提取时刻（时/分）。
// 支持：
// - 24h 表达：18:30
// - 中文表达：3点、3点半、3点20分
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

// defaultClockByHint 当文本只给了“日期/相对日”但没给具体时刻时，按语义兜底。
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
		// 只给了日期没有具体时刻时，默认当天结束前。
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

// resolveWeekdayDate 根据“本周/下周 + 周几”换算目标日期。
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
