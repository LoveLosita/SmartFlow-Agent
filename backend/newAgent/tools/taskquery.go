package newagenttools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

// ==================== 常量 ====================

const (
	// defaultTaskQueryLimit 是任务查询默认返回条数。
	defaultTaskQueryLimit = 5
	// maxTaskQueryLimit 是任务查询允许的最大返回条数，用于限制 LLM 输出范围。
	maxTaskQueryLimit = 20
)

// ==================== 优先级中文映射 ====================

// taskQueryPriorityLabelCN 将象限编号转为中文标签。
//
// 职责边界：
// 1. 只负责 1~4 的合法映射，超出范围返回"未知"。
// 2. 不依赖旧链路 agentmodel.PriorityLabelCN，保持新工具自包含。
func taskQueryPriorityLabelCN(priority int) string {
	switch priority {
	case 1:
		return "重要且紧急"
	case 2:
		return "重要不紧急"
	case 3:
		return "简单不重要"
	case 4:
		return "复杂不重要"
	default:
		return "未知"
	}
}

// ==================== 类型定义 ====================

// TaskQueryDeps 描述任务查询工具所需的外部依赖。
//
// 职责边界：
// 1. QueryTasks 负责真正查库，工具层不直接依赖 DAO；
// 2. UserID 由 execute 节点通过 args["_user_id"] 注入，工具层不自行解析会话身份。
type TaskQueryDeps struct {
	// QueryTasks 将解析后的查询参数传入业务层，返回匹配的任务列表。
	// 调用目的：解耦工具层与 DAO 层，方便测试和替换。
	QueryTasks func(ctx context.Context, userID int, params TaskQueryParams) ([]TaskQueryResult, error)
}

// TaskQueryParams 描述任务查询工具传给业务层的内部查询参数。
//
// 输入输出语义：
// 1. 所有筛选条件均为可选，Quadrant 为 nil 表示不限象限。
// 2. 时间边界为 nil 表示不限时间范围。
type TaskQueryParams struct {
	Quadrant         *int
	SortBy           string // deadline | priority | id
	Order            string // asc | desc
	Limit            int
	IncludeCompleted bool
	Keyword          string
	DeadlineBefore   *time.Time
	DeadlineAfter    *time.Time
}

// TaskQueryResult 描述任务查询工具返回给 LLM 的轻量任务视图。
//
// 职责边界：
// 1. 只承载展示所需字段，避免暴露底层数据库结构。
// 2. JSON 序列化后直接作为工具 observation 返回给 LLM。
type TaskQueryResult struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	PriorityGroup int    `json:"priority_group"`
	PriorityLabel string `json:"priority_label"`
	IsCompleted   bool   `json:"is_completed"`
	DeadlineAt    string `json:"deadline_at,omitempty"`
}

// ==================== 时间解析 ====================

// taskQueryTimeLayouts 支持的时间格式列表，按优先级尝试解析。
var taskQueryTimeLayouts = []string{
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
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

		// 1. 纯日期格式需要根据上下界补齐时分秒，保证时间区间语义正确。
		// 2. 若用户输入"2026-04-20"作为上界，意图是"截止到那天结束"，
		//    所以补 23:59:59；作为下界则补 00:00:00。
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

// formatTaskQueryTime 将内部时间格式化为给模型展示的分钟级文本。
func formatTaskQueryTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.In(time.Local).Format("2006-01-02 15:04")
}

// ==================== 工具 Handler ====================

// NewTaskQueryToolHandler 创建 query_tasks 工具的 handler 闭包。
//
// 职责边界：
// 1. 负责参数校验、时间解析、调 deps 查库、组装返回；
// 2. 不负责 LLM 交互和会话管理。
// 3. state 参数忽略——任务查询不需要 ScheduleState，已注册到 scheduleFreeTools。
func NewTaskQueryToolHandler(deps TaskQueryDeps) ToolHandler {
	return func(state *schedule.ScheduleState, args map[string]any) string {
		_ = state

		// 1. 提取 _user_id（由 execute 节点在调用前注入）。
		userID := 0
		if uid, ok := args["_user_id"].(int); ok {
			userID = uid
		}
		if userID <= 0 {
			return "工具调用失败：无法识别用户身份。"
		}

		// 2. 提取并校验查询参数。
		params, err := extractTaskQueryParams(args)
		if err != nil {
			return fmt.Sprintf("工具调用失败：%s", err)
		}

		// 3. 调用依赖查库。
		results, err := deps.QueryTasks(context.Background(), userID, params)
		if err != nil {
			return fmt.Sprintf("工具调用失败：查询任务时出错（%s）。", err)
		}

		// 4. 为每条结果填充优先级中文标签。
		for i := range results {
			results[i].PriorityLabel = taskQueryPriorityLabelCN(results[i].PriorityGroup)
		}

		// 5. 返回结构化 JSON。
		if len(results) == 0 {
			return `{"total":0,"items":[],"message":"当前没有匹配的任务。"}`
		}

		output := struct {
			Total   int               `json:"total"`
			Items   []TaskQueryResult `json:"items"`
			Message string            `json:"message"`
		}{
			Total:   len(results),
			Items:   results,
			Message: fmt.Sprintf("找到 %d 条匹配任务。", len(results)),
		}

		jsonBytes, marshalErr := json.Marshal(output)
		if marshalErr != nil {
			// JSON 序列化失败时降级为纯文本，确保 LLM 仍能拿到关键信息。
			return fmt.Sprintf("找到 %d 条匹配任务。", len(results))
		}
		return string(jsonBytes)
	}
}

// extractTaskQueryParams 从 args 提取并校验任务查询参数。
//
// 步骤说明：
// 1. 先准备默认值，保证空参数也能执行一次合理查询。
// 2. 再校验象限、排序、条数和时间区间，阻止非法参数下沉到业务层。
// 3. 若上下界冲突，则直接返回错误。
func extractTaskQueryParams(args map[string]any) (TaskQueryParams, error) {
	params := TaskQueryParams{
		SortBy:           "deadline",
		Order:            "asc",
		Limit:            defaultTaskQueryLimit,
		IncludeCompleted: false,
	}

	// 2.1 象限：1~4，超出范围拒绝。
	if v, ok := args["quadrant"]; ok {
		switch val := v.(type) {
		case float64:
			q := int(val)
			if q < 1 || q > 4 {
				return TaskQueryParams{}, fmt.Errorf("quadrant=%d 非法，必须在 1~4", q)
			}
			params.Quadrant = &q
		case int:
			if val < 1 || val > 4 {
				return TaskQueryParams{}, fmt.Errorf("quadrant=%d 非法，必须在 1~4", val)
			}
			params.Quadrant = &val
		}
	}

	// 2.2 排序字段：仅支持 deadline/priority/id。
	if v, ok := args["sort_by"].(string); ok {
		sortBy := strings.ToLower(strings.TrimSpace(v))
		if sortBy != "" {
			switch sortBy {
			case "deadline", "priority", "id":
				params.SortBy = sortBy
			default:
				return TaskQueryParams{}, fmt.Errorf("sort_by=%s 非法，仅支持 deadline|priority|id", sortBy)
			}
		}
	}

	// 2.3 排序方向：仅支持 asc/desc。
	if v, ok := args["order"].(string); ok {
		order := strings.ToLower(strings.TrimSpace(v))
		if order != "" {
			switch order {
			case "asc", "desc":
				params.Order = order
			default:
				return TaskQueryParams{}, fmt.Errorf("order=%s 非法，仅支持 asc|desc", order)
			}
		}
	}

	// 2.4 条数：默认 5，上限 20。
	if v, ok := args["limit"]; ok {
		switch val := v.(type) {
		case float64:
			params.Limit = int(val)
		case int:
			params.Limit = val
		}
	}
	if params.Limit <= 0 {
		params.Limit = defaultTaskQueryLimit
	}
	if params.Limit > maxTaskQueryLimit {
		params.Limit = maxTaskQueryLimit
	}

	// 2.5 是否包含已完成任务。
	if v, ok := args["include_completed"]; ok {
		switch val := v.(type) {
		case bool:
			params.IncludeCompleted = val
		}
	}

	// 2.6 关键词。
	if v, ok := args["keyword"].(string); ok {
		params.Keyword = strings.TrimSpace(v)
	}

	// 2.7 时间边界解析，解析失败直接报错，避免查出无意义的结果。
	beforeRaw, _ := args["deadline_before"].(string)
	before, err := parseTaskQueryBoundaryTime(beforeRaw, true)
	if err != nil {
		return TaskQueryParams{}, fmt.Errorf("deadline_before 格式错误: %s", err)
	}
	params.DeadlineBefore = before

	afterRaw, _ := args["deadline_after"].(string)
	after, err := parseTaskQueryBoundaryTime(afterRaw, false)
	if err != nil {
		return TaskQueryParams{}, fmt.Errorf("deadline_after 格式错误: %s", err)
	}
	params.DeadlineAfter = after

	// 2.8 时间区间合法性校验：下界不能晚于上界。
	if params.DeadlineBefore != nil && params.DeadlineAfter != nil &&
		params.DeadlineAfter.After(*params.DeadlineBefore) {
		return TaskQueryParams{}, fmt.Errorf("deadline_after 不能晚于 deadline_before")
	}

	return params, nil
}
