package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	agentllm "github.com/LoveLosita/smartflow/backend/agent2/llm"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentprompt "github.com/LoveLosita/smartflow/backend/agent2/prompt"
	agentstream "github.com/LoveLosita/smartflow/backend/agent2/stream"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	TaskQueryGraphNodePlan       = "task_query.plan"
	TaskQueryGraphNodeQuadrant   = "task_query.quadrant"
	TaskQueryGraphNodeTimeAnchor = "task_query.time_anchor"
	TaskQueryGraphNodeQuery      = "task_query.query"
	TaskQueryGraphNodeReflect    = "task_query.reflect"
)

var (
	explicitLimitPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\btop\s*(\d{1,2})\b`),
		regexp.MustCompile(`前\s*(\d{1,2})\s*(个|条|项)?`),
		regexp.MustCompile(`(\d{1,2})\s*(个|条|项)?\s*任务`),
		regexp.MustCompile(`给我\s*(\d{1,2})\s*(个|条|项)?`),
	}
	chineseDigitMap = map[rune]int{
		'一': 1,
		'二': 2,
		'两': 2,
		'三': 3,
		'四': 4,
		'五': 5,
		'六': 6,
		'七': 7,
		'八': 8,
		'九': 9,
		'十': 10,
	}
)

// TaskQueryGraphRunInput 描述一次任务查询图运行需要的依赖。
type TaskQueryGraphRunInput struct {
	Model     *ark.ChatModel
	State     *agentmodel.TaskQueryState
	Deps      TaskQueryToolDeps
	EmitStage func(stage, detail string)
}

// TaskQueryNodes 是任务查询图的节点容器。
//
// 职责边界：
// 1. 负责承接请求级依赖，并向 graph 暴露可直接挂载的方法。
// 2. 不负责 graph 编译、service 接线和持久化。
type TaskQueryNodes struct {
	input     TaskQueryGraphRunInput
	queryTool tool.InvokableTool
	emitStage agentstream.StageEmitter
}

func NewTaskQueryNodes(input TaskQueryGraphRunInput, queryTool tool.InvokableTool) (*TaskQueryNodes, error) {
	if input.Model == nil {
		return nil, fmt.Errorf("task query nodes: model is nil")
	}
	if input.State == nil {
		return nil, fmt.Errorf("task query nodes: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return nil, err
	}
	if queryTool == nil {
		return nil, fmt.Errorf("task query nodes: queryTool is nil")
	}
	return &TaskQueryNodes{
		input:     input,
		queryTool: queryTool,
		emitStage: agentstream.WrapStageEmitter(input.EmitStage),
	}, nil
}

// Plan 负责把用户原话规划成结构化查询计划。
func (n *TaskQueryNodes) Plan(ctx context.Context, st *agentmodel.TaskQueryState) (*agentmodel.TaskQueryState, error) {
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in plan node")
	}

	n.emitStage("task_query.plan.generating", "正在一次性规划查询范围、排序和时间条件。")
	planned, err := agentllm.PlanTaskQuery(ctx, n.input.Model, st.RequestNowText, st.UserMessage)
	if err != nil || planned == nil {
		st.UserGoal = "查询任务"
		st.Plan = defaultTaskQueryPlan()
		return st, nil
	}

	st.UserGoal = strings.TrimSpace(planned.UserGoal)
	if st.UserGoal == "" {
		st.UserGoal = "查询任务"
	}
	st.Plan = normalizeTaskQueryPlan(*planned)

	// 1. 若用户原话里明确指定了返回条数，则以后端识别结果为准。
	// 2. 这样可以避免规划模型漏掉数量要求，或后续反思 patch 意外改写 limit。
	if explicitLimit, found := extractExplicitLimitFromUser(st.UserMessage); found {
		st.ExplicitLimit = explicitLimit
		st.Plan.Limit = explicitLimit
	}
	return st, nil
}

// NormalizeQuadrant 负责把象限参数去重并统一成稳定顺序。
func (n *TaskQueryNodes) NormalizeQuadrant(ctx context.Context, st *agentmodel.TaskQueryState) (*agentmodel.TaskQueryState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in quadrant node")
	}

	n.emitStage("task_query.quadrant.routing", "正在归一化象限筛选范围。")
	st.Plan.Quadrants = normalizeQuadrants(st.Plan.Quadrants)
	return st, nil
}

// AnchorTime 负责把时间文本边界解析成可执行时间对象。
func (n *TaskQueryNodes) AnchorTime(ctx context.Context, st *agentmodel.TaskQueryState) (*agentmodel.TaskQueryState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in time anchor node")
	}

	n.emitStage("task_query.time.anchoring", "正在锁定时间过滤边界。")
	applyTimeAnchorOnPlan(&st.Plan)
	return st, nil
}

// Query 负责真正调用工具查询任务。
func (n *TaskQueryNodes) Query(ctx context.Context, st *agentmodel.TaskQueryState) (*agentmodel.TaskQueryState, error) {
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in query node")
	}

	n.emitStage("task_query.tool.querying", "正在查询任务数据。")
	items, err := n.executePlanByTool(ctx, st.Plan)
	if err != nil {
		st.LastQueryItems = make([]agentmodel.TaskQueryItem, 0)
		st.LastQueryTotal = 0
		st.ReflectReason = "查询工具执行失败"
		return st, nil
	}

	st.LastQueryItems = items
	st.LastQueryTotal = len(items)

	// 1. 如果首轮为空且还没自动放宽过，则做一次可解释的自动放宽。
	// 2. 放宽范围仅限关键词、完成状态、时间边界，不主动改象限与 limit，避免语义漂移。
	if st.LastQueryTotal == 0 && !st.AutoBroadenApplied {
		broadenedPlan, changed := autoBroadenPlan(st.Plan)
		if changed {
			st.AutoBroadenApplied = true
			st.Plan = broadenedPlan
			n.emitStage("task_query.tool.broadened", "首次查询为空，已自动放宽条件再试一次。")
			retryItems, retryErr := n.executePlanByTool(ctx, st.Plan)
			if retryErr == nil {
				st.LastQueryItems = retryItems
				st.LastQueryTotal = len(retryItems)
			}
		}
	}

	return st, nil
}

// Reflect 负责判断当前结果是否满足用户诉求，并决定是否重试。
func (n *TaskQueryNodes) Reflect(ctx context.Context, st *agentmodel.TaskQueryState) (*agentmodel.TaskQueryState, error) {
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in reflect node")
	}

	n.emitStage("task_query.reflecting", "正在判断结果是否贴合你的需求。")
	reflectPrompt := agentprompt.BuildTaskQueryReflectUserPrompt(
		st.RequestNowText,
		st.UserMessage,
		st.UserGoal,
		summarizeTaskQueryPlan(st.Plan),
		st.RetryCount,
		st.MaxReflectRetry,
		summarizeTaskQueryItems(st.LastQueryItems, 6),
	)
	reflectResult, err := agentllm.ReflectTaskQuery(ctx, n.input.Model, reflectPrompt)
	if err != nil || reflectResult == nil {
		st.NeedRetry = false
		st.FinalReply = buildTaskQueryFallbackReply(st.LastQueryItems)
		return st, nil
	}

	st.ReflectReason = strings.TrimSpace(reflectResult.Reason)

	if reflectResult.Satisfied {
		st.NeedRetry = false
		st.FinalReply = buildTaskQueryFinalReply(st.LastQueryItems, st.Plan, strings.TrimSpace(reflectResult.Reply))
		return st, nil
	}

	if reflectResult.NeedRetry && st.RetryCount < st.MaxReflectRetry {
		st.Plan = applyRetryPatch(st.Plan, reflectResult.RetryPatch, st.ExplicitLimit)
		st.RetryCount++
		st.NeedRetry = true
		if reply := strings.TrimSpace(reflectResult.Reply); reply != "" {
			st.FinalReply = reply
		}
		return st, nil
	}

	st.NeedRetry = false
	st.FinalReply = buildTaskQueryFinalReply(st.LastQueryItems, st.Plan, strings.TrimSpace(reflectResult.Reply))
	return st, nil
}

func (n *TaskQueryNodes) NextAfterReflect(ctx context.Context, st *agentmodel.TaskQueryState) (string, error) {
	_ = ctx
	if st != nil && st.NeedRetry {
		return TaskQueryGraphNodeQuery, nil
	}
	return compose.END, nil
}

func (n *TaskQueryNodes) executePlanByTool(ctx context.Context, plan agentmodel.TaskQueryPlan) ([]agentmodel.TaskQueryItem, error) {
	if n.queryTool == nil {
		return nil, fmt.Errorf("task query tool is nil")
	}

	merged := make([]agentmodel.TaskQueryItem, 0, plan.Limit)
	seen := make(map[int]struct{}, plan.Limit*2)

	runOne := func(quadrant *int) error {
		input := TaskQueryToolInput{
			Quadrant:       quadrant,
			SortBy:         plan.SortBy,
			Order:          plan.Order,
			Limit:          plan.Limit,
			Keyword:        plan.Keyword,
			DeadlineBefore: plan.DeadlineBeforeText,
			DeadlineAfter:  plan.DeadlineAfterText,
		}
		includeCompleted := plan.IncludeCompleted
		input.IncludeCompleted = &includeCompleted

		rawInput, err := json.Marshal(input)
		if err != nil {
			return err
		}
		rawOutput, err := n.queryTool.InvokableRun(ctx, string(rawInput))
		if err != nil {
			return err
		}
		parsed, err := agentllm.ParseJSONObject[TaskQueryToolOutput](rawOutput)
		if err != nil {
			return err
		}

		for _, item := range parsed.Items {
			if _, exists := seen[item.ID]; exists {
				continue
			}
			seen[item.ID] = struct{}{}
			merged = append(merged, item)
		}
		return nil
	}

	if len(plan.Quadrants) == 0 {
		if err := runOne(nil); err != nil {
			return nil, err
		}
	} else {
		for _, quadrant := range plan.Quadrants {
			q := quadrant
			if err := runOne(&q); err != nil {
				return nil, err
			}
		}
	}

	sortTaskQueryItems(merged, plan)
	if len(merged) > plan.Limit {
		merged = merged[:plan.Limit]
	}
	return merged, nil
}

func defaultTaskQueryPlan() agentmodel.TaskQueryPlan {
	return agentmodel.TaskQueryPlan{
		SortBy:           "deadline",
		Order:            "asc",
		Limit:            agentmodel.DefaultTaskQueryLimit,
		IncludeCompleted: false,
	}
}

func normalizeTaskQueryPlan(raw agentllm.TaskQueryPlanOutput) agentmodel.TaskQueryPlan {
	plan := defaultTaskQueryPlan()
	plan.Quadrants = normalizeQuadrants(raw.Quadrants)

	if sortBy := strings.ToLower(strings.TrimSpace(raw.SortBy)); sortBy == "deadline" || sortBy == "priority" || sortBy == "id" {
		plan.SortBy = sortBy
	}
	if order := strings.ToLower(strings.TrimSpace(raw.Order)); order == "asc" || order == "desc" {
		plan.Order = order
	}
	if raw.Limit > 0 {
		plan.Limit = raw.Limit
	}
	if plan.Limit > agentmodel.MaxTaskQueryLimit {
		plan.Limit = agentmodel.MaxTaskQueryLimit
	}
	if plan.Limit <= 0 {
		plan.Limit = agentmodel.DefaultTaskQueryLimit
	}
	if raw.IncludeCompleted != nil {
		plan.IncludeCompleted = *raw.IncludeCompleted
	}
	plan.Keyword = strings.TrimSpace(raw.Keyword)
	plan.DeadlineBeforeText = strings.TrimSpace(raw.DeadlineBefore)
	plan.DeadlineAfterText = strings.TrimSpace(raw.DeadlineAfter)
	applyTimeAnchorOnPlan(&plan)
	return plan
}

func normalizeQuadrants(quadrants []int) []int {
	if len(quadrants) == 0 {
		return nil
	}

	seen := make(map[int]struct{}, len(quadrants))
	result := make([]int, 0, len(quadrants))
	for _, quadrant := range quadrants {
		if quadrant < 1 || quadrant > 4 {
			continue
		}
		if _, exists := seen[quadrant]; exists {
			continue
		}
		seen[quadrant] = struct{}{}
		result = append(result, quadrant)
	}

	sort.Ints(result)
	if len(result) == 0 || len(result) == 4 {
		return nil
	}
	return result
}

func applyTimeAnchorOnPlan(plan *agentmodel.TaskQueryPlan) {
	if plan == nil {
		return
	}

	before, errBefore := parseTaskQueryBoundaryTime(plan.DeadlineBeforeText, true)
	after, errAfter := parseTaskQueryBoundaryTime(plan.DeadlineAfterText, false)

	if errBefore != nil {
		plan.DeadlineBefore = nil
		plan.DeadlineBeforeText = ""
	} else {
		plan.DeadlineBefore = before
	}
	if errAfter != nil {
		plan.DeadlineAfter = nil
		plan.DeadlineAfterText = ""
	} else {
		plan.DeadlineAfter = after
	}

	if plan.DeadlineBefore != nil && plan.DeadlineAfter != nil && plan.DeadlineAfter.After(*plan.DeadlineBefore) {
		plan.DeadlineBefore = nil
		plan.DeadlineAfter = nil
		plan.DeadlineBeforeText = ""
		plan.DeadlineAfterText = ""
	}
}

func autoBroadenPlan(plan agentmodel.TaskQueryPlan) (agentmodel.TaskQueryPlan, bool) {
	broadened := plan
	changed := false

	if strings.TrimSpace(broadened.Keyword) != "" {
		broadened.Keyword = ""
		changed = true
	}
	if !broadened.IncludeCompleted {
		broadened.IncludeCompleted = true
		changed = true
	}
	if broadened.DeadlineBefore != nil || broadened.DeadlineAfter != nil || broadened.DeadlineBeforeText != "" || broadened.DeadlineAfterText != "" {
		broadened.DeadlineBefore = nil
		broadened.DeadlineAfter = nil
		broadened.DeadlineBeforeText = ""
		broadened.DeadlineAfterText = ""
		changed = true
	}
	return broadened, changed
}

func applyRetryPatch(plan agentmodel.TaskQueryPlan, patch agentllm.TaskQueryRetryPatch, explicitLimit int) agentmodel.TaskQueryPlan {
	next := plan
	changed := false

	if patch.Quadrants != nil {
		next.Quadrants = normalizeQuadrants(*patch.Quadrants)
		changed = true
	}
	if patch.SortBy != nil {
		sortBy := strings.ToLower(strings.TrimSpace(*patch.SortBy))
		if sortBy == "deadline" || sortBy == "priority" || sortBy == "id" {
			next.SortBy = sortBy
			changed = true
		}
	}
	if patch.Order != nil {
		order := strings.ToLower(strings.TrimSpace(*patch.Order))
		if order == "asc" || order == "desc" {
			next.Order = order
			changed = true
		}
	}
	if patch.Limit != nil && explicitLimit <= 0 {
		limit := *patch.Limit
		if limit <= 0 {
			limit = agentmodel.DefaultTaskQueryLimit
		}
		if limit > agentmodel.MaxTaskQueryLimit {
			limit = agentmodel.MaxTaskQueryLimit
		}
		next.Limit = limit
		changed = true
	}
	if patch.IncludeCompleted != nil {
		next.IncludeCompleted = *patch.IncludeCompleted
		changed = true
	}
	if patch.Keyword != nil {
		next.Keyword = strings.TrimSpace(*patch.Keyword)
		changed = true
	}
	if patch.DeadlineBefore != nil {
		next.DeadlineBeforeText = strings.TrimSpace(*patch.DeadlineBefore)
		changed = true
	}
	if patch.DeadlineAfter != nil {
		next.DeadlineAfterText = strings.TrimSpace(*patch.DeadlineAfter)
		changed = true
	}

	if changed {
		applyTimeAnchorOnPlan(&next)
	}
	if explicitLimit > 0 {
		next.Limit = explicitLimit
	}
	return next
}

func summarizeTaskQueryPlan(plan agentmodel.TaskQueryPlan) string {
	quadrants := "全部象限"
	if len(plan.Quadrants) > 0 {
		parts := make([]string, 0, len(plan.Quadrants))
		for _, quadrant := range plan.Quadrants {
			parts = append(parts, strconv.Itoa(quadrant))
		}
		quadrants = strings.Join(parts, ",")
	}
	return fmt.Sprintf(
		"quadrants=%s sort=%s/%s limit=%d include_completed=%t keyword=%s before=%s after=%s",
		quadrants,
		plan.SortBy,
		plan.Order,
		plan.Limit,
		plan.IncludeCompleted,
		emptyToDash(plan.Keyword),
		emptyToDash(plan.DeadlineBeforeText),
		emptyToDash(plan.DeadlineAfterText),
	)
}

func summarizeTaskQueryItems(items []agentmodel.TaskQueryItem, max int) string {
	if len(items) == 0 {
		return "无结果"
	}
	if max <= 0 {
		max = 5
	}
	if len(items) > max {
		items = items[:max]
	}

	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"- #%d %s | 象限=%d | 完成=%t | 截止=%s",
			item.ID,
			item.Title,
			item.PriorityGroup,
			item.IsCompleted,
			emptyToDash(item.DeadlineAt),
		))
	}
	return strings.Join(lines, "\n")
}

func buildTaskQueryFallbackReply(items []agentmodel.TaskQueryItem) string {
	if len(items) == 0 {
		return "我这边暂时没找到匹配的任务。你可以再补一句，比如“按截止时间最早的前 3 个”或“只看简单不重要”。"
	}

	preview := items
	if len(preview) > 3 {
		preview = preview[:3]
	}
	lines := make([]string, 0, len(preview))
	for _, item := range preview {
		lines = append(lines, fmt.Sprintf("%s（%s）", item.Title, item.PriorityLabel))
	}
	return fmt.Sprintf("我先给你筛到这些：%s。要不要我再按“更紧急”或“更简单”继续细化？", strings.Join(lines, "、"))
}

func buildTaskQueryFinalReply(items []agentmodel.TaskQueryItem, plan agentmodel.TaskQueryPlan, llmReply string) string {
	if len(items) == 0 {
		base := buildTaskQueryFallbackReply(items)
		if strings.TrimSpace(llmReply) == "" {
			return base
		}
		return strings.TrimSpace(llmReply) + "\n" + base
	}

	desired := plan.Limit
	if desired <= 0 {
		desired = agentmodel.DefaultTaskQueryLimit
	}
	if desired > agentmodel.MaxTaskQueryLimit {
		desired = agentmodel.MaxTaskQueryLimit
	}

	showCount := desired
	if len(items) < showCount {
		showCount = len(items)
	}

	preview := items[:showCount]
	lines := make([]string, 0, len(preview))
	for idx, item := range preview {
		deadline := strings.TrimSpace(item.DeadlineAt)
		if deadline == "" {
			deadline = "无明确截止时间"
		}
		status := "未完成"
		if item.IsCompleted {
			status = "已完成"
		}
		lines = append(lines, fmt.Sprintf(
			"%d. %s（%s，%s，截止：%s）",
			idx+1,
			item.Title,
			item.PriorityLabel,
			status,
			deadline,
		))
	}

	header := fmt.Sprintf("给你整理了 %d 条任务：", showCount)
	if lead := extractSafeReplyLead(llmReply); lead != "" {
		header = lead + "\n" + header
	}

	reply := header + "\n" + strings.Join(lines, "\n")
	if len(items) > showCount {
		reply += fmt.Sprintf("\n另外还有 %d 条匹配任务，要不要我继续往下列？", len(items)-showCount)
	}
	return reply
}

func extractSafeReplyLead(llmReply string) string {
	text := strings.TrimSpace(llmReply)
	if text == "" {
		return ""
	}

	lower := strings.ToLower(text)
	if strings.Contains(text, "\n") ||
		strings.Contains(text, "#") ||
		strings.Contains(lower, "1.") ||
		strings.Contains(text, "1、") ||
		strings.Contains(text, "以下是") {
		return ""
	}
	if len([]rune(text)) > 30 {
		return ""
	}
	return text
}

func sortTaskQueryItems(items []agentmodel.TaskQueryItem, plan agentmodel.TaskQueryPlan) {
	if len(items) <= 1 {
		return
	}

	sortBy := strings.ToLower(strings.TrimSpace(plan.SortBy))
	order := strings.ToLower(strings.TrimSpace(plan.Order))
	if order != "desc" {
		order = "asc"
	}

	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		switch sortBy {
		case "priority":
			if left.PriorityGroup != right.PriorityGroup {
				if order == "desc" {
					return left.PriorityGroup > right.PriorityGroup
				}
				return left.PriorityGroup < right.PriorityGroup
			}
			return left.ID > right.ID
		case "id":
			if order == "desc" {
				return left.ID > right.ID
			}
			return left.ID < right.ID
		default:
			leftTime, leftOK := parseTaskQueryItemDeadline(left.DeadlineAt)
			rightTime, rightOK := parseTaskQueryItemDeadline(right.DeadlineAt)
			if leftOK && rightOK {
				if !leftTime.Equal(rightTime) {
					if order == "desc" {
						return leftTime.After(rightTime)
					}
					return leftTime.Before(rightTime)
				}
				return left.ID > right.ID
			}
			if leftOK && !rightOK {
				return true
			}
			if !leftOK && rightOK {
				return false
			}
			return left.ID > right.ID
		}
	})
}

func parseTaskQueryItemDeadline(raw string) (time.Time, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}, false
	}
	parsed, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func emptyToDash(text string) string {
	if strings.TrimSpace(text) == "" {
		return "-"
	}
	return strings.TrimSpace(text)
}

// extractExplicitLimitFromUser 从用户原话里提取显式条数要求。
//
// 步骤说明：
// 1. 先识别阿拉伯数字表达，例如“前3个”“给我5条”“top 10”。
// 2. 再识别中文数字表达，例如“前五个”“来三个”。
// 3. 最终统一约束到 1~20 范围内。
func extractExplicitLimitFromUser(userMessage string) (int, bool) {
	text := strings.TrimSpace(userMessage)
	if text == "" {
		return 0, false
	}

	for _, pattern := range explicitLimitPatterns {
		matched := pattern.FindStringSubmatch(text)
		if len(matched) < 2 {
			continue
		}
		number, err := strconv.Atoi(strings.TrimSpace(matched[1]))
		if err != nil {
			continue
		}
		return normalizeExplicitLimit(number)
	}

	for _, prefix := range []string{"前", "来", "给我"} {
		for digit, number := range chineseDigitMap {
			token := prefix + string(digit)
			if strings.Contains(text, token) {
				return normalizeExplicitLimit(number)
			}
			for _, suffix := range []string{"个", "条", "项"} {
				if strings.Contains(text, token+suffix) {
					return normalizeExplicitLimit(number)
				}
			}
		}
	}
	return 0, false
}

func normalizeExplicitLimit(number int) (int, bool) {
	if number <= 0 {
		return 0, false
	}
	if number > agentmodel.MaxTaskQueryLimit {
		number = agentmodel.MaxTaskQueryLimit
	}
	return number, true
}
