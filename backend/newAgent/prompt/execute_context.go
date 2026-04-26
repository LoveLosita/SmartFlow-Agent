package newagentprompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const (
	// executeHistoryKindKey 用于在 history 里区分普通用户消息与后端注入的纠错提示。
	// 这里负责“识别并过滤”，不负责写入该标记。
	executeHistoryKindKey            = "newagent_history_kind"
	executeHistoryKindCorrectionUser = "llm_correction_prompt"
)

type executeToolSchemaDoc struct {
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

type executeLoopRecord struct {
	Thought     string
	ToolName    string
	ToolArgs    string
	Observation string
}

type conversationTurn struct {
	Role    string
	Content string
}

type executeLatestToolRecord struct {
	ToolName    string
	Observation string
}

// buildExecuteStageMessages 组装 execute 阶段的四段式消息。
//
// 1. msg0：系统提示 + 动态规则包 + 工具简表。
// 2. msg1：真实对话流，只保留 user 和 assistant speak。
// 3. msg2：当前 ReAct tool loop 记录。
// 4. msg3：执行状态、阶段约束、记忆和本轮指令。
func buildExecuteStageMessages(
	stageSystemPrompt string,
	state *newagentmodel.CommonState,
	ctx *newagentmodel.ConversationContext,
	runtimeUserPrompt string,
) []*schema.Message {
	msg0 := buildExecuteMessage0(stageSystemPrompt, state, ctx)
	msg1 := buildExecuteMessage1V3(ctx)
	msg2 := buildExecuteMessage2V3(ctx)
	msg3 := buildExecuteMessage3(state, ctx, runtimeUserPrompt)

	return []*schema.Message{
		schema.SystemMessage(msg0),
		{Role: schema.Assistant, Content: msg1},
		{Role: schema.Assistant, Content: msg2},
		schema.SystemMessage(msg3),
	}
}

// buildExecuteMessage0 生成 execute 阶段的固定规则消息。
//
// 1. 先拼基础 system prompt，保证身份和输出协议稳定。
// 2. 再按当前 domain / packs 注入动态规则包，让模型先读到边界。
// 3. 最后再附工具简表，避免模型只看到工具不看到纪律。
func buildExecuteMessage0(stageSystemPrompt string, state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) string {
	base := strings.TrimSpace(mergeSystemPrompts(ctx, stageSystemPrompt))
	if base == "" {
		base = "你是 SmartMate 执行器，请继续当前执行阶段。"
	}

	rulePackSection, _ := renderExecuteRulePackSection(state, ctx)
	if rulePackSection != "" {
		base += "\n\n" + rulePackSection
	}

	toolCatalog := renderExecuteToolCatalogCompact(ctx, state)
	if toolCatalog != "" {
		base += "\n\n" + toolCatalog
	}
	return base
}

// buildExecuteMessage1V3 只渲染真实对话流，不混入 tool observation。
func buildExecuteMessage1V3(ctx *newagentmodel.ConversationContext) string {
	lines := []string{"历史上下文："}
	if ctx == nil {
		lines = append(lines,
			"- 对话历史：暂无。",
			"- 阶段锚点：按当前工具事实推进执行。",
		)
		return strings.Join(lines, "\n")
	}

	turns := collectExecuteConversationTurns(ctx.HistorySnapshot())
	if len(turns) == 0 {
		lines = append(lines, "- 对话历史：暂无。")
	} else {
		turnLines := make([]string, 0, len(turns)+1)
		turnLines = append(turnLines, "对话历史：")
		for _, turn := range turns {
			turnLines = append(turnLines, turn.Role+": \""+turn.Content+"\"")
		}
		lines = append(lines, strings.Join(turnLines, "\n"))
	}

	if hasExecuteRoughBuildDone(ctx) {
		lines = append(lines, "- 阶段锚点：粗排已完成，本轮仅做微调，不重新 place。")
	} else {
		lines = append(lines, "- 阶段锚点：按当前工具事实推进，不做无依据操作。")
	}
	return strings.Join(lines, "\n")
}

// buildExecuteMessage2V3 只承载当轮 ReAct loop。
//
// 1. 每条记录固定展示 thought / tool_call / observation，方便模型做局部闭环。
// 2. 如果当前还没有任何 tool loop，明确给“新一轮”占位，避免模型误判缺上下文。
func buildExecuteMessage2V3(ctx *newagentmodel.ConversationContext) string {
	lines := []string{"当轮 ReAct Loop 记录："}
	if ctx == nil {
		lines = append(lines, "- 暂无可用 ReAct 记录。")
		return strings.Join(lines, "\n")
	}

	loops := collectExecuteLoopRecords(ctx.HistorySnapshot())
	if len(loops) == 0 {
		lines = append(lines, "- 已清空（新一轮 loop 准备中）。")
		return strings.Join(lines, "\n")
	}

	for i, loop := range loops {
		lines = append(lines, fmt.Sprintf("%d) thought/reason：%s", i+1, loop.Thought))
		lines = append(lines, fmt.Sprintf("   tool_call：%s", renderExecuteToolCallText(loop.ToolName, loop.ToolArgs)))
		lines = append(lines, fmt.Sprintf("   observation：%s", loop.Observation))
	}
	return strings.Join(lines, "\n")
}

// buildExecuteMessage3 汇总当前执行状态和本轮指令。
//
// 1. 这里只放“当前轮真正会影响决策”的状态，避免 msg3 继续膨胀。
// 2. 读工具最近结果只给最新一条摘要，避免旧 observation 重复占上下文。
// 3. 最后一行固定落到“本轮指令”，保证模型收尾时注意力还在执行目标上。
func buildExecuteMessage3(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext, runtimeUserPrompt string) string {
	lines := []string{"当前执行状态："}
	roughBuildDone := hasExecuteRoughBuildDone(ctx)

	roundUsed, maxRounds := 0, newagentmodel.DefaultMaxRounds
	modeText := "自由执行（无预定义步骤）"
	activeDomain := ""
	activePacks := []string{}
	if state != nil {
		roundUsed = state.RoundUsed
		if state.MaxRounds > 0 {
			maxRounds = state.MaxRounds
		}
		if state.HasPlan() {
			modeText = "计划执行（有预定义步骤）"
		}
		activeDomain = strings.TrimSpace(state.ActiveToolDomain)
		activePacks = readExecuteActiveToolPacks(state)
	}

	lines = append(lines,
		fmt.Sprintf("- 当前轮次：%d/%d", roundUsed, maxRounds),
		"- 当前模式："+modeText,
	)

	if activeDomain == "" {
		lines = append(lines, "- 动态工具区：当前仅激活 context 管理工具。")
	} else if len(activePacks) == 0 {
		lines = append(lines, fmt.Sprintf("- 动态工具区：domain=%s，未显式激活 packs。", activeDomain))
	} else {
		lines = append(lines, fmt.Sprintf("- 动态工具区：domain=%s，packs=[%s]。", activeDomain, strings.Join(activePacks, ",")))
	}

	if state != nil && state.HasPlan() {
		current, total := state.PlanProgress()
		lines = append(lines, "计划步骤锚点（强约束）：")
		if step, ok := state.CurrentPlanStep(); ok {
			stepContent := strings.TrimSpace(step.Content)
			if stepContent == "" {
				stepContent = "（当前步骤内容为空）"
			}
			doneWhen := strings.TrimSpace(step.DoneWhen)
			if doneWhen == "" {
				doneWhen = "（未提供 done_when，需基于步骤目标给出可验证完成证据）"
			}
			lines = append(lines,
				fmt.Sprintf("- 当前步骤：第 %d/%d 步", current, total),
				"- 当前步骤内容："+stepContent,
				"- 当前步骤完成判定(done_when)："+doneWhen,
				"- 动作纪律1：未满足 done_when 时，只能 continue / confirm / ask_user，禁止 next_plan。",
				"- 动作纪律2：满足 done_when 时，优先 next_plan，并在 goal_check 对照 done_when 给证据。",
				"- 动作纪律3：禁止跳到后续步骤执行。",
			)
		} else {
			lines = append(lines,
				"- 当前计划步骤不可读；请先判断是否已完成全部计划。",
				"- 若已完成全部计划，输出 done 并给出 goal_check 证据。",
			)
		}
	}

	if latestAnalyze := renderExecuteLatestAnalyzeSummary(ctx); latestAnalyze != "" {
		lines = append(lines, "- 最近一次诊断："+latestAnalyze)
	}
	if latestMutation := renderExecuteLatestMutationSummary(ctx); latestMutation != "" {
		lines = append(lines, "- 最近一次写操作："+latestMutation)
	}
	if taskClassText := renderExecuteTaskClassIDs(state); taskClassText != "" {
		lines = append(lines, "- 目标任务类："+taskClassText)
	}

	lines = append(lines,
		"- 啥时候结束Loop：你可以根据工具调用记录自行判断。",
		"- 非目标：不重新粗排、不修改无关任务类。",
	)
	if roughBuildDone {
		lines = append(lines, "- 阶段约束：粗排已完成，本轮只微调 suggested；existing 仅作已安排事实参考，不作为可移动目标。")
	}
	lines = append(lines, "- 参数纪律：工具参数必须严格使用 schema 字段；若返回“参数非法”，需先改参再继续。")

	if state != nil {
		if state.AllowReorder {
			lines = append(lines, "- 顺序策略：用户已明确允许打乱顺序，可在必要时使用 min_context_switch。")
		} else {
			lines = append(lines, "- 顺序策略：默认保持 suggested 相对顺序，禁止调用 min_context_switch。")
		}
	}

	if upsertRuntime := renderTaskClassUpsertRuntime(state); upsertRuntime != "" {
		lines = append(lines, "任务类写入运行态：")
		lines = append(lines, upsertRuntime)
	}

	if memoryText := renderExecuteMemoryContext(ctx); memoryText != "" {
		lines = append(lines, "相关记忆（仅在确有帮助时参考，不要机械复述）：")
		lines = append(lines, memoryText)
	}

	latestAnalyze := renderExecuteLatestAnalyzeSummary(ctx)
	latestMutation := renderExecuteLatestMutationSummary(ctx)
	if nextStep := renderExecuteNextStepHintV2(state, latestAnalyze, latestMutation, roughBuildDone); nextStep != "" {
		lines = append(lines, "下一步提示：")
		lines = append(lines, "- "+nextStep)
	}

	instruction := strings.TrimSpace(runtimeUserPrompt)
	if instruction == "" {
		instruction = "请继续当前任务执行阶段，严格按 SMARTFLOW_DECISION 标签格式输出。"
	} else {
		instruction = firstExecuteLine(instruction)
	}
	lines = append(lines, "本轮指令："+instruction)

	return strings.Join(lines, "\n")
}

// renderExecuteToolCatalogCompact 将当前 tool schemas 渲染为紧凑简表。
//
// 1. 这里只给模型最低必要的参数和返回值感知，不重复塞完整 schema JSON。
// 2. 对复杂工具额外给一条调用示例，降低“参数字段写错”的概率。
// 3. P1 阶段隐藏 min_context_switch，避免模型误用已禁能力。
func renderExecuteToolCatalogCompact(ctx *newagentmodel.ConversationContext, state *newagentmodel.CommonState) string {
	if ctx == nil {
		return ""
	}
	schemas := ctx.ToolSchemasSnapshot()
	if len(schemas) == 0 {
		return ""
	}

	lines := []string{"可用工具（简表）："}
	index := 0
	for _, schemaItem := range schemas {
		name := strings.TrimSpace(schemaItem.Name)
		if name == "" {
			continue
		}
		if shouldHideMinContextSwitchForP1(state, name) {
			continue
		}

		index++
		desc := strings.TrimSpace(schemaItem.Desc)
		if desc == "" {
			desc = "无描述"
		}
		lines = append(lines, fmt.Sprintf("%d. %s：%s", index, name, desc))

		doc := parseExecuteToolSchema(schemaItem.SchemaText)
		paramSummary := renderExecuteToolParamSummary(doc.Parameters)
		lines = append(lines, "   参数："+paramSummary)

		returnType, returnSample := renderExecuteToolReturnHint(name)
		lines = append(lines, "   返回类型："+returnType)
		if shouldRenderExecuteToolReturnSample(name) {
			lines = append(lines, "   返回示例："+returnSample)
		}
		if callSample := renderExecuteToolCallHint(name); strings.TrimSpace(callSample) != "" {
			lines = append(lines, "   调用示例："+callSample)
		}
	}

	if index == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}

func shouldRenderExecuteToolReturnSample(toolName string) bool {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "query_available_slots",
		"query_target_tasks",
		"queue_pop_head",
		"queue_status",
		"queue_apply_head_move",
		"queue_skip_head",
		"web_search",
		"web_fetch",
		"analyze_health",
		"analyze_rhythm",
		"analyze_tolerance",
		"upsert_task_class":
		return true
	default:
		return false
	}
}

func renderExecuteToolCallHint(toolName string) string {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "upsert_task_class":
		return `{"name":"upsert_task_class","arguments":{"task_class":{"name":"线性代数复习","mode":"auto","start_date":"2026-06-01","end_date":"2026-06-20","subject_type":"quantitative","difficulty_level":"high","cognitive_intensity":"high","config":{"total_slots":8,"strategy":"steady","allow_filler_course":false,"excluded_slots":[1,11],"excluded_days_of_week":[6,7]},"items":[{"order":1,"content":"行列式定义与基础计算"},{"order":2,"content":"矩阵及其运算规则"},{"order":3,"content":"逆矩阵与矩阵的秩"}]}}}`
	default:
		return ""
	}
}

func renderExecuteToolReturnHint(toolName string) (returnType string, sample string) {
	returnType = "string（自然语言文本）"
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "get_overview":
		return returnType, "规划窗口共27天...课程占位条目34个...任务清单（已过滤课程）..."
	case "get_task_info":
		return returnType, "[35] 第一章随机事件与概率 | 状态：已预排(suggested) | 占用时段：第3天第5-6节"
	case "query_available_slots":
		return "string（JSON字符串）", `{"tool":"query_available_slots","count":12,"strict_count":8,"embedded_count":4,"slots":[{"day":5,"week":12,"day_of_week":3,"slot_start":1,"slot_end":2,"slot_type":"empty"}]}`
	case "query_target_tasks":
		return "string（JSON字符串）", `{"tool":"query_target_tasks","count":6,"status":"suggested","enqueue":true,"enqueued":6,"queue":{"pending_count":6},"items":[{"task_id":35,"name":"示例任务","status":"suggested","slots":[{"day":3,"week":12,"day_of_week":1,"slot_start":5,"slot_end":6}]}]}`
	case "queue_pop_head":
		return "string（JSON字符串）", `{"tool":"queue_pop_head","has_head":true,"pending_count":5,"current":{"task_id":35,"name":"示例任务","status":"suggested","slots":[{"day":3,"week":12,"day_of_week":1,"slot_start":5,"slot_end":6}]}}`
	case "queue_status":
		return "string（JSON字符串）", `{"tool":"queue_status","pending_count":5,"completed_count":1,"skipped_count":0,"current_task_id":35,"current_attempt":1}`
	case "queue_apply_head_move":
		return "string（JSON字符串）", `{"tool":"queue_apply_head_move","success":true,"task_id":35,"pending_count":4,"completed_count":2,"result":"已将 [35]... 从第3天第5-6节移至第5天第3-4节。"}`
	case "queue_skip_head":
		return "string（JSON字符串）", `{"tool":"queue_skip_head","success":true,"skipped_task_id":35,"pending_count":4,"skipped_count":1}`
	case "query_range":
		return returnType, "第5天第3-6节：第3节空、第4节空..."
	case "place":
		return returnType, "已将 [35]... 预排到第5天第3-4节。"
	case "move":
		return returnType, "已将 [35]... 从第3天第5-6节移至第5天第3-4节。"
	case "swap":
		return returnType, "交换完成：[35]... ↔ [36]..."
	case "batch_move":
		return returnType, "批量移动完成，2 个任务全部成功。"
	case "spread_even":
		return returnType, "均匀化调整完成：共处理 6 个任务，候选坑位 24 个。"
	case "min_context_switch":
		return returnType, "最少上下文切换重排完成：共处理 6 个任务，上下文切换次数 5 -> 2。"
	case "unplace":
		return returnType, "已将 [35]... 移除，恢复为待安排状态。"
	case "web_search":
		return "string（JSON字符串）", `{"tool":"web_search","query":"检索关键词","count":2,"items":[{"title":"搜索结果标题","url":"https://example.com/page","snippet":"摘要片段...","domain":"example.com","published_at":"2025-04-10"}]}`
	case "web_fetch":
		return "string（JSON字符串）", `{"tool":"web_fetch","url":"https://example.com/page","title":"页面标题","content":"正文内容...","truncated":false}`
	case "analyze_health":
		return "string（JSON字符串）", `{"tool":"analyze_health","success":true,"metrics":{"rhythm":{"avg_switches_per_day":1.1,"max_switch_count":4,"heavy_adjacent_days":2,"same_type_transition_ratio":0.58,"block_balance":0,"fragmented_count":0,"compressed_run_count":0},"tightness":{"locally_movable_task_count":3,"avg_local_alternative_slots":1.7,"cross_class_swap_options":1,"forced_heavy_adjacent_days":0,"tightness_level":"tight"},"can_close":false},"decision":{"should_continue_optimize":true,"recommended_operation":"swap","primary_problem":"第4天存在高认知背靠背","candidates":[{"candidate_id":"swap_35_44","tool":"swap","arguments":{"task_a":35,"task_b":44}}]}}`
	case "analyze_rhythm":
		return "string（JSON字符串）", `{"tool":"analyze_rhythm","success":true,"metrics":{"overview":{"avg_switches_per_day":3.4,"max_switch_day":4,"max_switch_count":5,"heavy_adjacent_days":2,"long_high_intensity_days":1,"same_type_transition_ratio":0.42}}}`
	case "analyze_tolerance":
		return "string（JSON字符串）", `{"tool":"analyze_tolerance","success":true,"metrics":{"overall":{"fragmentation_rate":0.52,"days_without_buffer":1}}}`
	case "upsert_task_class":
		return "string（JSON字符串）", `{"tool":"upsert_task_class","success":true,"task_class_id":123,"created":true,"validation":{"ok":true,"issues":[]},"error":"","error_code":""}`
	default:
		return returnType, "自然语言结果（成功/失败原因/关键数据摘要）。"
	}
}

func parseExecuteToolSchema(schemaText string) executeToolSchemaDoc {
	doc := executeToolSchemaDoc{Parameters: map[string]any{}}
	schemaText = strings.TrimSpace(schemaText)
	if schemaText == "" {
		return doc
	}
	if err := json.Unmarshal([]byte(schemaText), &doc); err != nil {
		return doc
	}
	if doc.Parameters == nil {
		doc.Parameters = map[string]any{}
	}
	return doc
}

func renderExecuteToolParamSummary(parameters map[string]any) string {
	if len(parameters) == 0 {
		return "{}"
	}

	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		status := "可选"
		typeText := ""

		switch typed := parameters[key].(type) {
		case string:
			status = "必填"
			typeText = strings.TrimSpace(typed)
		case map[string]any:
			if required, ok := typed["required"].(bool); ok && required {
				status = "必填"
			}
			typeText = strings.TrimSpace(asExecuteString(typed["type"]))
			if enumRaw, ok := typed["enum"].([]any); ok && len(enumRaw) > 0 {
				enumText := make([]string, 0, len(enumRaw))
				for _, item := range enumRaw {
					enumText = append(enumText, fmt.Sprintf("%v", item))
				}
				if typeText == "" {
					typeText = "enum"
				}
				typeText += ":" + strings.Join(enumText, "/")
			}
		}

		if typeText == "" {
			parts = append(parts, fmt.Sprintf("%s(%s)", key, status))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s(%s,%s)", key, status, typeText))
	}
	return strings.Join(parts, "；")
}

// collectExecuteLoopRecords 从 history 里提取 thought + tool_call + observation 三元组。
//
// 1. 以 assistant tool_call 为主记录。
// 2. 用 ToolCallID 去关联 tool observation，保证同轮绑定。
// 3. thought 只向前取最近一条 assistant 纯文本消息，不跨越到更早的工具调用之前做复杂回溯。
func collectExecuteLoopRecords(history []*schema.Message) []executeLoopRecord {
	if len(history) == 0 {
		return nil
	}

	toolResultByCallID := make(map[string]*schema.Message, len(history))
	for _, msg := range history {
		if msg == nil || msg.Role != schema.Tool {
			continue
		}
		callID := strings.TrimSpace(msg.ToolCallID)
		if callID == "" {
			continue
		}
		toolResultByCallID[callID] = msg
	}

	records := make([]executeLoopRecord, 0, len(history))
	for i, msg := range history {
		if msg == nil || msg.Role != schema.Assistant || len(msg.ToolCalls) == 0 {
			continue
		}

		thought := findExecuteThoughtBefore(history, i)
		for _, call := range msg.ToolCalls {
			toolName := strings.TrimSpace(call.Function.Name)
			if toolName == "" {
				toolName = "unknown_tool"
			}

			toolArgs := compactExecuteText(call.Function.Arguments, 160)
			if toolArgs == "" {
				toolArgs = "{}"
			}

			observation := "该工具调用尚未返回结果。"
			callID := strings.TrimSpace(call.ID)
			if callID != "" {
				if resultMsg, ok := toolResultByCallID[callID]; ok && resultMsg != nil {
					text := strings.TrimSpace(resultMsg.Content)
					if text != "" {
						observation = text
					}
				}
			}

			records = append(records, executeLoopRecord{
				Thought:     thought,
				ToolName:    toolName,
				ToolArgs:    toolArgs,
				Observation: observation,
			})
		}
	}
	return records
}

func findExecuteThoughtBefore(history []*schema.Message, index int) string {
	for i := index - 1; i >= 0; i-- {
		msg := history[i]
		if msg == nil || msg.Role != schema.Assistant {
			continue
		}
		if len(msg.ToolCalls) > 0 {
			continue
		}
		content := compactExecuteText(msg.Content, 140)
		if content != "" {
			return content
		}
	}
	return "（未记录）"
}

func renderExecuteToolCallText(toolName, toolArgs string) string {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown_tool"
	}
	toolArgs = strings.TrimSpace(toolArgs)
	if toolArgs == "" {
		toolArgs = "{}"
	}
	return toolName + "(" + toolArgs + ")"
}

func hasExecuteRoughBuildDone(ctx *newagentmodel.ConversationContext) bool {
	if ctx == nil {
		return false
	}
	for _, block := range ctx.PinnedBlocksSnapshot() {
		if strings.TrimSpace(block.Key) == "rough_build_done" {
			return true
		}
	}
	return false
}

func renderExecuteLatestAnalyzeSummary(ctx *newagentmodel.ConversationContext) string {
	record, ok := findExecuteLatestToolRecord(ctx, map[string]struct{}{
		"analyze_health":    {},
		"analyze_rhythm":    {},
		"analyze_tolerance": {},
	})
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s -> %s", record.ToolName, record.Observation)
}

func renderExecuteLatestMutationSummary(ctx *newagentmodel.ConversationContext) string {
	record, ok := findExecuteLatestToolRecord(ctx, map[string]struct{}{
		"place":                 {},
		"move":                  {},
		"swap":                  {},
		"batch_move":            {},
		"unplace":               {},
		"queue_apply_head_move": {},
		"spread_even":           {},
		"min_context_switch":    {},
	})
	if !ok {
		return ""
	}
	return fmt.Sprintf("%s -> %s", record.ToolName, record.Observation)
}

func findExecuteLatestToolRecord(ctx *newagentmodel.ConversationContext, allowSet map[string]struct{}) (executeLatestToolRecord, bool) {
	if ctx == nil || len(allowSet) == 0 {
		return executeLatestToolRecord{}, false
	}
	history := ctx.HistorySnapshot()
	if len(history) == 0 {
		return executeLatestToolRecord{}, false
	}

	toolNameByCallID := make(map[string]string, len(history))
	for _, msg := range history {
		if msg == nil || msg.Role != schema.Assistant || len(msg.ToolCalls) == 0 {
			continue
		}
		for _, call := range msg.ToolCalls {
			callID := strings.TrimSpace(call.ID)
			toolName := strings.TrimSpace(call.Function.Name)
			if callID == "" || toolName == "" {
				continue
			}
			toolNameByCallID[callID] = toolName
		}
	}

	for i := len(history) - 1; i >= 0; i-- {
		msg := history[i]
		if msg == nil || msg.Role != schema.Tool {
			continue
		}
		callID := strings.TrimSpace(msg.ToolCallID)
		if callID == "" {
			continue
		}
		toolName := strings.TrimSpace(toolNameByCallID[callID])
		if toolName == "" {
			continue
		}
		if _, ok := allowSet[toolName]; !ok {
			continue
		}
		return executeLatestToolRecord{
			ToolName:    toolName,
			Observation: summarizeExecuteToolObservation(msg.Content),
		}, true
	}

	return executeLatestToolRecord{}, false
}

func summarizeExecuteToolObservation(raw string) string {
	content := strings.TrimSpace(raw)
	if content == "" {
		return "无返回内容。"
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(content), &payload); err == nil && len(payload) > 0 {
		if toolName := strings.TrimSpace(asExecuteString(payload["tool"])); toolName == "analyze_health" {
			return summarizeExecuteAnalyzeHealthObservationV2(payload)
		}
		for _, key := range []string{"result", "message", "reason", "error"} {
			if text := strings.TrimSpace(asExecuteString(payload[key])); text != "" {
				return compactExecuteText(text, 120)
			}
		}
		if success, ok := payload["success"].(bool); ok {
			if success {
				return "执行成功。"
			}
			return "执行失败。"
		}
	}

	return compactExecuteText(content, 120)
}

// collectExecuteConversationTurns 只提取 user 和 assistant speak。
//
// 1. 过滤 correction prompt，避免把后端纠错提示伪装成用户真实意图。
// 2. 过滤 assistant tool_call 消息，避免 msg1 和 msg2 重复。
// 3. 保持原始顺序，不在这里裁剪长度。
func collectExecuteConversationTurns(history []*schema.Message) []conversationTurn {
	if len(history) == 0 {
		return nil
	}

	turns := make([]conversationTurn, 0, len(history))
	for _, msg := range history {
		if msg == nil {
			continue
		}
		text := strings.TrimSpace(msg.Content)
		if text == "" {
			continue
		}
		switch msg.Role {
		case schema.User:
			if isExecuteCorrectionPrompt(msg) {
				continue
			}
			turns = append(turns, conversationTurn{Role: "user", Content: text})
		case schema.Assistant:
			if len(msg.ToolCalls) > 0 {
				continue
			}
			turns = append(turns, conversationTurn{Role: "assistant", Content: text})
		}
	}

	return turns
}

func isExecuteCorrectionPrompt(msg *schema.Message) bool {
	if msg == nil || msg.Role != schema.User {
		return false
	}
	if msg.Extra != nil {
		if kind, ok := msg.Extra[executeHistoryKindKey].(string); ok && strings.TrimSpace(kind) == executeHistoryKindCorrectionUser {
			return true
		}
	}
	content := strings.TrimSpace(msg.Content)
	return strings.Contains(content, "请重新分析当前状态，输出正确的内容。")
}

func compactExecuteText(content string, maxLen int) string {
	content = firstExecuteLine(content)
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	runes := []rune(content)
	if len(runes) <= maxLen {
		return content
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func firstExecuteLine(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	return strings.TrimSpace(lines[0])
}

func asExecuteString(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func renderExecuteTaskClassIDs(state *newagentmodel.CommonState) string {
	if state == nil || len(state.TaskClassIDs) == 0 {
		return ""
	}

	parts := make([]string, len(state.TaskClassIDs))
	for i, id := range state.TaskClassIDs {
		parts[i] = strconv.Itoa(id)
	}
	return fmt.Sprintf("task_class_ids=[%s]", strings.Join(parts, ","))
}

// renderExecuteMemoryContext 复用统一记忆入口，避免 execute 私自拼接其他 pinned block。
func renderExecuteMemoryContext(ctx *newagentmodel.ConversationContext) string {
	return renderUnifiedMemoryContext(ctx)
}

func renderTaskClassUpsertRuntime(state *newagentmodel.CommonState) string {
	if state == nil || !state.TaskClassUpsertLastTried {
		return ""
	}

	lines := make([]string, 0, 4)
	if state.TaskClassUpsertLastSuccess {
		lines = append(lines, "- 最近一次 upsert_task_class 成功。")
	} else {
		lines = append(lines, "- 最近一次 upsert_task_class 失败。")
	}
	if state.TaskClassUpsertConsecutiveFailures > 0 {
		lines = append(lines, fmt.Sprintf("- 连续失败次数：%d", state.TaskClassUpsertConsecutiveFailures))
	}
	if len(state.TaskClassUpsertLastIssues) > 0 {
		lines = append(lines, "- 需要优先处理 validation.issues：")
		for _, issue := range state.TaskClassUpsertLastIssues {
			trimmed := strings.TrimSpace(issue)
			if trimmed == "" {
				continue
			}
			lines = append(lines, "  - "+trimmed)
		}
	}
	if !state.TaskClassUpsertLastSuccess {
		lines = append(lines, "- 在 issues 处理完之前，不要用 done 收口。")
	}
	return strings.Join(lines, "\n")
}

func shouldHideMinContextSwitchForP1(state *newagentmodel.CommonState, toolName string) bool {
	if strings.TrimSpace(toolName) != "min_context_switch" {
		return false
	}
	return true
}
