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
	executeHistoryKindKey            = "newagent_history_kind"
	executeHistoryKindCorrectionUser = "llm_correction_prompt"

	// executeLoopWindowLimit 控制“当轮 ReAct Loop 窗口”最多保留多少条记录。
	// 采用固定窗口能避免上下文无上限增长，且可保持“最近行为”可追踪。
	executeLoopWindowLimit = 8

	// executeTrimmedObservationText 是重复工具压缩后的 observation 占位文案。
	// 当同工具在窗口内出现多次时，只保留最新一条真实结果，其余旧结果统一替换为该文案。
	executeTrimmedObservationText = "当前工具调用结果过于久远，已经被删除。"
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
// 2. message[1] 历史上下文（聊天摘要 + 早期 ReAct 摘要）
// 3. message[2] 当轮 ReAct Loop 窗口（thought/reason + tool_call + observation 绑定展示）
// 4. message[3] 当前执行状态（含初始目标、结束判断原则、非目标）
func buildExecuteStageMessages(
	stageSystemPrompt string,
	state *newagentmodel.CommonState,
	ctx *newagentmodel.ConversationContext,
	runtimeUserPrompt string,
) []*schema.Message {
	msg0 := buildExecuteMessage0(stageSystemPrompt, ctx)
	msg1 := buildExecuteMessage1(ctx)
	msg2 := buildExecuteMessage2(ctx)
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
		base = "你是 SmartFlow NewAgent 执行器，请继续 execute 阶段。"
	}

	toolCatalog := renderExecuteToolCatalogCompact(ctx)
	if toolCatalog == "" {
		return base
	}
	return base + "\n\n" + toolCatalog
}

// buildExecuteMessage1 生成历史上下文短摘要。
func buildExecuteMessage1(ctx *newagentmodel.ConversationContext) string {
	lines := []string{"历史上下文（仅供参考）："}
	if ctx == nil {
		lines = append(lines,
			"- 用户目标：暂无可用历史输入。",
			"- 阶段锚点：按当前工具事实推进执行。",
			"- 早期 ReAct 摘要：暂无。",
		)
		return strings.Join(lines, "\n")
	}

	history := ctx.HistorySnapshot()
	firstUser, lastUser := pickExecuteUserInputs(history)
	switch {
	case firstUser == "":
		lines = append(lines, "- 用户目标：暂无可用历史输入。")
	case lastUser != "" && lastUser != firstUser:
		lines = append(lines, "- 用户目标："+firstUser+"；最近补充："+lastUser)
	default:
		lines = append(lines, "- 用户目标："+firstUser)
	}

	if hasExecuteRoughBuildDone(ctx) {
		lines = append(lines, "- 阶段锚点：粗排已完成，本轮仅做微调，不重新 place。")
	} else {
		lines = append(lines, "- 阶段锚点：按当前工具事实推进，不做无依据操作。")
	}

	allLoops := collectExecuteLoopRecords(history)
	lines = append(lines, "- 早期 ReAct 摘要："+buildEarlyExecuteReactSummary(allLoops, executeLoopWindowLimit))
	return strings.Join(lines, "\n")
}

// buildExecuteMessage2 生成当轮 ReAct Loop 窗口。
//
// 规则：
// 1. 每条记录都展示 thought/reason + tool_call + observation；
// 2. 对窗口内重复工具应用压缩：同工具只保留最新一条真实 observation；
// 3. 被压缩的旧 observation 统一替换为占位文案，避免语义断裂。
func buildExecuteMessage2(ctx *newagentmodel.ConversationContext) string {
	lines := []string{"当轮 ReAct Loop 记录（窗口）："}
	if ctx == nil {
		lines = append(lines, "- 暂无可用 ReAct 记录。")
		return strings.Join(lines, "\n")
	}

	allLoops := collectExecuteLoopRecords(ctx.HistorySnapshot())
	if len(allLoops) == 0 {
		lines = append(lines, "- 暂无可用 ReAct 记录。")
		return strings.Join(lines, "\n")
	}

	windowLoops := tailExecuteLoops(allLoops, executeLoopWindowLimit)
	windowLoops = compressExecuteLoopObservationsByTool(windowLoops)
	for i, loop := range windowLoops {
		lines = append(lines, fmt.Sprintf("%d) thought/reason：%s", i+1, loop.Thought))
		lines = append(lines, fmt.Sprintf("   tool_call：%s", renderExecuteToolCallText(loop.ToolName, loop.ToolArgs)))
		lines = append(lines, fmt.Sprintf("   observation：%s", loop.Observation))
	}
	return strings.Join(lines, "\n")
}

// buildExecuteMessage3 生成当前执行状态与执行锚点。
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

	goal := extractExecuteInitialGoal(ctx)
	if goal == "" {
		goal = "暂无可用目标描述，请按当前上下文稳步推进。"
	}

	lines = append(lines, "执行锚点：")
	lines = append(lines, "- 初始用户目标："+goal)
	if taskClassText := renderExecuteTaskClassIDs(state); taskClassText != "" {
		lines = append(lines, "- 目标任务类："+taskClassText)
	}
	lines = append(lines, "- 啥时候结束Loop：你可以根据工具调用记录自行判断。")
	lines = append(lines, "- 非目标：不重新粗排、不修改无关任务类。")
	if hasExecuteRoughBuildDone(ctx) {
		lines = append(lines, "- 阶段约束：粗排已完成，本轮只微调 suggested；existing 仅作已安排事实参考，不做 move/batch_move。")
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

// renderExecuteToolReturnHint 返回工具的“返回类型 + 最小示例”。
//
// 说明：
// 1. 所有工具当前都返回 string（自然语言），这里主要补“内容形态示例”，减少模型盲猜；
// 2. 示例只保留最小片段，避免工具说明过长挤占上下文窗口。
func renderExecuteToolReturnHint(toolName string) (returnType string, sample string) {
	returnType = "string（自然语言文本）"
	switch strings.ToLower(strings.TrimSpace(toolName)) {
	case "get_overview":
		return returnType, "规划窗口共27天...课程占位条目34个...任务清单（全量，已过滤课程）..."
	case "list_tasks":
		return returnType, "已预排任务共24个： [35]第一章随机事件与概率 — 已预排至 第3天第5-6节..."
	case "get_task_info":
		return returnType, "[35]第一章随机事件与概率 | 状态：已预排(suggested) | 占用时段：第3天第5-6节"
	case "find_first_free":
		return returnType, "首个可用位置：第5天第1-2节（可直接放置）| 当日负载：总占6/12..."
	case "find_free":
		return returnType, "兼容别名，返回同 find_first_free。"
	case "query_range":
		return returnType, "第5天第3-6节：第3节空、第4节空..."
	case "place":
		return returnType, "已将 [35]... 预排到第5天第3-4节。"
	case "move":
		return returnType, "已将 [35]... 从第3天第5-6节移至第5天第3-4节。"
	case "swap":
		return returnType, "交换完成：[35]... ↔ [36]..."
	case "batch_move":
		return returnType, "批量移动完成，2个任务全部成功。"
	case "unplace":
		return returnType, "已将 [35]... 移除，恢复为待安排状态。"
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

func tailExecuteLoops(records []executeLoopRecord, limit int) []executeLoopRecord {
	if len(records) == 0 {
		return nil
	}
	if limit <= 0 || len(records) <= limit {
		result := make([]executeLoopRecord, len(records))
		copy(result, records)
		return result
	}
	result := make([]executeLoopRecord, limit)
	copy(result, records[len(records)-limit:])
	return result
}

// compressExecuteLoopObservationsByTool 对窗口内重复工具做 observation 压缩。
//
// 规则：
// 1. 以“工具名”作为压缩键；
// 2. 同工具仅保留最新一条 observation 原文；
// 3. 旧记录保持 thought/tool_call，不丢记录，仅替换 observation。
func compressExecuteLoopObservationsByTool(records []executeLoopRecord) []executeLoopRecord {
	if len(records) == 0 {
		return records
	}

	latestIndexByTool := make(map[string]int, len(records))
	for i := len(records) - 1; i >= 0; i-- {
		key := strings.ToLower(strings.TrimSpace(records[i].ToolName))
		if key == "" {
			key = "unknown_tool"
		}
		if _, exists := latestIndexByTool[key]; !exists {
			latestIndexByTool[key] = i
		}
	}

	result := make([]executeLoopRecord, len(records))
	copy(result, records)
	for i := range result {
		key := strings.ToLower(strings.TrimSpace(result[i].ToolName))
		if key == "" {
			key = "unknown_tool"
		}
		if latestIndexByTool[key] != i {
			result[i].Observation = executeTrimmedObservationText
		}
	}
	return result
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

func buildEarlyExecuteReactSummary(records []executeLoopRecord, windowLimit int) string {
	if len(records) == 0 {
		return "暂无。"
	}
	if len(records) <= windowLimit {
		return "无（当前窗口已覆盖全部 ReAct 记录）。"
	}

	early := records[:len(records)-windowLimit]
	toolCounts := make(map[string]int, len(early))
	for _, record := range early {
		key := strings.TrimSpace(record.ToolName)
		if key == "" {
			key = "unknown_tool"
		}
		toolCounts[key]++
	}

	names := make([]string, 0, len(toolCounts))
	for name := range toolCounts {
		names = append(names, name)
	}
	sort.Strings(names)

	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s×%d", name, toolCounts[name]))
	}

	return fmt.Sprintf("已折叠 %d 条旧记录，涉及：%s。", len(early), strings.Join(parts, "、"))
}

func extractExecuteInitialGoal(ctx *newagentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}
	history := ctx.HistorySnapshot()
	firstUser, _ := pickExecuteUserInputs(history)
	return firstUser
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

func pickExecuteUserInputs(history []*schema.Message) (first string, last string) {
	realUsers := make([]string, 0, 2)
	for _, msg := range history {
		if msg == nil || msg.Role != schema.User {
			continue
		}
		if isExecuteCorrectionPrompt(msg) {
			continue
		}
		text := compactExecuteText(msg.Content, 120)
		if text == "" {
			continue
		}
		realUsers = append(realUsers, text)
	}
	if len(realUsers) == 0 {
		return "", ""
	}
	return realUsers[0], realUsers[len(realUsers)-1]
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
