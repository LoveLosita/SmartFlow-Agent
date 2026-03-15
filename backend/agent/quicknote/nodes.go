package quicknote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

type quickNoteIntentModelOutput struct {
	IsQuickNote bool   `json:"is_quick_note"`
	Title       string `json:"title"`
	DeadlineAt  string `json:"deadline_at"`
	Reason      string `json:"reason"`
}

type quickNotePriorityModelOutput struct {
	PriorityGroup int    `json:"priority_group"`
	Reason        string `json:"reason"`
}

// quickNotePlanModelOutput 是“单请求聚合规划”节点的模型输出。
type quickNotePlanModelOutput struct {
	Title          string `json:"title"`
	DeadlineAt     string `json:"deadline_at"`
	PriorityGroup  int    `json:"priority_group"`
	PriorityReason string `json:"priority_reason"`
	Banter         string `json:"banter"`
}

// runQuickNoteIntentNode 负责“意图识别 + 聚合规划 + 时间校验”。
// 说明：
// 1) trustRoute 命中时，直接走单请求聚合规划，跳过二次意图识别；
// 2) 无论是否走快路径，最终都要走本地时间硬校验，防止脏时间落库。
func runQuickNoteIntentNode(ctx context.Context, st *QuickNoteState, input QuickNoteGraphRunInput, emitStage func(stage, detail string)) (*QuickNoteState, error) {
	// 0. 基础防御：state 为空直接返回错误，避免后续节点空指针。
	if st == nil {
		return nil, errors.New("quick note graph: nil state in intent node")
	}

	// 1. 如果上游路由已高置信命中 quick_note，则走“单请求聚合快路径”。
	if input.SkipIntentVerification {
		emitStage("quick_note.intent.analyzing", "已由上游路由判定为任务请求，跳过二次意图判断。")
		st.IsQuickNoteIntent = true
		st.IntentJudgeReason = "上游路由已命中 quick_note，跳过二次意图判定"
		st.PlannedBySingleCall = true

		// 1.1 一次调用里尽量拿齐 title/deadline/priority/banter，减少串行模型开销。
		emitStage("quick_note.plan.generating", "正在一次性生成时间归一化、优先级与回复润色。")
		plan, planErr := planQuickNoteInSingleCall(ctx, input.Model, st.RequestNowText, st.RequestNow, st.UserInput)
		if planErr != nil {
			// 1.2 聚合规划失败不终止链路，改为后续本地兜底。
			st.IntentJudgeReason += "；聚合规划失败，回退本地兜底"
		} else {
			// 1.3 仅在字段有效时回填，避免无效值污染状态。
			if strings.TrimSpace(plan.Title) != "" {
				st.ExtractedTitle = strings.TrimSpace(plan.Title)
			}
			if plan.Deadline != nil {
				st.ExtractedDeadline = plan.Deadline
			}
			st.ExtractedDeadlineText = strings.TrimSpace(plan.DeadlineText)
			if IsValidTaskPriority(plan.PriorityGroup) {
				st.ExtractedPriority = plan.PriorityGroup
				st.ExtractedPriorityReason = strings.TrimSpace(plan.PriorityReason)
			}
			st.ExtractedBanter = strings.TrimSpace(plan.Banter)
		}

		// 1.4 如果模型没给标题，基于原句做本地标题提取兜底。
		if strings.TrimSpace(st.ExtractedTitle) == "" {
			st.ExtractedTitle = deriveQuickNoteTitleFromInput(st.UserInput)
		}

		// 1.5 无论是否聚合成功，都要进行本地时间硬校验，防止脏时间写库。
		emitStage("quick_note.deadline.validating", "正在校验并归一化任务时间。")
		userDeadline, userHasTimeHint, userDeadlineErr := parseOptionalDeadlineFromUserInput(st.UserInput, st.RequestNow)
		if userHasTimeHint && userDeadlineErr != nil {
			st.DeadlineValidationError = userDeadlineErr.Error()
			st.AssistantReply = "我识别到你给了时间信息，但这个时间格式我没法准确解析，请改成例如：2026-03-20 18:30、明天下午3点、下周一上午9点。"
			emitStage("quick_note.failed", "时间校验失败，未执行写入。")
			return st, nil
		}
		if userDeadline != nil {
			// 用户原句能解析出时间时，以原句解析结果为准（更贴近真实输入）。
			st.ExtractedDeadline = userDeadline
			st.ExtractedDeadlineText = strings.TrimSpace(st.UserInput)
		}
		return st, nil
	}

	// 2. 常规路径：先让模型做意图识别 + 初步抽取。
	emitStage("quick_note.intent.analyzing", "正在分析用户输入是否属于任务安排请求。")
	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
用户输入：%s
请仅输出 JSON（不要 markdown，不要解释），字段如下：
{
  "is_quick_note": boolean,
  "title": string,
  "deadline_at": string,
  "reason": string
}
字段约束：
1) deadline_at 只允许输出绝对时间，格式必须为 "yyyy-MM-dd HH:mm"。
2) 如果用户说了“明天/后天/下周一/今晚”等相对时间，必须基于上面的当前时间换算成绝对时间。
3) 如果用户没有提及时间，deadline_at 输出空字符串。`,
		st.RequestNowText,
		st.UserInput,
	)

	// 2.1 模型调用失败时，保守回退普通聊天，避免误写任务。
	raw, callErr := callModelForJSON(ctx, input.Model, QuickNoteIntentPrompt, prompt)
	if callErr != nil {
		st.IsQuickNoteIntent = false
		st.IntentJudgeReason = "意图识别失败，回退普通聊天"
		return st, nil
	}

	// 2.2 解析失败同样回退普通聊天，保证稳定性优先。
	parsed, parseErr := parseJSONPayload[quickNoteIntentModelOutput](raw)
	if parseErr != nil {
		st.IsQuickNoteIntent = false
		st.IntentJudgeReason = "意图识别结果不可解析，回退普通聊天"
		return st, nil
	}

	st.IsQuickNoteIntent = parsed.IsQuickNote
	st.IntentJudgeReason = strings.TrimSpace(parsed.Reason)
	if !st.IsQuickNoteIntent {
		// 非随口记：后续通过分支直接退出 graph。
		return st, nil
	}

	// 2.3 处理标题字段：为空时回退到用户原句。
	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = strings.TrimSpace(st.UserInput)
	}
	st.ExtractedTitle = title

	emitStage("quick_note.deadline.validating", "正在校验并归一化任务时间。")

	// Step A：优先尝试解析模型抽取出来的 deadline。
	// 这样可利用模型“结构化理解”能力先拿一次候选时间。
	st.ExtractedDeadlineText = strings.TrimSpace(parsed.DeadlineAt)
	if st.ExtractedDeadlineText != "" {
		if deadline, deadlineErr := parseOptionalDeadlineWithNow(st.ExtractedDeadlineText, st.RequestNow); deadlineErr == nil {
			st.ExtractedDeadline = deadline
		}
	}

	// Step B：基于用户原句执行“本地时间解析 + 合法性校验”。
	// 本地校验是最终硬门槛，确保“用户给错时间不会被静默写成 NULL”。
	userDeadline, userHasTimeHint, userDeadlineErr := parseOptionalDeadlineFromUserInput(st.UserInput, st.RequestNow)
	if userHasTimeHint && userDeadlineErr != nil {
		st.DeadlineValidationError = userDeadlineErr.Error()
		st.AssistantReply = "我识别到你给了时间信息，但这个时间格式我没法准确解析，请改成例如：2026-03-20 18:30、明天下午3点、下周一上午9点。"
		emitStage("quick_note.failed", "时间校验失败，未执行写入。")
		return st, nil
	}

	if st.ExtractedDeadline == nil && userDeadline != nil {
		// 当模型未提取出时间，但原句能解析时，补写时间结果。
		st.ExtractedDeadline = userDeadline
		if st.ExtractedDeadlineText == "" {
			st.ExtractedDeadlineText = strings.TrimSpace(st.UserInput)
		}
	}
	return st, nil
}

// runQuickNotePriorityNode 负责“优先级评估”。
// 说明：
// 1) 聚合规划已给出合法优先级时直接复用；
// 2) 快路径下缺失优先级时直接本地兜底，避免额外模型调用；
// 3) 其余场景走独立评估模型，失败再兜底。
func runQuickNotePriorityNode(ctx context.Context, st *QuickNoteState, input QuickNoteGraphRunInput, emitStage func(stage, detail string)) (*QuickNoteState, error) {
	if st == nil {
		return nil, errors.New("quick note graph: nil state in priority node")
	}
	// 1. 非随口记或时间校验失败时，不做优先级评估。
	if !st.IsQuickNoteIntent || strings.TrimSpace(st.DeadlineValidationError) != "" {
		return st, nil
	}

	// 2. 已有合法优先级则直接复用，避免重复调用模型。
	if IsValidTaskPriority(st.ExtractedPriority) {
		if strings.TrimSpace(st.ExtractedPriorityReason) == "" {
			st.ExtractedPriorityReason = "复用聚合规划优先级"
		}
		emitStage("quick_note.priority.evaluating", "已复用聚合规划结果中的优先级。")
		return st, nil
	}
	// 3. 快路径下若缺失优先级，直接本地兜底，追求低延迟。
	if input.SkipIntentVerification || st.PlannedBySingleCall {
		st.ExtractedPriority = fallbackPriority(st)
		st.ExtractedPriorityReason = "聚合规划未给出合法优先级，使用本地兜底"
		emitStage("quick_note.priority.evaluating", "聚合优先级缺失，已使用本地兜底。")
		return st, nil
	}

	// 4. 常规路径才调用独立优先级模型。
	emitStage("quick_note.priority.evaluating", "正在评估任务优先级。")
	deadlineText := "无"
	if st.ExtractedDeadline != nil {
		deadlineText = formatQuickNoteTimeToMinute(*st.ExtractedDeadline)
	}
	deadlineClue := strings.TrimSpace(st.ExtractedDeadlineText)
	if deadlineClue == "" {
		deadlineClue = "无"
	}

	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
请对以下任务评估优先级：
- 任务标题：%s
- 用户原始输入：%s
- 时间线索原文：%s
- 归一化截止时间：%s

请仅输出 JSON（不要 markdown，不要解释）：
{
  "priority_group": 1|2|3|4,
  "reason": "简短理由"
}`,
		st.RequestNowText,
		st.ExtractedTitle,
		st.UserInput,
		deadlineClue,
		deadlineText,
	)

	// 4.1 调用失败：使用本地兜底，不中断主链路。
	raw, callErr := callModelForJSON(ctx, input.Model, QuickNotePriorityPrompt, prompt)
	if callErr != nil {
		st.ExtractedPriority = fallbackPriority(st)
		st.ExtractedPriorityReason = "优先级评估失败，使用兜底策略"
		return st, nil
	}

	// 4.2 解析失败或非法值：同样兜底。
	parsed, parseErr := parseJSONPayload[quickNotePriorityModelOutput](raw)
	if parseErr != nil || !IsValidTaskPriority(parsed.PriorityGroup) {
		st.ExtractedPriority = fallbackPriority(st)
		st.ExtractedPriorityReason = "优先级结果异常，使用兜底策略"
		return st, nil
	}

	st.ExtractedPriority = parsed.PriorityGroup
	st.ExtractedPriorityReason = strings.TrimSpace(parsed.Reason)
	return st, nil
}

// runQuickNotePersistNodeInternal 负责“写库工具调用 + 重试态回填”。
func runQuickNotePersistNodeInternal(ctx context.Context, st *QuickNoteState, createTaskTool tool.InvokableTool, input QuickNoteGraphRunInput, emitStage func(stage, detail string)) (*QuickNoteState, error) {
	_ = input // 保留参数形状，后续若需要基于输入开关扩展可直接使用。

	if st == nil {
		return nil, errors.New("quick note graph: nil state in persist node")
	}
	// 1. 非随口记或时间非法时不允许落库。
	if !st.IsQuickNoteIntent || strings.TrimSpace(st.DeadlineValidationError) != "" {
		return st, nil
	}

	// 2. 准备工具入参：优先使用已评估优先级，缺失则兜底。
	emitStage("quick_note.persisting", "正在写入任务数据。")
	priority := st.ExtractedPriority
	if !IsValidTaskPriority(priority) {
		priority = fallbackPriority(st)
		st.ExtractedPriority = priority
	}

	deadlineText := ""
	if st.ExtractedDeadline != nil {
		deadlineText = st.ExtractedDeadline.In(quickNoteLocation()).Format(time.RFC3339)
	}

	// 3. 工具参数序列化失败视作一次失败尝试，交由重试分支处理。
	toolInput := QuickNoteCreateTaskToolInput{
		Title:         st.ExtractedTitle,
		PriorityGroup: priority,
		DeadlineAt:    deadlineText,
	}
	rawInput, marshalErr := json.Marshal(toolInput)
	if marshalErr != nil {
		st.RecordToolError("构造工具参数失败: " + marshalErr.Error())
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，记录任务时参数处理失败，请稍后重试。"
			emitStage("quick_note.failed", "参数构造失败，未完成写入。")
		}
		return st, nil
	}

	// 4. 调用写库工具。
	rawOutput, invokeErr := createTaskTool.InvokableRun(ctx, string(rawInput))
	if invokeErr != nil {
		st.RecordToolError(invokeErr.Error())
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，我尝试了多次仍未能成功记录这条任务，请稍后再试。"
			emitStage("quick_note.failed", "多次重试后仍未完成写入。")
		}
		return st, nil
	}

	// 5. 工具返回解析失败同样按“可重试错误”处理。
	toolOutput, parseErr := parseJSONPayload[QuickNoteCreateTaskToolOutput](rawOutput)
	if parseErr != nil {
		st.RecordToolError("解析工具返回失败: " + parseErr.Error())
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，我拿到了异常结果，没能确认任务是否记录成功，请稍后再试。"
			emitStage("quick_note.failed", "结果解析异常，无法确认写入结果。")
		}
		return st, nil
	}

	// 成功判定硬门槛：必须拿到有效 task_id，防止“假成功”。
	if toolOutput.TaskID <= 0 {
		st.RecordToolError(fmt.Sprintf("工具返回非法 task_id=%d", toolOutput.TaskID))
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，这次我没能确认任务写入成功，请再发一次我立刻补上。"
			emitStage("quick_note.failed", "写入结果缺少有效 task_id，已终止成功回包。")
		}
		return st, nil
	}

	// 6. 写库成功后回填状态，并准备最终回复内容。
	st.RecordToolSuccess(toolOutput.TaskID)
	if strings.TrimSpace(toolOutput.Title) != "" {
		st.ExtractedTitle = strings.TrimSpace(toolOutput.Title)
	}
	if IsValidTaskPriority(toolOutput.PriorityGroup) {
		st.ExtractedPriority = toolOutput.PriorityGroup
	}

	reply := strings.TrimSpace(toolOutput.Message)
	if reply == "" {
		reply = fmt.Sprintf("已为你记录：%s（%s）", st.ExtractedTitle, PriorityLabelCN(st.ExtractedPriority))
	}
	st.AssistantReply = reply
	emitStage("quick_note.persisted", "任务写入成功，正在组织回复内容。")
	return st, nil
}

// selectQuickNoteNextAfterIntent 根据意图与时间校验结果决定 intent 后分支。
func selectQuickNoteNextAfterIntent(st *QuickNoteState) string {
	// 1) 非随口记 -> exit；
	// 2) 时间校验失败 -> exit；
	// 3) 其余 -> priority 节点。
	if st == nil || !st.IsQuickNoteIntent {
		return quickNoteGraphNodeExit
	}
	if strings.TrimSpace(st.DeadlineValidationError) != "" {
		return quickNoteGraphNodeExit
	}
	return quickNoteGraphNodeRank
}

// selectQuickNoteNextAfterPersist 根据持久化状态决定 persist 后分支。
func selectQuickNoteNextAfterPersist(st *QuickNoteState) string {
	// 分支规则：
	// 1) state=nil：防御式结束；
	// 2) 已持久化：结束；
	// 3) 可重试：回到 persist 重试；
	// 4) 不可重试：写失败文案并结束。
	if st == nil {
		return compose.END
	}
	if st.Persisted {
		return compose.END
	}
	if st.CanRetryTool() {
		return quickNoteGraphNodePersist
	}
	if strings.TrimSpace(st.AssistantReply) == "" {
		st.AssistantReply = "抱歉，我尝试了多次仍未能成功记录这条任务，请稍后再试。"
	}
	return compose.END
}

func getInvokableToolByName(bundle *QuickNoteToolBundle, name string) (tool.InvokableTool, error) {
	// 1. 校验工具包有效性。
	if bundle == nil {
		return nil, errors.New("tool bundle is nil")
	}
	if len(bundle.Tools) == 0 || len(bundle.ToolInfos) == 0 {
		return nil, errors.New("tool bundle is empty")
	}
	// 2. 通过 ToolInfo 名称定位并拿到同索引的 Tool 实例。
	for idx, info := range bundle.ToolInfos {
		if info == nil || info.Name != name {
			continue
		}
		invokable, ok := bundle.Tools[idx].(tool.InvokableTool)
		if !ok {
			return nil, fmt.Errorf("tool %s is not invokable", name)
		}
		return invokable, nil
	}
	return nil, fmt.Errorf("tool %s not found", name)
}

func callModelForJSON(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string) (string, error) {
	// 默认 JSON 输出场景 token 足够小，使用 256 作为保守上限。
	return callModelForJSONWithMaxTokens(ctx, chatModel, systemPrompt, userPrompt, 256)
}

func callModelForJSONWithMaxTokens(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	// 1. 构造 system + user 两段消息。
	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userPrompt),
	}
	// 2. 统一关闭 thinking，降低额外延迟，并用温度 0 提升结构化稳定性。
	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
	}
	if maxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(maxTokens))
	}

	// 3. 调模型并对空响应做防御校验。
	resp, err := chatModel.Generate(ctx, messages, opts...)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("模型返回为空")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", errors.New("模型返回内容为空")
	}
	return content, nil
}

type quickNotePlannedResult struct {
	Title          string
	Deadline       *time.Time
	DeadlineText   string
	PriorityGroup  int
	PriorityReason string
	Banter         string
}

// planQuickNoteInSingleCall 在一次模型调用里完成“时间/优先级/banter”聚合规划。
func planQuickNoteInSingleCall(
	ctx context.Context,
	chatModel *ark.ChatModel,
	nowText string,
	now time.Time,
	userInput string,
) (*quickNotePlannedResult, error) {
	// 1. 构造聚合 prompt：一次返回所有结构化字段，减少多次 LLM 往返。
	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
用户输入：%s

请仅输出 JSON（不要 markdown，不要解释），字段如下：
{
  "title": string,
  "deadline_at": string,
  "priority_group": 1|2|3|4,
  "priority_reason": string,
  "banter": string
}

约束：
1) deadline_at 只允许 "yyyy-MM-dd HH:mm" 或空字符串；
2) 若用户给了相对时间（如明天/今晚/下周一），必须换算为绝对时间；
3) banter 只允许一句中文，不超过30字，不得改动任务事实。`,
		nowText,
		strings.TrimSpace(userInput),
	)

	// 2. 控制 maxTokens，避免模型冗长输出导致延迟上升。
	raw, err := callModelForJSONWithMaxTokens(ctx, chatModel, QuickNotePlanPrompt, prompt, 220)
	if err != nil {
		return nil, err
	}
	// 3. 解析模型输出 JSON。
	parsed, parseErr := parseJSONPayload[quickNotePlanModelOutput](raw)
	if parseErr != nil {
		return nil, parseErr
	}

	result := &quickNotePlannedResult{
		Title:          strings.TrimSpace(parsed.Title),
		DeadlineText:   strings.TrimSpace(parsed.DeadlineAt),
		PriorityGroup:  parsed.PriorityGroup,
		PriorityReason: strings.TrimSpace(parsed.PriorityReason),
		Banter:         strings.TrimSpace(parsed.Banter),
	}

	// 4. banter 只保留首行，防止模型输出多行破坏最终回复风格。
	if result.Banter != "" {
		if idx := strings.Index(result.Banter, "\n"); idx >= 0 {
			result.Banter = strings.TrimSpace(result.Banter[:idx])
		}
	}

	// 5. 对 deadline 做本地二次校验，确保可落库。
	if result.DeadlineText != "" {
		if deadline, deadlineErr := parseOptionalDeadlineWithNow(result.DeadlineText, now); deadlineErr == nil {
			result.Deadline = deadline
		}
	}
	return result, nil
}

func parseJSONPayload[T any](raw string) (*T, error) {
	// 1. 空字符串直接失败。
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, errors.New("empty response")
	}

	// 2. 兼容 ```json ... ``` 包裹输出。
	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	// 3. 先尝试整体反序列化（最快路径）。
	var out T
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}

	// 4. 若模型附带额外文本，则提取最外层 JSON 对象再解析。
	obj := extractJSONObject(clean)
	if obj == "" {
		return nil, fmt.Errorf("no json object found in: %s", clean)
	}
	if err := json.Unmarshal([]byte(obj), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func extractJSONObject(text string) string {
	// 简化提取策略：取首个“{”到最后“}”的片段。
	// 对当前 prompt 场景足够稳定，且实现成本低。
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func fallbackPriority(st *QuickNoteState) int {
	// 兜底规则：
	// 1) 有截止时间且 <=48h：重要且紧急；
	// 2) 有截止时间但较远：重要不紧急；
	// 3) 无截止时间：简单不重要。
	if st == nil {
		return QuickNotePrioritySimpleNotImportant
	}
	if st.ExtractedDeadline != nil {
		if time.Until(*st.ExtractedDeadline) <= 48*time.Hour {
			return QuickNotePriorityImportantUrgent
		}
		return QuickNotePriorityImportantNotUrgent
	}
	return QuickNotePrioritySimpleNotImportant
}

// deriveQuickNoteTitleFromInput 在“跳过二次意图判定”场景下，从用户原句提取任务标题。
func deriveQuickNoteTitleFromInput(userInput string) string {
	// 1. 先清理空白。
	text := strings.TrimSpace(userInput)
	if text == "" {
		return "这条任务"
	}

	// 2. 去掉常见指令前缀，保留核心任务语义。
	prefixes := []string{
		"请帮我", "麻烦帮我", "麻烦你", "帮我", "提醒我", "请提醒我", "记一下", "记个", "帮我记一下",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}

	// 3. 截断“记得/到时候”等尾部提醒语，避免标题过长。
	suffixSeparators := []string{
		"，记得", ",记得", "，到时候", ",到时候", " 到时候", "，别忘了", ",别忘了", "。记得",
	}
	for _, sep := range suffixSeparators {
		if idx := strings.Index(text, sep); idx > 0 {
			text = strings.TrimSpace(text[:idx])
			break
		}
	}

	// 4. 收尾清理标点；若清理后为空则回退原句。
	text = strings.Trim(text, "，,。.!！？；; ")
	if text == "" {
		return strings.TrimSpace(userInput)
	}
	return text
}
