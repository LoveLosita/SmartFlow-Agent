package taskquery

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

type taskQueryPlanOutput struct {
	UserGoal         string `json:"user_goal"`
	Quadrants        []int  `json:"quadrants"`
	SortBy           string `json:"sort_by"`
	Order            string `json:"order"`
	Limit            int    `json:"limit"`
	IncludeCompleted *bool  `json:"include_completed"`
	Keyword          string `json:"keyword"`
	DeadlineBefore   string `json:"deadline_before"`
	DeadlineAfter    string `json:"deadline_after"`
}

type taskQueryReflectOutput struct {
	Satisfied  bool                `json:"satisfied"`
	NeedRetry  bool                `json:"need_retry"`
	Reason     string              `json:"reason"`
	Reply      string              `json:"reply"`
	RetryPatch taskQueryRetryPatch `json:"retry_patch"`
}

type taskQueryRetryPatch struct {
	Quadrants        *[]int  `json:"quadrants,omitempty"`
	SortBy           *string `json:"sort_by,omitempty"`
	Order            *string `json:"order,omitempty"`
	Limit            *int    `json:"limit,omitempty"`
	IncludeCompleted *bool   `json:"include_completed,omitempty"`
	Keyword          *string `json:"keyword,omitempty"`
	DeadlineBefore   *string `json:"deadline_before,omitempty"`
	DeadlineAfter    *string `json:"deadline_after,omitempty"`
}

var (
	// explicitLimitPatterns 用于从用户原话提取“显式数量要求”。
	//
	// 例子：
	// 1. 前3个任务
	// 2. 给我5条
	// 3. top 10
	explicitLimitPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\btop\s*(\d{1,2})\b`),
		regexp.MustCompile(`前\s*(\d{1,2})\s*(个|条|项)?`),
		regexp.MustCompile(`(\d{1,2})\s*(个|条|项)\s*任务?`),
		regexp.MustCompile(`给我\s*(\d{1,2})\s*(个|条|项)?`),
	}
	// chineseDigitMap 支持常见中文数字（用于“前五个”“来三个”这类口语）。
	chineseDigitMap = map[rune]int{
		'一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5,
		'六': 6, '七': 7, '八': 8, '九': 9, '十': 10,
	}
)

func (r *taskQueryGraphRunner) planNode(ctx context.Context, st *TaskQueryState) (*TaskQueryState, error) {
	// 1. 防御校验：state 为空时直接返回，避免后续节点空指针。
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in plan node")
	}

	// 2. 规划节点只调用一次模型，把查询意图打包成结构化计划。
	r.emit("task_query.plan.generating", "正在一次性规划查询范围、排序和时间条件。")
	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
用户输入：%s

请输出任务查询计划 JSON。`, st.RequestNowText, st.UserMessage)

	raw, err := callTaskQueryModelForJSON(ctx, r.input.Model, TaskQueryPlanPrompt, prompt, 260)
	if err != nil {
		// 3. 模型失败时不直接终止：回退到默认计划，保证可用性。
		st.UserGoal = "查询任务"
		st.Plan = defaultTaskQueryPlan()
		return st, nil
	}

	planned, parseErr := parseTaskQueryJSON[taskQueryPlanOutput](raw)
	if parseErr != nil {
		// 4. JSON 异常同样回退默认计划，避免用户请求直接失败。
		st.UserGoal = "查询任务"
		st.Plan = defaultTaskQueryPlan()
		return st, nil
	}

	// 5. 规划结果统一规范化，保证后续节点拿到稳定参数。
	st.UserGoal = strings.TrimSpace(planned.UserGoal)
	if st.UserGoal == "" {
		st.UserGoal = "查询任务"
	}
	st.Plan = normalizePlan(taskQueryPlanOutput{
		UserGoal:         planned.UserGoal,
		Quadrants:        planned.Quadrants,
		SortBy:           planned.SortBy,
		Order:            planned.Order,
		Limit:            planned.Limit,
		IncludeCompleted: planned.IncludeCompleted,
		Keyword:          planned.Keyword,
		DeadlineBefore:   planned.DeadlineBefore,
		DeadlineAfter:    planned.DeadlineAfter,
	})

	// 6. 若用户原话里有明确数量要求（例如“给我3个”），强制覆盖 plan.limit。
	//    这样即使规划模型漏掉 limit，也不会影响最终返回条数预期。
	if explicitLimit, found := extractExplicitLimitFromUser(st.UserMessage); found {
		st.ExplicitLimit = explicitLimit
		st.Plan.Limit = explicitLimit
	}
	return st, nil
}

func (r *taskQueryGraphRunner) quadrantNode(ctx context.Context, st *TaskQueryState) (*TaskQueryState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in quadrant node")
	}

	// 1. 象限节点不调用模型，只做“象限参数兜底与去重”。
	// 2. 为空表示全象限，非空表示指定象限。
	r.emit("task_query.quadrant.routing", "正在归一化象限筛选范围。")
	st.Plan.Quadrants = normalizeQuadrants(st.Plan.Quadrants)
	return st, nil
}

func (r *taskQueryGraphRunner) timeAnchorNode(ctx context.Context, st *TaskQueryState) (*TaskQueryState, error) {
	_ = ctx
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in time anchor node")
	}

	// 1. 时间节点不再调用模型，只负责把规划中的时间文本解析为绝对时间对象。
	// 2. 解析失败时清空该边界，避免非法时间导致整条查询失败。
	r.emit("task_query.time.anchoring", "正在锁定时间过滤边界。")
	applyTimeAnchorOnPlan(&st.Plan)
	return st, nil
}

func (r *taskQueryGraphRunner) queryNode(ctx context.Context, st *TaskQueryState) (*TaskQueryState, error) {
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in query node")
	}

	// 1. 按当前计划执行工具查询。
	r.emit("task_query.tool.querying", "正在查询任务数据。")
	items, err := r.executePlanByTool(ctx, st.Plan)
	if err != nil {
		// 查询失败不抛出硬错误，交给反思节点决定如何回复用户。
		st.LastQueryItems = make([]TaskQueryToolRecord, 0)
		st.LastQueryTotal = 0
		st.ReflectReason = "查询工具执行失败"
		return st, nil
	}
	st.LastQueryItems = items
	st.LastQueryTotal = len(items)

	// 2. 额外优化：若结果为空且还没自动放宽过，则先放宽一次再查询（无额外模型调用）。
	if st.LastQueryTotal == 0 && !st.AutoBroadenApplied {
		plan, broadened := autoBroadenPlan(st.Plan)
		if broadened {
			st.AutoBroadenApplied = true
			st.Plan = plan
			r.emit("task_query.tool.broadened", "首次查询为空，已自动放宽条件再试一次。")
			retryItems, retryErr := r.executePlanByTool(ctx, st.Plan)
			if retryErr == nil {
				st.LastQueryItems = retryItems
				st.LastQueryTotal = len(retryItems)
			}
		}
	}
	return st, nil
}

func (r *taskQueryGraphRunner) reflectNode(ctx context.Context, st *TaskQueryState) (*TaskQueryState, error) {
	if st == nil {
		return nil, fmt.Errorf("task query graph: nil state in reflect node")
	}

	// 1. 反思节点负责三件事：
	// 1.1 判断当前结果是否满足用户诉求；
	// 1.2 需要重试时给出最小 patch；
	// 1.3 同时给出可直接返回用户的中文回复。
	r.emit("task_query.reflecting", "正在判断结果是否贴合你的需求。")
	reflectPrompt := buildReflectUserPrompt(st)
	raw, err := callTaskQueryModelForJSON(ctx, r.input.Model, TaskQueryReflectPrompt, reflectPrompt, 380)
	if err != nil {
		// 2. 反思调用失败时直接收束，避免无限等待。
		st.NeedRetry = false
		st.FinalReply = buildTaskQueryFallbackReply(st.LastQueryItems)
		return st, nil
	}

	reflectResult, parseErr := parseTaskQueryJSON[taskQueryReflectOutput](raw)
	if parseErr != nil {
		st.NeedRetry = false
		st.FinalReply = buildTaskQueryFallbackReply(st.LastQueryItems)
		return st, nil
	}

	st.ReflectReason = strings.TrimSpace(reflectResult.Reason)

	// 3. 满足需求时直接结束。
	if reflectResult.Satisfied {
		st.NeedRetry = false
		st.FinalReply = buildTaskQueryFinalReply(st.LastQueryItems, st.Plan, strings.TrimSpace(reflectResult.Reply))
		return st, nil
	}

	// 4. 不满足且允许重试时，应用 patch 并回到查询节点。
	if reflectResult.NeedRetry && st.RetryCount < st.MaxReflectRetry {
		st.Plan = applyRetryPatch(st.Plan, reflectResult.RetryPatch, st.ExplicitLimit)
		st.RetryCount++
		st.NeedRetry = true
		if strings.TrimSpace(reflectResult.Reply) != "" {
			// 4.1 这里先缓存中间回复，最终是否使用取决于后续是否成功命中。
			st.FinalReply = strings.TrimSpace(reflectResult.Reply)
		}
		return st, nil
	}

	// 5. 不再重试：输出最终回复并结束。
	st.NeedRetry = false
	st.FinalReply = buildTaskQueryFinalReply(st.LastQueryItems, st.Plan, strings.TrimSpace(reflectResult.Reply))
	return st, nil
}

func (r *taskQueryGraphRunner) executePlanByTool(ctx context.Context, plan QueryPlan) ([]TaskQueryToolRecord, error) {
	// 1. 这里强制通过工具执行查询，而不是直接读 DAO。
	//    目的：保持“工具边界”一致，后续迁移多工具编排时可复用同一协议。
	if r.queryTool == nil {
		return nil, fmt.Errorf("task query tool is nil")
	}

	merged := make([]TaskQueryToolRecord, 0, plan.Limit)
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

		rawOutput, err := r.queryTool.InvokableRun(ctx, string(rawInput))
		if err != nil {
			return err
		}
		parsed, err := parseTaskQueryJSON[TaskQueryToolOutput](rawOutput)
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

	// 2. Quadrants 为空表示全象限，执行一次无象限过滤查询。
	if len(plan.Quadrants) == 0 {
		if err := runOne(nil); err != nil {
			return nil, err
		}
	} else {
		// 3. 指定象限时逐个调用工具并合并去重。
		for _, quadrant := range plan.Quadrants {
			q := quadrant
			if err := runOne(&q); err != nil {
				return nil, err
			}
		}
	}

	// 4. 合并后再按计划统一排序，保证跨象限结果顺序稳定。
	sortTaskQueryToolRecords(merged, plan)
	if len(merged) > plan.Limit {
		merged = merged[:plan.Limit]
	}
	return merged, nil
}

func normalizePlan(raw taskQueryPlanOutput) QueryPlan {
	plan := defaultTaskQueryPlan()
	plan.Quadrants = normalizeQuadrants(raw.Quadrants)

	sortBy := strings.ToLower(strings.TrimSpace(raw.SortBy))
	switch sortBy {
	case "deadline", "priority", "id":
		plan.SortBy = sortBy
	}

	order := strings.ToLower(strings.TrimSpace(raw.Order))
	switch order {
	case "asc", "desc":
		plan.Order = order
	}

	if raw.Limit > 0 {
		plan.Limit = raw.Limit
	}
	if plan.Limit > MaxTaskQueryLimit {
		plan.Limit = MaxTaskQueryLimit
	}
	if plan.Limit <= 0 {
		plan.Limit = DefaultTaskQueryLimit
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

func defaultTaskQueryPlan() QueryPlan {
	return QueryPlan{
		Quadrants:        nil,
		SortBy:           "deadline",
		Order:            "asc",
		Limit:            DefaultTaskQueryLimit,
		IncludeCompleted: false,
		Keyword:          "",
	}
}

func normalizeQuadrants(quadrants []int) []int {
	if len(quadrants) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(quadrants))
	result := make([]int, 0, len(quadrants))
	for _, q := range quadrants {
		if q < 1 || q > 4 {
			continue
		}
		if _, exists := seen[q]; exists {
			continue
		}
		seen[q] = struct{}{}
		result = append(result, q)
	}
	sort.Ints(result)
	if len(result) == 0 {
		return nil
	}
	if len(result) == 4 {
		// 指定了全部象限时与“空=全象限”等价，统一归一化为 nil。
		return nil
	}
	return result
}

func applyTimeAnchorOnPlan(plan *QueryPlan) {
	if plan == nil {
		return
	}
	before, errBefore := parseOptionalBoundaryTime(plan.DeadlineBeforeText, true)
	after, errAfter := parseOptionalBoundaryTime(plan.DeadlineAfterText, false)

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

	// 边界冲突时清空，防止构造出“必为空结果”的死条件。
	if plan.DeadlineBefore != nil && plan.DeadlineAfter != nil && plan.DeadlineAfter.After(*plan.DeadlineBefore) {
		plan.DeadlineBefore = nil
		plan.DeadlineAfter = nil
		plan.DeadlineBeforeText = ""
		plan.DeadlineAfterText = ""
	}
}

func autoBroadenPlan(plan QueryPlan) (QueryPlan, bool) {
	// 1. 仅允许自动放宽一次，且放宽必须“可解释”：
	// 1.1 清空关键词；
	// 1.2 放开完成状态；
	// 1.3 清空时间边界；
	// 1.4 不主动改象限和 limit，避免语义漂移（例如“简单任务”被放宽成全象限）。
	changed := false
	broadened := plan

	if strings.TrimSpace(broadened.Keyword) != "" {
		broadened.Keyword = ""
		changed = true
	}
	if !broadened.IncludeCompleted {
		broadened.IncludeCompleted = true
		changed = true
	}
	if broadened.DeadlineBefore != nil || broadened.DeadlineAfter != nil ||
		broadened.DeadlineBeforeText != "" || broadened.DeadlineAfterText != "" {
		broadened.DeadlineBefore = nil
		broadened.DeadlineAfter = nil
		broadened.DeadlineBeforeText = ""
		broadened.DeadlineAfterText = ""
		changed = true
	}
	return broadened, changed
}

func applyRetryPatch(plan QueryPlan, patch taskQueryRetryPatch, explicitLimit int) QueryPlan {
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
	if patch.Limit != nil {
		// 用户显式指定数量时，锁定 limit，不允许反思补丁改写。
		if explicitLimit <= 0 {
			limit := *patch.Limit
			if limit <= 0 {
				limit = DefaultTaskQueryLimit
			}
			if limit > MaxTaskQueryLimit {
				limit = MaxTaskQueryLimit
			}
			next.Limit = limit
			changed = true
		}
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
	// 双保险：显式数量存在时再次锁定，避免其他路径误改。
	if explicitLimit > 0 {
		next.Limit = explicitLimit
	}
	return next
}

func buildReflectUserPrompt(st *TaskQueryState) string {
	planSummary := summarizePlan(st.Plan)
	resultSummary := summarizeQueryItems(st.LastQueryItems, 6)
	return fmt.Sprintf(`当前时间：%s
用户原话：%s
用户目标：%s
当前查询计划：%s
当前重试：%d/%d
查询结果摘要：
%s`,
		st.RequestNowText,
		st.UserMessage,
		st.UserGoal,
		planSummary,
		st.RetryCount,
		st.MaxReflectRetry,
		resultSummary,
	)
}

func summarizePlan(plan QueryPlan) string {
	quadrants := "全部象限"
	if len(plan.Quadrants) > 0 {
		parts := make([]string, 0, len(plan.Quadrants))
		for _, q := range plan.Quadrants {
			parts = append(parts, strconv.Itoa(q))
		}
		quadrants = strings.Join(parts, ",")
	}
	return fmt.Sprintf("quadrants=%s sort=%s/%s limit=%d include_completed=%t keyword=%s before=%s after=%s",
		quadrants, plan.SortBy, plan.Order, plan.Limit, plan.IncludeCompleted,
		emptyToDash(plan.Keyword), emptyToDash(plan.DeadlineBeforeText), emptyToDash(plan.DeadlineAfterText))
}

func summarizeQueryItems(items []TaskQueryToolRecord, max int) string {
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
		line := fmt.Sprintf("- #%d %s | 象限=%d | 完成=%t | 截止=%s",
			item.ID, item.Title, item.PriorityGroup, item.IsCompleted, emptyToDash(item.DeadlineAt))
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func buildTaskQueryFallbackReply(items []TaskQueryToolRecord) string {
	if len(items) == 0 {
		return "我这边暂时没找到匹配的任务。你可以再补一句，比如“按截止时间最早的前3个”或“只看简单不重要”。"
	}
	// 1. 用最多 3 条摘要拼一个稳态回复，避免模型异常时空白返回。
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

// buildTaskQueryFinalReply 构建“确定性条数”的最终回复。
//
// 设计目的：
// 1. 让返回条数严格受 plan.limit 约束，避免 LLM 自由发挥导致“只说1条”；
// 2. 仍可保留 LLM 的语气前缀，但清单主体由后端稳定渲染；
// 3. 无结果时统一走兜底文案。
func buildTaskQueryFinalReply(items []TaskQueryToolRecord, plan QueryPlan, llmReply string) string {
	if len(items) == 0 {
		base := buildTaskQueryFallbackReply(items)
		if strings.TrimSpace(llmReply) == "" {
			return base
		}
		return strings.TrimSpace(llmReply) + "\n" + base
	}

	desired := plan.Limit
	if desired <= 0 {
		desired = DefaultTaskQueryLimit
	}
	if desired > MaxTaskQueryLimit {
		desired = MaxTaskQueryLimit
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
		lines = append(lines, fmt.Sprintf("%d. %s（%s，%s，截止：%s）",
			idx+1, item.Title, item.PriorityLabel, status, deadline))
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

// extractSafeReplyLead 从 LLM 回复中提取“安全前缀句”。
//
// 目的：
// 1. 防止 LLM 已经输出一整段列表时再次和后端列表拼接，造成双重输出；
// 2. 仅保留单行短句语气前缀，正文列表始终以后端确定性渲染为准。
func extractSafeReplyLead(llmReply string) string {
	text := strings.TrimSpace(llmReply)
	if text == "" {
		return ""
	}
	// 有明显列表迹象时直接丢弃，避免重复列举。
	lower := strings.ToLower(text)
	if strings.Contains(text, "\n") || strings.Contains(text, "#") ||
		strings.Contains(lower, "1.") || strings.Contains(text, "1、") || strings.Contains(text, "以下是") {
		return ""
	}
	// 太长也不保留，避免把冗长模型输出混进最终回复。
	if len([]rune(text)) > 30 {
		return ""
	}
	return text
}

func sortTaskQueryToolRecords(items []TaskQueryToolRecord, plan QueryPlan) {
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
			lTime, lOK := parseRecordDeadline(left.DeadlineAt)
			rTime, rOK := parseRecordDeadline(right.DeadlineAt)
			if lOK && rOK {
				if !lTime.Equal(rTime) {
					if order == "desc" {
						return lTime.After(rTime)
					}
					return lTime.Before(rTime)
				}
				return left.ID > right.ID
			}
			if lOK && !rOK {
				return true
			}
			if !lOK && rOK {
				return false
			}
			return left.ID > right.ID
		}
	})
}

func parseRecordDeadline(raw string) (time.Time, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02 15:04", text, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

func emptyToDash(text string) string {
	if strings.TrimSpace(text) == "" {
		return "-"
	}
	return strings.TrimSpace(text)
}

// extractExplicitLimitFromUser 从用户原话提取显式数量诉求。
//
// 解析策略：
// 1. 先匹配阿拉伯数字（前3个/top 5/给我2条）；
// 2. 再匹配常见中文数字（前五个/来三个）；
// 3. 统一限制在 1~20 之间。
func extractExplicitLimitFromUser(userMessage string) (int, bool) {
	text := strings.TrimSpace(userMessage)
	if text == "" {
		return 0, false
	}

	for _, pattern := range explicitLimitPatterns {
		matches := pattern.FindStringSubmatch(text)
		if len(matches) < 2 {
			continue
		}
		number, err := strconv.Atoi(strings.TrimSpace(matches[1]))
		if err != nil {
			continue
		}
		return normalizeExplicitLimit(number)
	}

	// 中文数字兜底：覆盖高频口语模式。
	chinesePatterns := []string{"前", "来", "给我"}
	for _, prefix := range chinesePatterns {
		for digitRune, number := range chineseDigitMap {
			token := prefix + string(digitRune)
			if strings.Contains(text, token) {
				return normalizeExplicitLimit(number)
			}
			// “前五个”“来三个”这类再补一个“个/条/项”尾缀判断。
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
	if number > MaxTaskQueryLimit {
		number = MaxTaskQueryLimit
	}
	return number, true
}

func callTaskQueryModelForJSON(
	ctx context.Context,
	model *ark.ChatModel,
	systemPrompt string,
	userPrompt string,
	maxTokens int,
) (string, error) {
	if model == nil {
		return "", fmt.Errorf("task query model is nil")
	}
	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userPrompt),
	}

	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
	}
	if maxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(maxTokens))
	}

	resp, err := model.Generate(ctx, messages, opts...)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("task query model returned nil")
	}
	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return "", fmt.Errorf("task query model returned empty content")
	}
	return text, nil
}

func parseTaskQueryJSON[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("empty response")
	}

	// 1. 兼容 ```json 包裹格式。
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	// 2. 先尝试整体解析。
	var out T
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}

	// 3. 若模型前后带了额外文本，则提取最外层对象再解析。
	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start == -1 || end == -1 || end <= start {
		return nil, fmt.Errorf("no json object found")
	}
	obj := clean[start : end+1]
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}
