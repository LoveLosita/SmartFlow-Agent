package taskquery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	// ToolNameTaskQueryTasks 是“任务查询工具”对模型暴露的标准名称。
	ToolNameTaskQueryTasks = "query_tasks"
	// ToolDescTaskQueryTasks 是工具职责说明，给模型理解参数语义。
	ToolDescTaskQueryTasks = "按象限/关键字/截止时间筛选并排序任务，返回结构化任务列表"
)

var (
	// taskQueryTimeLayouts 是任务查询工具允许的时间输入格式白名单。
	taskQueryTimeLayouts = []string{
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
)

// TaskQueryToolDeps 描述任务查询工具依赖的外部能力。
//
// 职责边界：
// 1. QueryTasks 负责真实数据读取；
// 2. 工具层只负责参数校验与结果封装，不直接耦合 DAO 实现。
type TaskQueryToolDeps struct {
	QueryTasks func(ctx context.Context, req TaskQueryRequest) ([]TaskRecord, error)
}

func (d TaskQueryToolDeps) validate() error {
	// 1. 工具没有 QueryTasks 依赖就无法提供任何真实结果，启动时直接失败。
	if d.QueryTasks == nil {
		return errors.New("task query tool deps: QueryTasks is nil")
	}
	return nil
}

// TaskQueryToolBundle 是任务查询工具包输出。
//
// 说明：
// 1. Tools 用于实际执行；
// 2. ToolInfos 用于模型注册工具 schema。
type TaskQueryToolBundle struct {
	Tools     []tool.BaseTool
	ToolInfos []*schema.ToolInfo
}

// TaskQueryRequest 是工具层到业务层的内部查询请求。
//
// 职责边界：
// 1. 只承载“查询条件”，不承载数据库/缓存实现细节；
// 2. UserID 不由模型提供，必须由服务层上下文注入。
type TaskQueryRequest struct {
	UserID           int
	Quadrant         *int
	SortBy           string
	Order            string
	Limit            int
	IncludeCompleted bool
	Keyword          string
	DeadlineBefore   *time.Time
	DeadlineAfter    *time.Time
}

// TaskRecord 是业务层返回给工具层的任务记录。
type TaskRecord struct {
	ID                 int
	Title              string
	PriorityGroup      int
	IsCompleted        bool
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
}

// TaskQueryToolInput 是对模型暴露的工具输入结构。
//
// 参数语义：
// 1. quadrant 可选：1~4；
// 2. sort_by 可选：deadline/priority/id；
// 3. order 可选：asc/desc；
// 4. limit 可选：默认 5，上限 20；
// 5. include_completed 可选：默认 false。
type TaskQueryToolInput struct {
	Quadrant         *int   `json:"quadrant,omitempty" jsonschema:"description=可选象限(1~4)"`
	SortBy           string `json:"sort_by,omitempty" jsonschema:"description=排序字段(deadline|priority|id)"`
	Order            string `json:"order,omitempty" jsonschema:"description=排序方向(asc|desc)"`
	Limit            int    `json:"limit,omitempty" jsonschema:"description=返回条数，默认5，上限20"`
	IncludeCompleted *bool  `json:"include_completed,omitempty" jsonschema:"description=是否包含已完成任务，默认false"`
	Keyword          string `json:"keyword,omitempty" jsonschema:"description=可选标题关键词，模糊匹配"`
	DeadlineBefore   string `json:"deadline_before,omitempty" jsonschema:"description=可选截止上界，支持RFC3339或yyyy-MM-dd HH:mm"`
	DeadlineAfter    string `json:"deadline_after,omitempty" jsonschema:"description=可选截止下界，支持RFC3339或yyyy-MM-dd HH:mm"`
}

// TaskQueryToolOutput 是返回给模型的结构化结果。
type TaskQueryToolOutput struct {
	Total int                   `json:"total"`
	Items []TaskQueryToolRecord `json:"items"`
}

// TaskQueryToolRecord 是单条任务输出结构。
type TaskQueryToolRecord struct {
	ID                 int    `json:"id"`
	Title              string `json:"title"`
	PriorityGroup      int    `json:"priority_group"`
	PriorityLabel      string `json:"priority_label"`
	IsCompleted        bool   `json:"is_completed"`
	DeadlineAt         string `json:"deadline_at,omitempty"`
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty"`
}

// BuildTaskQueryToolBundle 构建任务查询工具包。
//
// 步骤化说明：
// 1. 先校验依赖，确保工具具备真实查询能力；
// 2. 通过 InferTool 声明工具 schema，并在闭包内做全部参数校验；
// 3. 输出 Tools + ToolInfos，供模型与执行器分别使用。
func BuildTaskQueryToolBundle(ctx context.Context, deps TaskQueryToolDeps) (*TaskQueryToolBundle, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	queryTool, err := toolutils.InferTool(
		ToolNameTaskQueryTasks,
		ToolDescTaskQueryTasks,
		func(ctx context.Context, input *TaskQueryToolInput) (*TaskQueryToolOutput, error) {
			// 1. 允许 input 为空，统一按默认参数执行一次查询。
			normalized, normalizeErr := normalizeToolInput(input)
			if normalizeErr != nil {
				return nil, normalizeErr
			}

			// 2. 执行真实查询。
			records, queryErr := deps.QueryTasks(ctx, normalized)
			if queryErr != nil {
				return nil, queryErr
			}

			// 3. 把业务记录映射成模型友好的结构化输出。
			items := make([]TaskQueryToolRecord, 0, len(records))
			for _, record := range records {
				items = append(items, TaskQueryToolRecord{
					ID:                 record.ID,
					Title:              record.Title,
					PriorityGroup:      record.PriorityGroup,
					PriorityLabel:      priorityLabelCN(record.PriorityGroup),
					IsCompleted:        record.IsCompleted,
					DeadlineAt:         formatOptionalTime(record.DeadlineAt),
					UrgencyThresholdAt: formatOptionalTime(record.UrgencyThresholdAt),
				})
			}

			return &TaskQueryToolOutput{
				Total: len(items),
				Items: items,
			}, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("构建任务查询工具失败: %w", err)
	}

	tools := []tool.BaseTool{queryTool}
	infos, err := collectToolInfos(ctx, tools)
	if err != nil {
		return nil, err
	}
	return &TaskQueryToolBundle{
		Tools:     tools,
		ToolInfos: infos,
	}, nil
}

// normalizeToolInput 负责参数清洗、默认值填充与合法性校验。
//
// 失败策略：
// 1. 参数非法直接返回 error，阻止错误查询落到数据层；
// 2. 参数缺失走默认值，优先保证“可用”。
func normalizeToolInput(input *TaskQueryToolInput) (TaskQueryRequest, error) {
	// 1. 先准备默认值，保证“空参数”也能查到结果。
	req := TaskQueryRequest{
		SortBy:           "deadline",
		Order:            "asc",
		Limit:            5,
		IncludeCompleted: false,
	}
	if input == nil {
		return req, nil
	}

	// 2. 象限校验：若提供则必须在 1~4。
	if input.Quadrant != nil {
		if *input.Quadrant < 1 || *input.Quadrant > 4 {
			return TaskQueryRequest{}, fmt.Errorf("quadrant=%d 非法，必须在 1~4", *input.Quadrant)
		}
		quadrant := *input.Quadrant
		req.Quadrant = &quadrant
	}

	// 3. 排序字段校验。
	if strings.TrimSpace(input.SortBy) != "" {
		req.SortBy = strings.ToLower(strings.TrimSpace(input.SortBy))
	}
	switch req.SortBy {
	case "deadline", "priority", "id":
		// 允许字段。
	default:
		return TaskQueryRequest{}, fmt.Errorf("sort_by=%s 非法，仅支持 deadline|priority|id", req.SortBy)
	}

	// 4. 排序方向校验。
	if strings.TrimSpace(input.Order) != "" {
		req.Order = strings.ToLower(strings.TrimSpace(input.Order))
	}
	switch req.Order {
	case "asc", "desc":
		// 允许方向。
	default:
		return TaskQueryRequest{}, fmt.Errorf("order=%s 非法，仅支持 asc|desc", req.Order)
	}

	// 5. limit 校验与上限保护。
	if input.Limit > 0 {
		req.Limit = input.Limit
	}
	if req.Limit > 20 {
		req.Limit = 20
	}
	if req.Limit <= 0 {
		req.Limit = 5
	}

	// 6. include_completed 默认 false；明确传入时才覆盖。
	if input.IncludeCompleted != nil {
		req.IncludeCompleted = *input.IncludeCompleted
	}

	// 7. keyword 清洗：去首尾空格，空串视为未设置。
	req.Keyword = strings.TrimSpace(input.Keyword)

	// 8. 截止时间上下界解析。
	before, err := parseOptionalBoundaryTime(input.DeadlineBefore, true)
	if err != nil {
		return TaskQueryRequest{}, err
	}
	after, err := parseOptionalBoundaryTime(input.DeadlineAfter, false)
	if err != nil {
		return TaskQueryRequest{}, err
	}
	req.DeadlineBefore = before
	req.DeadlineAfter = after

	// 9. 上下界合法性检查：after 不能晚于 before。
	if req.DeadlineBefore != nil && req.DeadlineAfter != nil && req.DeadlineAfter.After(*req.DeadlineBefore) {
		return TaskQueryRequest{}, errors.New("deadline_after 不能晚于 deadline_before")
	}
	return req, nil
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

// parseOptionalBoundaryTime 解析时间上下界。
//
// 参数语义：
// 1. isUpper=true：按“上界”解析，若输入仅日期则补到 23:59；
// 2. isUpper=false：按“下界”解析，若输入仅日期则补到 00:00。
func parseOptionalBoundaryTime(raw string, isUpper bool) (*time.Time, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil
	}

	loc := time.Local
	for _, layout := range taskQueryTimeLayouts {
		var (
			t   time.Time
			err error
		)
		if layout == time.RFC3339 {
			t, err = time.Parse(layout, text)
			if err == nil {
				t = t.In(loc)
			}
		} else {
			t, err = time.ParseInLocation(layout, text, loc)
		}
		if err != nil {
			continue
		}

		// 仅日期输入时，按上下界补齐时分。
		if layout == "2006-01-02" {
			if isUpper {
				t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, loc)
			} else {
				t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
			}
		}
		return &t, nil
	}
	return nil, fmt.Errorf("时间格式不支持: %s", text)
}

func priorityLabelCN(priority int) string {
	switch priority {
	case 1:
		return "重要且紧急"
	case 2:
		return "重要不紧急"
	case 3:
		return "简单不重要"
	case 4:
		return "不简单不重要"
	default:
		return "未知优先级"
	}
}

func formatOptionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(time.Local).Format("2006-01-02 15:04")
}
