package schedulerefine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
)

// reactToolCall 表示模型输出的单个工具调用指令。
type reactToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
}

// reactToolResult 表示工具调用的结构化执行结果。
type reactToolResult struct {
	Tool      string `json:"tool"`
	Success   bool   `json:"success"`
	ErrorCode string `json:"error_code,omitempty"`
	Result    string `json:"result"`
}

// reactLLMOutput 表示“强 ReAct”要求的固定 JSON 输出结构。
//
// 字段语义：
// 1. goal_check：本轮要先验证的目标点；
// 2. decision：本轮动作选择依据；
// 3. missing_info：模型明确缺失的信息，前端可直接展示；
// 4. reflect：本轮动作前的预期说明（不是执行后事实）；
// 5. tool_calls：本轮工具动作列表（业务侧只取第一条）。
type reactLLMOutput struct {
	Done        bool            `json:"done"`
	Summary     string          `json:"summary"`
	GoalCheck   string          `json:"goal_check"`
	Decision    string          `json:"decision"`
	MissingInfo []string        `json:"missing_info"`
	Reflect     string          `json:"reflect"`
	ToolCalls   []reactToolCall `json:"tool_calls"`
}

// reviewOutput 表示终审节点要求的固定 JSON 输出结构。
type reviewOutput struct {
	Pass   bool     `json:"pass"`
	Reason string   `json:"reason"`
	Unmet  []string `json:"unmet"`
}

// planningWindow 表示微调工具允许活动的 week/day 边界窗口。
//
// 设计说明：
// 1. 这里用已有 HybridEntries 自动推导窗口，避免把任务移动到完全无关的周；
// 2. 若窗口不可用（没有任何 entry），则降级为“仅做基础合法性校验”。
type planningWindow struct {
	Enabled   bool
	StartWeek int
	StartDay  int
	EndWeek   int
	EndDay    int
}

// refineToolPolicy 是工具层硬约束策略。
//
// 职责边界：
// 1. 负责承载“是否强制保持相对顺序”的策略开关；
// 2. 负责承载顺序校验需要的 origin_order 映射；
// 3. 不负责语义判定（语义仍由 LLM 终审节点负责）。
type refineToolPolicy struct {
	KeepRelativeOrder bool
	OrderScope        string
	OriginOrderMap    map[int]int
}

// dispatchRefineTool 负责把模型输出的 tool_call 分发到具体工具实现。
//
// 步骤化说明：
// 1. 先识别工具名并路由到对应实现；
// 2. 工具实现内部负责参数校验、冲突校验、边界校验、顺序校验；
// 3. 任何失败都返回 Success=false 的结构化结果，而不是直接 panic。
func dispatchRefineTool(entries []model.HybridScheduleEntry, call reactToolCall, window planningWindow, policy refineToolPolicy) ([]model.HybridScheduleEntry, reactToolResult) {
	switch strings.TrimSpace(call.Tool) {
	case "QueryTargetTasks":
		return refineToolQueryTargetTasks(entries, call.Params, policy)
	case "QueryAvailableSlots":
		return refineToolQueryAvailableSlots(entries, call.Params, window)
	case "Move":
		return refineToolMove(entries, call.Params, window, policy)
	case "Swap":
		return refineToolSwap(entries, call.Params, window, policy)
	case "BatchMove":
		return refineToolBatchMove(entries, call.Params, window, policy)
	case "Verify":
		return refineToolVerify(entries, policy)
	default:
		return entries, reactToolResult{
			Tool:    strings.TrimSpace(call.Tool),
			Success: false,
			Result:  fmt.Sprintf("不支持的工具：%s（仅允许 QueryTargetTasks/QueryAvailableSlots/Move/Swap/BatchMove/Verify）", strings.TrimSpace(call.Tool)),
		}
	}
}

// pickSingleToolCall 在“单步动作”策略下选取一个工具调用。
//
// 返回语义：
// 1. call=nil：本轮无可执行动作；
// 2. warn 非空：模型返回了多个调用，本轮只执行第一个并记录告警。
func pickSingleToolCall(calls []reactToolCall) (*reactToolCall, string) {
	if len(calls) == 0 {
		return nil, ""
	}
	call := calls[0]
	if len(calls) == 1 {
		return &call, ""
	}
	return &call, fmt.Sprintf("模型返回了 %d 个工具调用，本轮仅执行第一个：%s", len(calls), call.Tool)
}

// parseReactLLMOutput 解析模型输出的 ReAct JSON。
//
// 容错策略：
// 1. 兼容 ```json 代码块包装；
// 2. 兼容 JSON 前后有解释性文字（提取最外层对象）。
func parseReactLLMOutput(raw string) (*reactLLMOutput, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("ReAct 输出为空")
	}
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	var out reactLLMOutput
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		if out.MissingInfo == nil {
			out.MissingInfo = make([]string, 0)
		}
		return &out, nil
	}
	obj, objErr := extractFirstJSONObject(clean)
	if objErr != nil {
		return nil, fmt.Errorf("无法从输出中提取 JSON：%s", truncate(clean, 220))
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	if out.MissingInfo == nil {
		out.MissingInfo = make([]string, 0)
	}
	return &out, nil
}

// parseReviewOutput 解析终审评估节点输出。
func parseReviewOutput(raw string) (*reviewOutput, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, fmt.Errorf("review 输出为空")
	}
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	var out reviewOutput
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}
	obj, objErr := extractFirstJSONObject(clean)
	if objErr != nil {
		return nil, fmt.Errorf("无法从 review 输出中提取 JSON：%s", truncate(clean, 220))
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// refineToolMove 执行“移动一个 suggested 任务到指定时段”。
//
// 步骤化说明：
// 1. 先校验参数完整性与目标时段合法性，避免写入脏坐标；
// 2. 再校验原任务是否存在、跨度是否一致（防止任务长度被模型改坏）；
// 3. 再校验窗口边界与冲突，确保不会穿透到不可用位置；
// 4. 若启用顺序硬约束，再校验“移动后是否打乱原相对顺序”；
// 5. 全部通过后才真正修改 entries 并返回 Success=true。
func refineToolMove(entries []model.HybridScheduleEntry, params map[string]any, window planningWindow, policy refineToolPolicy) ([]model.HybridScheduleEntry, reactToolResult) {
	// 0. task_id 兼容策略：
	// 0.1 标准键是 task_item_id；
	// 0.2 为了兼容模型偶发输出别名 task_id，这里做兜底兼容，避免“语义正确但参数名不一致”导致整轮白跑；
	// 0.3 两者都不存在时，仍按参数缺失返回失败，由上层 ReAct 继续下一轮决策。
	taskID, ok := paramIntAny(params, "task_item_id", "task_id")
	if !ok {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: "参数缺失：task_item_id"}
	}
	// 1. 参数兼容策略：
	// 1.1 优先读取标准键（to_week/to_day/...）；
	// 1.2 若模型输出了历史别名（target_xxx/day_of_week 等），也兼容解析；
	// 1.3 目标是减少“仅参数名不一致导致的无效失败轮次”。
	toWeek, okWeek := paramIntAny(params, "to_week", "target_week", "week")
	toDay, okDay := paramIntAny(params, "to_day", "target_day", "target_day_of_week", "day_of_week", "day")
	toSF, okSF := paramIntAny(params, "to_section_from", "target_section_from", "section_from")
	toST, okST := paramIntAny(params, "to_section_to", "target_section_to", "section_to")
	if !okWeek || !okDay || !okSF || !okST {
		return entries, reactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  "参数缺失：需要 to_week/to_day/to_section_from/to_section_to",
		}
	}
	if toDay < 1 || toDay > 7 {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("day_of_week=%d 非法，必须在 1~7", toDay)}
	}
	if toSF < 1 || toST > 12 || toSF > toST {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("节次区间 %d-%d 非法", toSF, toST)}
	}

	idx := findSuggestedByID(entries, taskID)
	if idx < 0 {
		return entries, reactToolResult{Tool: "Move", Success: false, Result: fmt.Sprintf("未找到 task_item_id=%d 的 suggested 任务", taskID)}
	}
	origSpan := entries[idx].SectionTo - entries[idx].SectionFrom
	newSpan := toST - toSF
	if origSpan != newSpan {
		return entries, reactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  fmt.Sprintf("任务跨度不一致：原跨度=%d，目标跨度=%d", origSpan+1, newSpan+1),
		}
	}

	if !isWithinWindow(window, toWeek, toDay) {
		return entries, reactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  fmt.Sprintf("目标 W%dD%d 超出允许窗口", toWeek, toDay),
		}
	}

	if conflict, name := hasConflict(entries, toWeek, toDay, toSF, toST, map[int]bool{idx: true}); conflict {
		return entries, reactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  fmt.Sprintf("目标时段已被 %s 占用", name),
		}
	}

	beforeEntries := cloneHybridEntries(entries)
	entry := &entries[idx]
	before := fmt.Sprintf("W%dD%d %d-%d", entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo)
	entry.Week = toWeek
	entry.DayOfWeek = toDay
	entry.SectionFrom = toSF
	entry.SectionTo = toST
	after := fmt.Sprintf("W%dD%d %d-%d", entry.Week, entry.DayOfWeek, entry.SectionFrom, entry.SectionTo)

	sortHybridEntries(entries)
	if issues := validateRelativeOrder(entries, policy); len(issues) > 0 {
		return beforeEntries, reactToolResult{
			Tool:    "Move",
			Success: false,
			Result:  "顺序约束不满足：" + strings.Join(issues, "；"),
		}
	}

	return entries, reactToolResult{
		Tool:    "Move",
		Success: true,
		Result:  fmt.Sprintf("已将任务[%s](id=%d) 从 %s 移动到 %s", entry.Name, taskID, before, after),
	}
}

// refineToolSwap 执行“交换两个 suggested 任务的位置”。
//
// 步骤化说明：
// 1. 先校验两端 task_item_id；
// 2. 再双向验证交换后的落点是否与其他条目冲突；
// 3. 若启用顺序硬约束，再校验“交换后是否打乱相对顺序”；
// 4. 校验通过后提交交换并返回成功。
func refineToolSwap(entries []model.HybridScheduleEntry, params map[string]any, window planningWindow, policy refineToolPolicy) ([]model.HybridScheduleEntry, reactToolResult) {
	// 1. 参数兼容策略同 Move：
	// 1.1 兼容 task_a/task_b 与 task_item_a/task_item_b 等常见别名；
	// 1.2 目标是减少模型输出字段差异导致的无效失败。
	idA, okA := paramIntAny(params, "task_a", "task_item_a", "task_item_id_a")
	idB, okB := paramIntAny(params, "task_b", "task_item_b", "task_item_id_b")
	if !okA || !okB {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: "参数缺失：task_a/task_b"}
	}
	if idA == idB {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: "task_a 与 task_b 不能相同"}
	}

	idxA := findSuggestedByID(entries, idA)
	idxB := findSuggestedByID(entries, idB)
	if idxA < 0 || idxB < 0 {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: "至少有一个任务不是可交换的 suggested 条目"}
	}

	a := entries[idxA]
	b := entries[idxB]
	if !isWithinWindow(window, b.Week, b.DayOfWeek) || !isWithinWindow(window, a.Week, a.DayOfWeek) {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: "交换目标超出允许窗口"}
	}

	excludes := map[int]bool{idxA: true, idxB: true}
	if conflict, name := hasConflict(entries, b.Week, b.DayOfWeek, b.SectionFrom, b.SectionTo, excludes); conflict {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: fmt.Sprintf("任务A交换后将与 %s 冲突", name)}
	}
	if conflict, name := hasConflict(entries, a.Week, a.DayOfWeek, a.SectionFrom, a.SectionTo, excludes); conflict {
		return entries, reactToolResult{Tool: "Swap", Success: false, Result: fmt.Sprintf("任务B交换后将与 %s 冲突", name)}
	}

	beforeEntries := cloneHybridEntries(entries)
	entries[idxA].Week, entries[idxB].Week = entries[idxB].Week, entries[idxA].Week
	entries[idxA].DayOfWeek, entries[idxB].DayOfWeek = entries[idxB].DayOfWeek, entries[idxA].DayOfWeek
	entries[idxA].SectionFrom, entries[idxB].SectionFrom = entries[idxB].SectionFrom, entries[idxA].SectionFrom
	entries[idxA].SectionTo, entries[idxB].SectionTo = entries[idxB].SectionTo, entries[idxA].SectionTo

	sortHybridEntries(entries)
	if issues := validateRelativeOrder(entries, policy); len(issues) > 0 {
		return beforeEntries, reactToolResult{
			Tool:    "Swap",
			Success: false,
			Result:  "顺序约束不满足：" + strings.Join(issues, "；"),
		}
	}

	return entries, reactToolResult{
		Tool:    "Swap",
		Success: true,
		Result:  fmt.Sprintf("已交换任务 id=%d 与 id=%d 的时段", idA, idB),
	}
}

// refineToolBatchMove 执行“原子批量移动 suggested 任务”。
//
// 步骤化说明：
// 1. 参数要求：params.moves 必须是数组，每个元素都满足 Move 的参数格式；
// 2. 执行策略：在 working 副本上按顺序逐条执行 Move；
// 3. 原子语义：任一步失败，整批回滚（返回原 entries）；全部成功才一次性提交；
// 4. 适用场景：用户明确希望“同一轮挪多个任务”，减少 ReAct 往返轮次。
func refineToolBatchMove(entries []model.HybridScheduleEntry, params map[string]any, window planningWindow, policy refineToolPolicy) ([]model.HybridScheduleEntry, reactToolResult) {
	moveParamsList, parseErr := parseBatchMoveParams(params)
	if parseErr != nil {
		return entries, reactToolResult{
			Tool:      "BatchMove",
			Success:   false,
			ErrorCode: "PARAM_MISSING",
			Result:    parseErr.Error(),
		}
	}

	// 1. 在副本上执行，保证原子性：
	// 1.1 每一步都复用 refineToolMove 的全部校验逻辑（冲突、窗口、顺序、跨度）；
	// 1.2 只要任一步失败就中止并回滚到原 entries；
	// 1.3 全部成功后再返回 working，作为整批提交结果。
	working := cloneHybridEntries(entries)
	stepSummary := make([]string, 0, len(moveParamsList))
	currentWindow := buildPlanningWindowFromEntries(working)
	if !currentWindow.Enabled {
		currentWindow = window
	}
	for idx, moveParams := range moveParamsList {
		nextEntries, stepResult := refineToolMove(working, moveParams, currentWindow, policy)
		if !stepResult.Success {
			return entries, reactToolResult{
				Tool:      "BatchMove",
				Success:   false,
				ErrorCode: classifyBatchMoveErrorCode(stepResult.Result),
				Result:    fmt.Sprintf("BatchMove 第%d步失败：%s", idx+1, stepResult.Result),
			}
		}
		working = nextEntries
		currentWindow = buildPlanningWindowFromEntries(working)
		stepSummary = append(stepSummary, fmt.Sprintf("第%d步:%s", idx+1, truncate(stepResult.Result, 120)))
	}

	return working, reactToolResult{
		Tool:    "BatchMove",
		Success: true,
		Result:  fmt.Sprintf("BatchMove 原子提交成功，共执行%d步。%s", len(moveParamsList), strings.Join(stepSummary, " | ")),
	}
}

// refineToolQueryTargetTasks 查询“本轮潜在目标任务集合”。
//
// 步骤化说明：
// 1. 支持按 day_scope(weekend/workday/all)、week 范围、limit 过滤；
// 2. 只读查询，不修改 entries；
// 3. 返回结构化 JSON 字符串，供下一轮模型直接消费。
func refineToolQueryTargetTasks(entries []model.HybridScheduleEntry, params map[string]any, policy refineToolPolicy) ([]model.HybridScheduleEntry, reactToolResult) {
	scope := normalizeDayScope(readString(params, "day_scope", "all"))
	statusFilter := normalizeStatusFilter(readString(params, "status", "suggested"))
	weekFrom, hasWeekFrom := paramIntAny(params, "week_from", "from_week")
	weekTo, hasWeekTo := paramIntAny(params, "week_to", "to_week")
	if week, hasWeek := paramIntAny(params, "week"); hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	if !hasWeekFrom || !hasWeekTo {
		startWeek, endWeek := inferWeekBounds(entries, planningWindow{Enabled: false})
		if !hasWeekFrom {
			weekFrom = startWeek
		}
		if !hasWeekTo {
			weekTo = endWeek
		}
	}
	limit, okLimit := paramIntAny(params, "limit")
	if !okLimit || limit <= 0 {
		limit = 16
	}
	dayFilter := intSliceToSet(readIntSlice(params, "day_of_week", "days"))

	type targetTask struct {
		TaskItemID   int    `json:"task_item_id"`
		Name         string `json:"name"`
		Week         int    `json:"week"`
		DayOfWeek    int    `json:"day_of_week"`
		SectionFrom  int    `json:"section_from"`
		SectionTo    int    `json:"section_to"`
		OriginRank   int    `json:"origin_rank,omitempty"`
		ContextTag   string `json:"context_tag,omitempty"`
		CurrentState string `json:"status"`
	}

	list := make([]targetTask, 0, 32)
	for _, entry := range entries {
		if !matchStatusFilter(entry.Status, statusFilter) {
			continue
		}
		if entry.TaskItemID <= 0 {
			continue
		}
		if len(dayFilter) > 0 {
			if _, ok := dayFilter[entry.DayOfWeek]; !ok {
				continue
			}
		} else if !matchDayScope(entry.DayOfWeek, scope) {
			continue
		}
		if hasWeekFrom && entry.Week < weekFrom {
			continue
		}
		if hasWeekTo && entry.Week > weekTo {
			continue
		}
		list = append(list, targetTask{
			TaskItemID:   entry.TaskItemID,
			Name:         strings.TrimSpace(entry.Name),
			Week:         entry.Week,
			DayOfWeek:    entry.DayOfWeek,
			SectionFrom:  entry.SectionFrom,
			SectionTo:    entry.SectionTo,
			OriginRank:   policy.OriginOrderMap[entry.TaskItemID],
			ContextTag:   strings.TrimSpace(entry.ContextTag),
			CurrentState: entry.Status,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Week != list[j].Week {
			return list[i].Week < list[j].Week
		}
		if list[i].DayOfWeek != list[j].DayOfWeek {
			return list[i].DayOfWeek < list[j].DayOfWeek
		}
		if list[i].SectionFrom != list[j].SectionFrom {
			return list[i].SectionFrom < list[j].SectionFrom
		}
		return list[i].TaskItemID < list[j].TaskItemID
	})
	if len(list) > limit {
		list = list[:limit]
	}

	payload := map[string]any{
		"tool":        "QueryTargetTasks",
		"count":       len(list),
		"status":      statusFilter,
		"day_scope":   scope,
		"week_from":   weekFrom,
		"week_to":     weekTo,
		"day_of_week": keysOfIntSet(dayFilter),
		"items":       list,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entries, reactToolResult{
			Tool:      "QueryTargetTasks",
			Success:   false,
			ErrorCode: "QUERY_ENCODE_FAILED",
			Result:    fmt.Sprintf("序列化查询结果失败：%v", err),
		}
	}
	return entries, reactToolResult{
		Tool:    "QueryTargetTasks",
		Success: true,
		Result:  string(raw),
	}
}

// refineToolQueryAvailableSlots 查询“可放置 suggested 的空位”。
//
// 步骤化说明：
// 1. 根据 day_scope/week 范围/span/exclude_sections 过滤候选时段；
// 2. 使用现有冲突判定（entryBlocksSuggested + sectionsOverlap）确保结果可放置；
// 3. 返回结构化 JSON 字符串，不修改 entries。
func refineToolQueryAvailableSlots(entries []model.HybridScheduleEntry, params map[string]any, window planningWindow) ([]model.HybridScheduleEntry, reactToolResult) {
	scope := normalizeDayScope(readString(params, "day_scope", "all"))
	dayFilter := intSliceToSet(readIntSlice(params, "day_of_week", "days"))
	span, okSpan := paramIntAny(params, "span")
	if !okSpan || span <= 0 {
		span = 2
	}
	if span > 12 {
		return entries, reactToolResult{
			Tool:      "QueryAvailableSlots",
			Success:   false,
			ErrorCode: "SPAN_INVALID",
			Result:    fmt.Sprintf("span=%d 非法，必须在 1~12", span),
		}
	}
	limit, okLimit := paramIntAny(params, "limit")
	if !okLimit || limit <= 0 {
		limit = 12
	}

	weekFrom, hasWeekFrom := paramIntAny(params, "week_from", "from_week")
	weekTo, hasWeekTo := paramIntAny(params, "week_to", "to_week")
	if week, hasWeek := paramIntAny(params, "week"); hasWeek {
		weekFrom, weekTo = week, week
		hasWeekFrom, hasWeekTo = true, true
	}
	if hasWeekFrom && hasWeekTo && weekFrom > weekTo {
		weekFrom, weekTo = weekTo, weekFrom
	}
	if !hasWeekFrom || !hasWeekTo {
		startWeek, endWeek := inferWeekBounds(entries, window)
		if !hasWeekFrom {
			weekFrom = startWeek
		}
		if !hasWeekTo {
			weekTo = endWeek
		}
	}

	excludedSet := make(map[int]struct{})
	for _, sec := range readIntSlice(params, "exclude_sections", "exclude_section") {
		if sec >= 1 && sec <= 12 {
			excludedSet[sec] = struct{}{}
		}
	}
	afterSection, hasAfter := paramIntAny(params, "after_section")
	beforeSection, hasBefore := paramIntAny(params, "before_section")

	type slot struct {
		Week        int `json:"week"`
		DayOfWeek   int `json:"day_of_week"`
		SectionFrom int `json:"section_from"`
		SectionTo   int `json:"section_to"`
	}
	slots := make([]slot, 0, limit)
	for week := weekFrom; week <= weekTo; week++ {
		for day := 1; day <= 7; day++ {
			if len(dayFilter) > 0 {
				if _, ok := dayFilter[day]; !ok {
					continue
				}
			} else if !matchDayScope(day, scope) {
				continue
			}
			if !isWithinWindow(window, week, day) {
				continue
			}
			for sf := 1; sf+span-1 <= 12; sf++ {
				st := sf + span - 1
				if hasAfter && sf <= afterSection {
					continue
				}
				if hasBefore && st >= beforeSection {
					continue
				}
				if intersectsExcludedSections(sf, st, excludedSet) {
					continue
				}
				if conflict, _ := hasConflict(entries, week, day, sf, st, nil); conflict {
					continue
				}
				slots = append(slots, slot{
					Week:        week,
					DayOfWeek:   day,
					SectionFrom: sf,
					SectionTo:   st,
				})
				if len(slots) >= limit {
					break
				}
			}
			if len(slots) >= limit {
				break
			}
		}
		if len(slots) >= limit {
			break
		}
	}

	payload := map[string]any{
		"tool":             "QueryAvailableSlots",
		"count":            len(slots),
		"day_scope":        scope,
		"day_of_week":      keysOfIntSet(dayFilter),
		"week_from":        weekFrom,
		"week_to":          weekTo,
		"span":             span,
		"exclude_sections": keysOfIntSet(excludedSet),
		"slots":            slots,
	}
	if hasAfter {
		payload["after_section"] = afterSection
	}
	if hasBefore {
		payload["before_section"] = beforeSection
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entries, reactToolResult{
			Tool:      "QueryAvailableSlots",
			Success:   false,
			ErrorCode: "QUERY_ENCODE_FAILED",
			Result:    fmt.Sprintf("序列化空位结果失败：%v", err),
		}
	}
	return entries, reactToolResult{
		Tool:    "QueryAvailableSlots",
		Success: true,
		Result:  string(raw),
	}
}

// refineToolVerify 进行“轻量确定性自检”。
//
// 说明：
// 1. 当前只做 deterministic 校验（冲突/顺序），不做语义 LLM 终审；
// 2. 语义层终审仍在 hard_check 节点统一处理；
// 3. 该工具用于给执行阶段一个“可提前自查”的信号。
func refineToolVerify(entries []model.HybridScheduleEntry, policy refineToolPolicy) ([]model.HybridScheduleEntry, reactToolResult) {
	physicsIssues := physicsCheck(entries, 0)
	orderIssues := validateRelativeOrder(entries, policy)
	if len(physicsIssues) == 0 && len(orderIssues) == 0 {
		return entries, reactToolResult{
			Tool:    "Verify",
			Success: true,
			Result:  `{"tool":"Verify","pass":true,"reason":"deterministic checks passed"}`,
		}
	}
	payload := map[string]any{
		"tool":           "Verify",
		"pass":           false,
		"physics_issues": physicsIssues,
		"order_issues":   orderIssues,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return entries, reactToolResult{
			Tool:      "Verify",
			Success:   false,
			ErrorCode: "VERIFY_FAILED",
			Result:    "Verify 校验失败且结果无法序列化",
		}
	}
	return entries, reactToolResult{
		Tool:      "Verify",
		Success:   false,
		ErrorCode: "VERIFY_FAILED",
		Result:    string(raw),
	}
}

// validateRelativeOrder 校验 suggested 任务是否保持“初始相对顺序”。
//
// 步骤化说明：
// 1. 若策略未启用 keep_relative_order，直接通过；
// 2. 否则按时间位置排序 suggested 任务，并映射到 origin_rank；
// 3. 检查 rank 是否单调不降；一旦逆序即判定失败；
// 4. 支持 week 作用域：仅要求每周内保持相对顺序。
func validateRelativeOrder(entries []model.HybridScheduleEntry, policy refineToolPolicy) []string {
	if !policy.KeepRelativeOrder {
		return nil
	}
	if len(policy.OriginOrderMap) == 0 {
		return []string{"未提供顺序基线(origin_order_map)"}
	}

	suggested := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Status == "suggested" && entry.TaskItemID > 0 {
			suggested = append(suggested, entry)
		}
	}
	if len(suggested) <= 1 {
		return nil
	}
	sort.SliceStable(suggested, func(i, j int) bool {
		left := suggested[i]
		right := suggested[j]
		if left.Week != right.Week {
			return left.Week < right.Week
		}
		if left.DayOfWeek != right.DayOfWeek {
			return left.DayOfWeek < right.DayOfWeek
		}
		if left.SectionFrom != right.SectionFrom {
			return left.SectionFrom < right.SectionFrom
		}
		if left.SectionTo != right.SectionTo {
			return left.SectionTo < right.SectionTo
		}
		return left.TaskItemID < right.TaskItemID
	})

	scope := normalizeOrderScope(policy.OrderScope)
	issues := make([]string, 0, 4)
	if scope == "week" {
		lastRankByWeek := make(map[int]int)
		lastNameByWeek := make(map[int]string)
		lastIDByWeek := make(map[int]int)
		for _, entry := range suggested {
			rank, ok := policy.OriginOrderMap[entry.TaskItemID]
			if !ok {
				issues = append(issues, fmt.Sprintf("任务 id=%d 缺少 origin_rank", entry.TaskItemID))
				continue
			}
			last, exists := lastRankByWeek[entry.Week]
			if exists && rank < last {
				issues = append(issues, fmt.Sprintf(
					"W%d 出现逆序：任务[%s](id=%d,rank=%d) 被排在 [%s](id=%d,rank=%d) 之后",
					entry.Week, entry.Name, entry.TaskItemID, rank, lastNameByWeek[entry.Week], lastIDByWeek[entry.Week], last,
				))
			}
			lastRankByWeek[entry.Week] = rank
			lastNameByWeek[entry.Week] = entry.Name
			lastIDByWeek[entry.Week] = entry.TaskItemID
		}
		return issues
	}

	lastRank := -1
	lastName := ""
	lastID := 0
	for _, entry := range suggested {
		rank, ok := policy.OriginOrderMap[entry.TaskItemID]
		if !ok {
			issues = append(issues, fmt.Sprintf("任务 id=%d 缺少 origin_rank", entry.TaskItemID))
			continue
		}
		if lastRank >= 0 && rank < lastRank {
			issues = append(issues, fmt.Sprintf(
				"出现逆序：任务[%s](id=%d,rank=%d) 被排在 [%s](id=%d,rank=%d) 之后",
				entry.Name, entry.TaskItemID, rank, lastName, lastID, lastRank,
			))
		}
		lastRank = rank
		lastName = entry.Name
		lastID = entry.TaskItemID
	}
	return issues
}

// normalizeOrderScope 规范化顺序约束作用域。
func normalizeOrderScope(scope string) string {
	switch strings.TrimSpace(strings.ToLower(scope)) {
	case "week":
		return "week"
	default:
		return "global"
	}
}

// buildPlanningWindowFromEntries 根据现有条目推导允许移动窗口。
func buildPlanningWindowFromEntries(entries []model.HybridScheduleEntry) planningWindow {
	if len(entries) == 0 {
		return planningWindow{Enabled: false}
	}
	startWeek, startDay := entries[0].Week, entries[0].DayOfWeek
	endWeek, endDay := entries[0].Week, entries[0].DayOfWeek
	for _, entry := range entries {
		if compareWeekDay(entry.Week, entry.DayOfWeek, startWeek, startDay) < 0 {
			startWeek, startDay = entry.Week, entry.DayOfWeek
		}
		if compareWeekDay(entry.Week, entry.DayOfWeek, endWeek, endDay) > 0 {
			endWeek, endDay = entry.Week, entry.DayOfWeek
		}
	}
	return planningWindow{
		Enabled:   true,
		StartWeek: startWeek,
		StartDay:  startDay,
		EndWeek:   endWeek,
		EndDay:    endDay,
	}
}

// isWithinWindow 判断目标 week/day 是否落在窗口内。
func isWithinWindow(window planningWindow, week, day int) bool {
	if !window.Enabled {
		return true
	}
	if day < 1 || day > 7 {
		return false
	}
	if compareWeekDay(week, day, window.StartWeek, window.StartDay) < 0 {
		return false
	}
	if compareWeekDay(week, day, window.EndWeek, window.EndDay) > 0 {
		return false
	}
	return true
}

// compareWeekDay 比较两个 week/day 坐标。
// 返回：
// 1) <0：left 更早；
// 2) =0：相同；
// 3) >0：left 更晚。
func compareWeekDay(leftWeek, leftDay, rightWeek, rightDay int) int {
	if leftWeek != rightWeek {
		return leftWeek - rightWeek
	}
	return leftDay - rightDay
}

// findSuggestedByID 在 entries 中查找指定 task_item_id 的 suggested 条目索引。
func findSuggestedByID(entries []model.HybridScheduleEntry, taskItemID int) int {
	for i, entry := range entries {
		if entry.Status == "suggested" && entry.TaskItemID == taskItemID {
			return i
		}
	}
	return -1
}

// hasConflict 检查目标时段是否与其他条目冲突。
//
// 判断规则：
// 1. 仅把“会阻塞 suggested 的条目”纳入冲突判断；
// 2. excludes 中的索引会被跳过（常用于 Move 自身排除或 Swap 双排除）。
func hasConflict(entries []model.HybridScheduleEntry, week, day, sf, st int, excludes map[int]bool) (bool, string) {
	for idx, entry := range entries {
		if excludes != nil && excludes[idx] {
			continue
		}
		if !entryBlocksSuggested(entry) {
			continue
		}
		if entry.Week == week && entry.DayOfWeek == day && sectionsOverlap(entry.SectionFrom, entry.SectionTo, sf, st) {
			return true, fmt.Sprintf("%s(%s)", entry.Name, entry.Type)
		}
	}
	return false, ""
}

// entryBlocksSuggested 判断条目是否会阻塞 suggested 任务落位。
func entryBlocksSuggested(entry model.HybridScheduleEntry) bool {
	if entry.Status == "suggested" {
		return true
	}
	if entry.Status == "existing" {
		return entry.BlockForSuggested
	}
	// 未知状态保守处理为阻塞，避免写入潜在冲突。
	return true
}

// sectionsOverlap 判断两个节次区间是否有交叠。
func sectionsOverlap(aFrom, aTo, bFrom, bTo int) bool {
	return aFrom <= bTo && bFrom <= aTo
}

// paramInt 从 map 中提取 int 参数，兼容 JSON 常见数值类型。
func paramInt(params map[string]any, key string) (int, bool) {
	raw, ok := params[key]
	if !ok {
		return 0, false
	}
	switch v := raw.(type) {
	case int:
		return v, true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// paramIntAny 按“候选键优先级”提取 int 参数。
//
// 步骤化说明：
// 1. 按传入顺序依次尝试每个 key；
// 2. 命中第一个合法值即返回；
// 3. 全部未命中则返回 false，由上层统一抛参数缺失错误。
func paramIntAny(params map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		if v, ok := paramInt(params, key); ok {
			return v, true
		}
	}
	return 0, false
}

// readString 读取字符串参数，缺失时返回默认值。
func readString(params map[string]any, key string, fallback string) string {
	raw, ok := params[key]
	if !ok {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", raw))
	if text == "" {
		return fallback
	}
	return text
}

// normalizeDayScope 规范化 day_scope 取值。
func normalizeDayScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "weekend":
		return "weekend"
	case "workday":
		return "workday"
	default:
		return "all"
	}
}

// normalizeStatusFilter 规范化 status 过滤条件。
func normalizeStatusFilter(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "existing":
		return "existing"
	case "all":
		return "all"
	default:
		return "suggested"
	}
}

// matchStatusFilter 判断条目状态是否命中 status 过滤。
func matchStatusFilter(entryStatus string, statusFilter string) bool {
	switch strings.ToLower(strings.TrimSpace(statusFilter)) {
	case "all":
		return true
	case "existing":
		return strings.TrimSpace(entryStatus) == "existing"
	default:
		return strings.TrimSpace(entryStatus) == "suggested"
	}
}

// matchDayScope 判断 day_of_week 是否满足 scope 过滤条件。
func matchDayScope(day int, scope string) bool {
	switch scope {
	case "weekend":
		return day == 6 || day == 7
	case "workday":
		return day >= 1 && day <= 5
	default:
		return day >= 1 && day <= 7
	}
}

// intSliceToSet 把 int 切片转换为 set，并自动去除非法 day 值。
func intSliceToSet(items []int) map[int]struct{} {
	if len(items) == 0 {
		return nil
	}
	set := make(map[int]struct{}, len(items))
	for _, item := range items {
		if item < 1 || item > 7 {
			continue
		}
		set[item] = struct{}{}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// inferWeekBounds 推断查询周区间。
func inferWeekBounds(entries []model.HybridScheduleEntry, window planningWindow) (int, int) {
	if window.Enabled {
		return window.StartWeek, window.EndWeek
	}
	if len(entries) == 0 {
		return 1, 1
	}
	minWeek, maxWeek := entries[0].Week, entries[0].Week
	for _, entry := range entries {
		if entry.Week < minWeek {
			minWeek = entry.Week
		}
		if entry.Week > maxWeek {
			maxWeek = entry.Week
		}
	}
	return minWeek, maxWeek
}

// readIntSlice 读取 int 切片参数，兼容 []any / []int / 单个数值。
func readIntSlice(params map[string]any, keys ...string) []int {
	for _, key := range keys {
		raw, ok := params[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case []int:
			out := make([]int, len(v))
			copy(out, v)
			return out
		case []any:
			out := make([]int, 0, len(v))
			for _, item := range v {
				switch n := item.(type) {
				case int:
					out = append(out, n)
				case float64:
					out = append(out, int(n))
				case string:
					if parsed, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
						out = append(out, parsed)
					}
				}
			}
			return out
		default:
			if n, okNum := paramInt(params, key); okNum {
				return []int{n}
			}
		}
	}
	return nil
}

// intersectsExcludedSections 判断候选区间是否与排除节次有交集。
func intersectsExcludedSections(from, to int, excluded map[int]struct{}) bool {
	if len(excluded) == 0 {
		return false
	}
	for sec := from; sec <= to; sec++ {
		if _, ok := excluded[sec]; ok {
			return true
		}
	}
	return false
}

// keysOfIntSet 返回 int set 的有序键。
func keysOfIntSet(set map[int]struct{}) []int {
	if len(set) == 0 {
		return nil
	}
	keys := make([]int, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}

// parseBatchMoveParams 解析 BatchMove 的 moves 参数。
//
// 步骤化说明：
// 1. 先读取 params["moves"]，必须存在且为非空数组；
// 2. 再把数组元素逐条转换成 map[string]any，便于复用 refineToolMove；
// 3. 任一元素类型非法即整体失败，避免“部分可执行、部分不可执行”带来的语义歧义。
func parseBatchMoveParams(params map[string]any) ([]map[string]any, error) {
	rawMoves, ok := params["moves"]
	if !ok {
		return nil, fmt.Errorf("参数缺失：BatchMove 需要 moves 数组")
	}

	var items []any
	switch v := rawMoves.(type) {
	case []any:
		items = v
	case []map[string]any:
		items = make([]any, 0, len(v))
		for _, item := range v {
			items = append(items, item)
		}
	default:
		return nil, fmt.Errorf("参数类型错误：BatchMove 的 moves 必须是数组")
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("参数错误：BatchMove 的 moves 不能为空")
	}

	moveParamsList := make([]map[string]any, 0, len(items))
	for idx, item := range items {
		paramMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("参数类型错误：BatchMove 第%d步不是对象", idx+1)
		}
		moveParamsList = append(moveParamsList, paramMap)
	}
	return moveParamsList, nil
}

// classifyBatchMoveErrorCode 把单步 Move 失败原因映射为 BatchMove 层错误码。
//
// 说明：
// 1. 映射保持与普通 Move 的错误语义一致，便于模型统一处理；
// 2. 这里按失败文案做轻量推断，避免引入跨文件循环依赖。
func classifyBatchMoveErrorCode(detail string) string {
	text := strings.TrimSpace(detail)
	switch {
	case strings.Contains(text, "顺序约束不满足"):
		return "ORDER_VIOLATION"
	case strings.Contains(text, "参数缺失"):
		return "PARAM_MISSING"
	case strings.Contains(text, "目标时段已被"):
		return "SLOT_CONFLICT"
	case strings.Contains(text, "任务跨度不一致"):
		return "SPAN_MISMATCH"
	case strings.Contains(text, "超出允许窗口"):
		return "OUT_OF_WINDOW"
	case strings.Contains(text, "day_of_week"):
		return "DAY_INVALID"
	case strings.Contains(text, "节次区间"):
		return "SECTION_INVALID"
	case strings.Contains(text, "未找到 task_item_id"):
		return "TASK_NOT_FOUND"
	default:
		return "BATCH_MOVE_FAILED"
	}
}

// sortHybridEntries 对混合条目做稳定排序，保证日志与预览输出稳定。
func sortHybridEntries(entries []model.HybridScheduleEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.Week != right.Week {
			return left.Week < right.Week
		}
		if left.DayOfWeek != right.DayOfWeek {
			return left.DayOfWeek < right.DayOfWeek
		}
		if left.SectionFrom != right.SectionFrom {
			return left.SectionFrom < right.SectionFrom
		}
		if left.SectionTo != right.SectionTo {
			return left.SectionTo < right.SectionTo
		}
		return left.Name < right.Name
	})
}

// truncate 截断日志内容，避免错误信息无上限增长。
func truncate(text string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxLen {
		return text
	}
	return string(runes[:maxLen]) + "..."
}
