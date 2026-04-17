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
	// executeHistoryKindKey 用于在 history 中打运行态标记，供 prompt 分层识别。
	// 说明：loop_closed / step_advanced 等边界标记仍由节点层写入，但 prompt 层已不再消费它们——
	// 因为 msg1/msg2 已经按"真实对话流 + 当前活跃 ReAct 记录"重构，不再做 msg2→msg1 的归档搬运。
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

// buildExecuteStageMessages 组装 execute 阶段 4 条消息骨架。
//
// 消息结构（固定）：
// 1. message[0] 固定 prompt（规则 + 微调硬引导 + 输出约束 + 工具简表）
// 2. message[1] 历史上下文（真实对话流 + 早期 ReAct 摘要）
// 3. message[2] 当轮 ReAct Loop 窗口（thought/reason + tool_call + observation 绑定展示）
// 4. message[3] 当前执行状态（轮次、模式、plan 步骤、任务类、相关记忆等）
func buildExecuteStageMessages(
	stageSystemPrompt string,
	state *newagentmodel.CommonState,
	ctx *newagentmodel.ConversationContext,
	runtimeUserPrompt string,
) []*schema.Message {
	msg0 := buildExecuteMessage0(stageSystemPrompt, ctx)
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

// buildExecuteMessage0 生成固定规则消息，并附带工具简表。
func buildExecuteMessage0(stageSystemPrompt string, ctx *newagentmodel.ConversationContext) string {
	base := strings.TrimSpace(mergeSystemPrompts(ctx, stageSystemPrompt))
	if base == "" {
		base = "你是 SmartMate 执行器，请继续 execute 阶段。"
	}

	toolCatalog := renderExecuteToolCatalogCompact(ctx)
	if toolCatalog == "" {
		return base
	}
	return base + "\n\n" + toolCatalog
}

// buildExecuteMessage1V3 只渲染"真实对话流 + 阶段锚点"。
//
// 改造说明：
// 1. msg1 只保留 user + assistant speak 组成的真实对话历史，全量注入；
// 2. tool_call / observation 一律由 msg2 承载，这里不再重复；
// 3. 不再从历史中"归档"上一轮 ReAct 结果到 msg1——归档搬运逻辑已随 splitExecuteLoopRecordsByBoundary 一并移除；
// 4. token 预算由统一压缩层兜底，prompt 层不做提前裁剪。
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

// buildExecuteMessage2V3 承载当前会话中全部 ReAct Loop 记录。
//
// 改造说明：
// 1. 不再按 execute_loop_closed / execute_step_advanced 边界切分"归档/活跃"两段；
// 2. 直接从 history 提取全部 assistant tool_call + 对应 observation 作为当前 Loop 视图；
// 3. 新一轮刚开始（尚未产生 tool_call）时返回明确占位，方便模型识别"干净起点"。
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

func buildExecuteMessage3(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext, runtimeUserPrompt string) string {
	lines := []string{"当前执行状态："}

	roundUsed, maxRounds := 0, newagentmodel.DefaultMaxRounds
	modeText := "自由执行（无预定义步骤）"
	if state != nil {
		roundUsed = state.RoundUsed
		if state.MaxRounds > 0 {
			maxRounds = state.MaxRounds
		}
		if state.HasPlan() {
			modeText = "计划执行（有预定义步骤）"
		}
	}
	lines = append(lines,
		fmt.Sprintf("- 当前轮次：%d/%d", roundUsed, maxRounds),
		"- 当前模式："+modeText,
	)

	// 1. 有 plan 时，把当前步骤与完成判定强制写入 msg3。
	// 2. 该锚点用于约束模型只推进当前步骤，避免退化成泛化 ReAct。
	// 3. 当前步骤不可读时给出兜底指引，避免引用旧步骤。
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
			lines = append(lines, fmt.Sprintf("- 当前步骤：第 %d/%d 步", current, total))
			lines = append(lines, "- 当前步骤内容："+stepContent)
			lines = append(lines, "- 当前步骤完成判定(done_when)："+doneWhen)
			lines = append(lines, "- 动作纪律1：未满足 done_when 时，只能 continue / confirm / ask_user，禁止 next_plan")
			lines = append(lines, "- 动作纪律2：满足 done_when 时，优先 next_plan，并在 goal_check 对照 done_when 给证据")
			lines = append(lines, "- 动作纪律3：禁止跳到后续步骤执行")
		} else {
			lines = append(lines, "- 当前计划步骤不可读；请先判断是否已完成全部计划")
			lines = append(lines, "- 若已完成全部计划，输出 done 并给出 goal_check 证据")
		}
	}
	if taskClassText := renderExecuteTaskClassIDs(state); taskClassText != "" {
		lines = append(lines, "- 目标任务类："+taskClassText)
	}
	lines = append(lines, "- 啥时候结束Loop：你可以根据工具调用记录自行判断。")
	lines = append(lines, "- 非目标：不重新粗排、不修改无关任务类。")
	if hasExecuteRoughBuildDone(ctx) {
		lines = append(lines, "- 阶段约束：粗排已完成，本轮只微调 suggested；existing 仅作已安排事实参考，不作为可移动目标。")
	}
	lines = append(lines, "- 参数纪律：工具参数必须严格使用 schema 字段；若返回'参数非法'，需先改参再继续。")
	if state != nil {
		if state.AllowReorder {
			lines = append(lines, "- 顺序策略：用户已明确允许打乱顺序，可在必要时使用 min_context_switch。")
		} else {
			lines = append(lines, "- 顺序策略：默认保持 suggested 相对顺序，禁止调用 min_context_switch。")
		}
	}
	if memoryText := renderExecuteMemoryContext(ctx); memoryText != "" {
		lines = append(lines, "相关记忆（仅在确有帮助时参考，不要机械复述）：")
		lines = append(lines, memoryText)
	}

	// 兼容上层传入的执行指令；若为空则使用固定收口指令。
	instruction := strings.TrimSpace(runtimeUserPrompt)
	if instruction == "" {
		instruction = "请继续当前任务执行阶段，严格输出 JSON。"
	} else {
		instruction = firstExecuteLine(instruction)
	}
	lines = append(lines, "本轮指令："+instruction)

	return strings.Join(lines, "\n")
}

// renderExecuteToolCatalogCompact 将工具 schema 渲染成简表，避免大段 JSON 示例占用上下文。
func renderExecuteToolCatalogCompact(ctx *newagentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}
	schemas := ctx.ToolSchemasSnapshot()
	if len(schemas) == 0 {
		return ""
	}

	lines := []string{"可用工具（简表）："}
	for i, schemaItem := range schemas {
		name := strings.TrimSpace(schemaItem.Name)
		desc := strings.TrimSpace(schemaItem.Desc)
		if name == "" {
			continue
		}
		if desc == "" {
			desc = "无描述"
		}
		lines = append(lines, fmt.Sprintf("%d. %s：%s", i+1, name, desc))

		doc := parseExecuteToolSchema(schemaItem.SchemaText)
		paramSummary := renderExecuteToolParamSummary(doc.Parameters)
		lines = append(lines, "   参数："+paramSummary)
		returnType, returnSample := renderExecuteToolReturnHint(name)
		lines = append(lines, "   返回类型："+returnType)
		lines = append(lines, "   返回示例："+returnSample)
	}

	return strings.Join(lines, "\n")
}

// renderExecuteToolReturnHint 返回工具的返回类型 + 最小示例。
func renderExecuteToolReturnHint(toolName string) (returnType string, sample string) {
	returnType = "string（自然语言文本）"
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "get_overview":
		return returnType, "规划窗口共27天...课程占位条目34个...任务清单（全量，已过滤课程）..."
	case "get_task_info":
		return returnType, "[35]第一章随机事件与概率 | 状态：已预排(suggested) | 占用时段：第3天第5-6节"
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
		return returnType, "批量移动完成，2个任务全部成功。（单次最多2条）"
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

// collectExecuteLoopRecords 从历史中提取 ReAct 记录。
//
// 提取策略：
// 1. 以 assistant tool_call 消息为主键；
// 2. 关联同 ToolCallID 的 tool result 作为 observation；
// 3. 向前回溯最近一条 assistant 文本消息作为 thought/reason。
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
		if content == "" {
			continue
		}
		return content
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

// conversationTurn 表示对话历史中的一轮交互（user 或 assistant speak）。
type conversationTurn struct {
	Role    string
	Content string
}

// collectExecuteConversationTurns 从历史消息中提取 user + assistant speak 对话流。
//
// 提取规则：
// 1. 只保留 user 消息（排除 correction prompt）和 assistant speak 消息（非空 Content 且无 ToolCalls）；
// 2. 全量保留，不再限制轮数和单条长度（token 预算由 execute 层统一管理）；
// 3. 返回的条目按原始时间顺序排列。
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

// renderExecuteMemoryContext 提取 execute 阶段要注入 msg3 的记忆文本。
//
// 1. 只读取统一的 memory_context，避免把其他 pinned block 误塞进 prompt。
// 2. 为空时直接返回空串，保持 msg3 干净。
// 3. 复用统一记忆渲染逻辑，保证各阶段记忆入口一致。
func renderExecuteMemoryContext(ctx *newagentmodel.ConversationContext) string {
	return renderUnifiedMemoryContext(ctx)
}
