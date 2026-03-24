package agentnode

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
)

const (
	ToolNameTaskQueryTasks = "query_tasks"
	ToolDescTaskQueryTasks = "按象限、关键词、截止时间筛选并排序任务，返回结构化任务列表"
)

var taskQueryTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
}

// TaskQueryToolDeps 描述任务查询工具依赖的外部查询能力。
//
// 职责边界：
// 1. QueryTasks 负责读取真实任务数据。
// 2. 工具层只负责参数校验、归一化和结构化输出，不直接耦合 DAO。
type TaskQueryToolDeps struct {
	QueryTasks func(ctx context.Context, req TaskQueryRequest) ([]TaskQueryTaskRecord, error)
}

// Validate 负责校验任务查询工具依赖是否齐全。
func (d TaskQueryToolDeps) Validate() error {
	if d.QueryTasks == nil {
		return errors.New("task query tool deps: QueryTasks is nil")
	}
	return nil
}

// TaskQueryToolBundle 同时返回工具实例和工具元信息。
//
// 职责边界：
// 1. Tools 给执行节点使用。
// 2. ToolInfos 给模型注册 schema 使用。
type TaskQueryToolBundle struct {
	Tools     []tool.BaseTool
	ToolInfos []*schema.ToolInfo
}

// TaskQueryRequest 是工具层传给业务层的内部查询请求。
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

// TaskQueryTaskRecord 是业务层返回给工具层的任务记录。
type TaskQueryTaskRecord struct {
	ID                 int
	Title              string
	PriorityGroup      int
	IsCompleted        bool
	DeadlineAt         *time.Time
	UrgencyThresholdAt *time.Time
}

// TaskQueryToolInput 是暴露给大模型的工具入参。
type TaskQueryToolInput struct {
	Quadrant         *int   `json:"quadrant,omitempty" jsonschema:"description=可选象限(1~4)"`
	SortBy           string `json:"sort_by,omitempty" jsonschema:"description=排序字段(deadline|priority|id)"`
	Order            string `json:"order,omitempty" jsonschema:"description=排序方向(asc|desc)"`
	Limit            int    `json:"limit,omitempty" jsonschema:"description=返回条数，默认5，上限20"`
	IncludeCompleted *bool  `json:"include_completed,omitempty" jsonschema:"description=是否包含已完成任务，默认false"`
	Keyword          string `json:"keyword,omitempty" jsonschema:"description=可选标题关键词，模糊匹配"`
	DeadlineBefore   string `json:"deadline_before,omitempty" jsonschema:"description=可选截止时间上界，支持RFC3339或yyyy-MM-dd HH:mm"`
	DeadlineAfter    string `json:"deadline_after,omitempty" jsonschema:"description=可选截止时间下界，支持RFC3339或yyyy-MM-dd HH:mm"`
}

// TaskQueryToolOutput 是返回给模型的结构化结果。
type TaskQueryToolOutput struct {
	Total int                        `json:"total"`
	Items []agentmodel.TaskQueryItem `json:"items"`
}

// BuildTaskQueryToolBundle 负责构建任务查询工具包。
//
// 步骤说明：
// 1. 先校验依赖是否完整，避免生成一个运行时必定失败的工具。
// 2. 再把输入归一化成内部请求，调用业务查询函数拿到真实数据。
// 3. 最后把业务记录转换成统一的轻量任务视图，供模型和反思节点复用。
func BuildTaskQueryToolBundle(ctx context.Context, deps TaskQueryToolDeps) (*TaskQueryToolBundle, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}

	queryTool, err := toolutils.InferTool(
		ToolNameTaskQueryTasks,
		ToolDescTaskQueryTasks,
		func(ctx context.Context, input *TaskQueryToolInput) (*TaskQueryToolOutput, error) {
			req, err := normalizeTaskQueryToolInput(input)
			if err != nil {
				return nil, err
			}

			records, err := deps.QueryTasks(ctx, req)
			if err != nil {
				return nil, err
			}

			items := make([]agentmodel.TaskQueryItem, 0, len(records))
			for _, record := range records {
				items = append(items, agentmodel.TaskQueryItem{
					ID:                 record.ID,
					Title:              record.Title,
					PriorityGroup:      record.PriorityGroup,
					PriorityLabel:      agentmodel.PriorityLabelCN(record.PriorityGroup),
					IsCompleted:        record.IsCompleted,
					DeadlineAt:         formatTaskQueryTime(record.DeadlineAt),
					UrgencyThresholdAt: formatTaskQueryTime(record.UrgencyThresholdAt),
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

// GetTaskQueryInvokableToolByName 按工具名提取可执行工具。
func GetTaskQueryInvokableToolByName(bundle *TaskQueryToolBundle, name string) (tool.InvokableTool, error) {
	if bundle == nil {
		return nil, errors.New("task query tool bundle is nil")
	}
	return getInvokableToolByName(bundle.Tools, bundle.ToolInfos, name)
}

// normalizeTaskQueryToolInput 负责参数默认值回填与合法性校验。
//
// 步骤说明：
// 1. 先准备默认值，保证空参数也能执行一次合理查询。
// 2. 再校验象限、排序、条数和时间区间，阻止非法参数下沉到业务层。
// 3. 若上下界冲突，则直接返回错误，避免查出必为空的结果。
func normalizeTaskQueryToolInput(input *TaskQueryToolInput) (TaskQueryRequest, error) {
	req := TaskQueryRequest{
		SortBy:           "deadline",
		Order:            "asc",
		Limit:            agentmodel.DefaultTaskQueryLimit,
		IncludeCompleted: false,
	}
	if input == nil {
		return req, nil
	}

	if input.Quadrant != nil {
		if *input.Quadrant < 1 || *input.Quadrant > 4 {
			return TaskQueryRequest{}, fmt.Errorf("quadrant=%d 非法，必须在 1~4", *input.Quadrant)
		}
		quadrant := *input.Quadrant
		req.Quadrant = &quadrant
	}

	if sortBy := strings.ToLower(strings.TrimSpace(input.SortBy)); sortBy != "" {
		req.SortBy = sortBy
	}
	switch req.SortBy {
	case "deadline", "priority", "id":
	default:
		return TaskQueryRequest{}, fmt.Errorf("sort_by=%s 非法，仅支持 deadline|priority|id", req.SortBy)
	}

	if order := strings.ToLower(strings.TrimSpace(input.Order)); order != "" {
		req.Order = order
	}
	switch req.Order {
	case "asc", "desc":
	default:
		return TaskQueryRequest{}, fmt.Errorf("order=%s 非法，仅支持 asc|desc", req.Order)
	}

	if input.Limit > 0 {
		req.Limit = input.Limit
	}
	if req.Limit > agentmodel.MaxTaskQueryLimit {
		req.Limit = agentmodel.MaxTaskQueryLimit
	}
	if req.Limit <= 0 {
		req.Limit = agentmodel.DefaultTaskQueryLimit
	}

	if input.IncludeCompleted != nil {
		req.IncludeCompleted = *input.IncludeCompleted
	}
	req.Keyword = strings.TrimSpace(input.Keyword)

	before, err := parseTaskQueryBoundaryTime(input.DeadlineBefore, true)
	if err != nil {
		return TaskQueryRequest{}, err
	}
	after, err := parseTaskQueryBoundaryTime(input.DeadlineAfter, false)
	if err != nil {
		return TaskQueryRequest{}, err
	}
	req.DeadlineBefore = before
	req.DeadlineAfter = after
	if req.DeadlineBefore != nil && req.DeadlineAfter != nil && req.DeadlineAfter.After(*req.DeadlineBefore) {
		return TaskQueryRequest{}, errors.New("deadline_after 不能晚于 deadline_before")
	}

	return req, nil
}

// parseTaskQueryBoundaryTime 解析截止时间上下界。
//
// 职责边界：
// 1. isUpper=true 时，纯日期补到当天 23:59:59。
// 2. isUpper=false 时，纯日期补到当天 00:00:00。
// 3. 不支持的格式直接返回错误，由调用方决定是否回退。
func parseTaskQueryBoundaryTime(raw string, isUpper bool) (*time.Time, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return nil, nil
	}

	loc := time.Local
	for _, layout := range taskQueryTimeLayouts {
		var (
			parsed time.Time
			err    error
		)
		if layout == time.RFC3339 {
			parsed, err = time.Parse(layout, text)
			if err == nil {
				parsed = parsed.In(loc)
			}
		} else {
			parsed, err = time.ParseInLocation(layout, text, loc)
		}
		if err != nil {
			continue
		}

		if layout == "2006-01-02" {
			if isUpper {
				parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, loc)
			} else {
				parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, loc)
			}
		}
		return &parsed, nil
	}
	return nil, fmt.Errorf("时间格式不支持: %s", text)
}

// formatTaskQueryTime 负责把内部时间格式化为给模型展示的分钟级文本。
func formatTaskQueryTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(time.Local).Format("2006-01-02 15:04")
}
