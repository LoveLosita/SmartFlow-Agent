package quicknote

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
	// 1. ResolveUserID 为空会导致工具无法绑定当前用户，必须提前失败。
	if d.ResolveUserID == nil {
		return errors.New("quick note tool deps: ResolveUserID is nil")
	}
	// 2. CreateTask 为空说明没有真实写库实现，工具无法完成核心职责。
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
	// 1. 启动期做依赖校验，尽早暴露 wiring 问题，避免运行时才 panic。
	if err := deps.validate(); err != nil {
		return nil, err
	}

	// 2. 通过 InferTool 把 Go 函数声明成“模型可调用工具”。
	//    该闭包函数是工具的真实执行体，后续所有参数校验都在这里兜底。
	createTaskTool, err := toolutils.InferTool(
		ToolNameQuickNoteCreateTask,
		ToolDescQuickNoteCreateTask,
		func(ctx context.Context, input *QuickNoteCreateTaskToolInput) (*QuickNoteCreateTaskToolOutput, error) {
			// 2.1 防御式检查：工具调用参数不能为 nil。
			if input == nil {
				return nil, errors.New("工具参数不能为空")
			}

			// 2.2 标题与优先级是写库硬条件，必须先校验。
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

			// 2.3 user_id 一律来自鉴权上下文，不信任模型侧入参，防止越权写别人的任务。
			userID, err := deps.ResolveUserID(ctx)
			if err != nil {
				return nil, fmt.Errorf("解析用户身份失败: %w", err)
			}
			if userID <= 0 {
				return nil, fmt.Errorf("非法 user_id=%d", userID)
			}

			// 2.4 走业务层写库。
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

			// 2.5 结果归一化：优先使用业务层返回值，其次回退到入参，保证输出稳定可读。
			finalTitle := title
			if strings.TrimSpace(result.Title) != "" {
				finalTitle = strings.TrimSpace(result.Title)
			}

			finalPriority := input.PriorityGroup
			if IsValidTaskPriority(result.PriorityGroup) {
				finalPriority = result.PriorityGroup
			}

			// 2.6 截止时间输出统一为 RFC3339，便于跨系统传输与调试。
			deadlineStr := ""
			if result.DeadlineAt != nil {
				deadlineStr = result.DeadlineAt.In(quickNoteLocation()).Format(time.RFC3339)
			} else if deadline != nil {
				deadlineStr = deadline.In(quickNoteLocation()).Format(time.RFC3339)
			}

			// 2.7 组装给模型的结构化结果，包含可直接面向用户的 message 草稿。
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

	// 3. Tools 给执行节点使用，ToolInfos 给模型注册 schema 使用，二者都要返回。
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
	// 按工具列表顺序提取 ToolInfo，确保“tools[idx] <-> infos[idx]”一一对应。
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
	// 1. 先做标点与空白归一化，避免中文输入噪声影响解析。
	value := normalizeDeadlineInput(raw)
	if value == "" {
		// 2. 空字符串合法，表示任务无截止时间。
		return nil, nil
	}

	// 3. 统一按“严格模式”解析：给了时间就必须成功解析。
	deadline, hasHint, err := parseOptionalDeadlineFromText(value, quickNoteNowToMinute())
	if err != nil {
		return nil, err
	}
	if deadline == nil {
		// 4. 区分“无时间线索”和“有线索但不支持”，返回更准确错误信息。
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
	// 场景：模型已给出 deadline_at，需要基于同一 requestNow 再次硬校验。
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
	// 场景：解析用户原始句子时，允许“没给时间”，但不允许“给了错误时间却静默通过”。
	value := normalizeDeadlineInput(raw)
	if value == "" {
		return nil, false, nil
	}

	deadline, hasHint, err := parseOptionalDeadlineFromText(value, now)
	if err != nil {
		if hasHint {
			// 有时间线索 + 解析失败：上层应明确提示用户改时间格式。
			return nil, true, err
		}
		// 无明显时间线索：按“未提供时间”处理。
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

	// 1. 统一时区与时间基准，保证相对时间可重复计算。
	loc := quickNoteLocation()
	now = now.In(loc)
	hasHint := hasDeadlineHint(value)

	// 2. 先尝试绝对时间（优先级更高，歧义更小）。
	if abs, ok := tryParseAbsoluteDeadline(value, loc); ok {
		return abs, true, nil
	}

	// 3. 再尝试相对时间（明天/下周一/今晚）。
	if rel, recognized, err := tryParseRelativeDeadline(value, now, loc); recognized {
		if err != nil {
			return nil, true, err
		}
		return rel, true, nil
	}

	// 4. 到这里仍失败时，根据 hasHint 决定返回“软失败”还是“硬失败”。
	if hasHint {
		return nil, true, fmt.Errorf("deadline_at 格式不支持: %s", value)
	}
	return nil, false, nil
}

// normalizeDeadlineInput 把中文标点和空白先归一化，降低格式解析的噪声。
func normalizeDeadlineInput(raw string) string {
	// 先 trim，避免纯空格输入影响后续逻辑。
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	// 将中文标点统一成英文形态，降低正则和 layout 解析复杂度。
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
	// 1. 先用结构化正则快速判断（时间格式、日期格式、周几格式）。
	if quickNoteClockHMRegex.MatchString(value) ||
		quickNoteClockCNRegex.MatchString(value) ||
		quickNoteYMDRegex.MatchString(value) ||
		quickNoteMDRegex.MatchString(value) ||
		quickNoteDateSepRegex.MatchString(value) ||
		quickNoteWeekdayRegex.MatchString(value) {
		return true
	}
	// 2. 再用词元判断“明天/今晚”等语义线索。
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
	// 逐个 layout 尝试，命中即返回。
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

		// Date-only 输入（例如 2026-03-20）默认补到 23:59。
		if _, dateOnly := quickNoteDateOnlyLayouts[layout]; dateOnly {
			t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 0, 0, loc)
		} else {
			// 非 date-only 则统一清零秒级，保持分钟粒度一致。
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
	// 1. 先确定“哪一天”。
	baseDate, recognized := inferBaseDate(value, now, loc)
	if !recognized {
		return nil, false, nil
	}

	// 2. 再解析“几点几分”，若缺失则按语义默认时刻兜底。
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
	// 1) yyyy年MM月dd日
	if matched := quickNoteYMDRegex.FindStringSubmatch(value); len(matched) == 4 {
		year, _ := strconv.Atoi(matched[1])
		month, _ := strconv.Atoi(matched[2])
		day, _ := strconv.Atoi(matched[3])
		if isValidDate(year, month, day) {
			return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc), true
		}
	}

	// 2) MM月dd日（自动推断年份：若今年已过则滚到明年）
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

	// 3) 本周/下周 + 周几
	if matched := quickNoteWeekdayRegex.FindStringSubmatch(value); len(matched) == 3 {
		prefix := matched[1]
		target, ok := toWeekday(matched[2])
		if ok {
			return resolveWeekdayDate(now, prefix, target), true
		}
	}

	// 4) 今天/明天/后天/大后天/昨天等相对词
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
	// hour/minute 最终会用于 time.Date，需要先做范围约束。
	hour := 0
	minute := 0
	hasClock := false

	// 1) 24 小时制：18:30
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
		// 2) 中文时刻：3点 / 3点半 / 3点20分
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
		// 没有显式时刻并不是错误，交给默认时刻策略处理。
		return 0, 0, false, nil
	}

	// 3) 根据“下午/晚上/中午/凌晨”等语义修正 12/24 小时制。
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
	// 没有明确时刻时按中文语义设置一个“可解释的默认值”。
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
	// 下午/晚上/傍晚通常应映射到 12:00 之后。
	return strings.Contains(value, "下午") || strings.Contains(value, "晚上") || strings.Contains(value, "今晚") || strings.Contains(value, "傍晚")
}

func isNoonHint(value string) bool {
	// “中午 1 点”这类表达通常是 13:00 而非 01:00。
	return strings.Contains(value, "中午")
}

func startOfDay(t time.Time) time.Time {
	// 保留原时区，只把时分秒归零。
	loc := t.Location()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func isValidDate(year, month, day int) bool {
	// 先做快速范围筛，再用 time.Date 回填校验闰月闰年和越界日期。
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return false
	}
	candidate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return candidate.Year() == year && int(candidate.Month()) == month && candidate.Day() == day
}

func toWeekday(chinese string) (time.Weekday, bool) {
	// 把中文周几映射到 Go 的 Weekday 枚举。
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
	// 1. 先定位本周周一。
	today := startOfDay(now)
	weekdayOffset := (int(today.Weekday()) + 6) % 7
	weekStart := today.AddDate(0, 0, -weekdayOffset)
	targetOffset := (int(target) + 6) % 7
	candidateThisWeek := weekStart.AddDate(0, 0, targetOffset)

	// 2. 再根据“本周/下周/无前缀”选择最终日期。
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
