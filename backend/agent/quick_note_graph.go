package agent

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

const (
	quickNoteGraphNodeIntent  = "quick_note_intent"
	quickNoteGraphNodeRank    = "quick_note_priority"
	quickNoteGraphNodePersist = "quick_note_persist"
	quickNoteGraphNodeExit    = "quick_note_exit"
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
// 说明：
// - 路由命中 quick_note 时，尽量通过这一份结果覆盖“时间/优先级/润色”三步；
// - 任一字段异常不应阻断主链路，后续会有本地兜底与校验。
type quickNotePlanModelOutput struct {
	Title          string `json:"title"`
	DeadlineAt     string `json:"deadline_at"`
	PriorityGroup  int    `json:"priority_group"`
	PriorityReason string `json:"priority_reason"`
	Banter         string `json:"banter"`
}

// QuickNoteGraphRunInput 是运行“随口记 graph”所需的输入依赖。
// 说明：
// - EmitStage 可选，用于把节点进度推送给外层（例如 SSE 状态块）；
// - 不传 EmitStage 时，图逻辑保持静默执行。
type QuickNoteGraphRunInput struct {
	Model *ark.ChatModel
	State *QuickNoteState
	Deps  QuickNoteToolDeps

	// SkipIntentVerification=true 时，跳过“意图识别二次模型判定”：
	// - 适用于上游路由已明确给出 quick_note 的场景；
	// - 可减少一次模型调用，降低首包前等待；
	// - 仍保留时间合法性校验与写库成功校验，避免脏数据与假成功。
	SkipIntentVerification bool

	EmitStage func(stage, detail string)
}

// RunQuickNoteGraph 执行“随口记”图编排。
// 设计目标：
// 1) 意图识别和信息抽取与写库解耦；
// 2) 发生模型抖动或工具失败时，具备可控降级和重试；
// 3) 时间解析严格可控，避免把非法日期静默写成 NULL。
func RunQuickNoteGraph(ctx context.Context, input QuickNoteGraphRunInput) (*QuickNoteState, error) {
	if input.Model == nil {
		return nil, errors.New("quick note graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("quick note graph: state is nil")
	}
	if err := input.Deps.validate(); err != nil {
		return nil, err
	}

	emitStage := func(stage, detail string) {
		if input.EmitStage != nil {
			input.EmitStage(stage, detail)
		}
	}

	// 统一初始化“当前时间基准”：
	// - RequestNow 用于相对时间解析；
	// - RequestNowText 用于拼接到提示词，让模型知道“现在是几点”。
	if input.State.RequestNow.IsZero() {
		input.State.RequestNow = quickNoteNowToMinute()
	}
	if strings.TrimSpace(input.State.RequestNowText) == "" {
		input.State.RequestNowText = formatQuickNoteTimeToMinute(input.State.RequestNow)
	}

	toolBundle, err := BuildQuickNoteToolBundle(ctx, input.Deps)
	if err != nil {
		return nil, err
	}
	createTaskTool, err := getInvokableToolByName(toolBundle, ToolNameQuickNoteCreateTask)
	if err != nil {
		return nil, err
	}

	graph := compose.NewGraph[*QuickNoteState, *QuickNoteState]()

	// 节点1：意图识别与信息抽取。
	if err = graph.AddLambdaNode(quickNoteGraphNodeIntent, compose.InvokableLambda(
		func(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
			if st == nil {
				return nil, errors.New("quick note graph: nil state in intent node")
			}

			if input.SkipIntentVerification {
				emitStage("quick_note.intent.analyzing", "已由上游路由判定为任务请求，跳过二次意图判断。")
				st.IsQuickNoteIntent = true
				st.IntentJudgeReason = "上游路由已命中 quick_note，跳过二次意图判定"
				st.PlannedBySingleCall = true

				emitStage("quick_note.plan.generating", "正在一次性生成时间归一化、优先级与回复润色。")
				plan, planErr := planQuickNoteInSingleCall(ctx, input.Model, st.RequestNowText, st.RequestNow, st.UserInput)
				if planErr != nil {
					st.IntentJudgeReason += "；聚合规划失败，回退本地兜底"
				} else {
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

				if strings.TrimSpace(st.ExtractedTitle) == "" {
					st.ExtractedTitle = deriveQuickNoteTitleFromInput(st.UserInput)
				}

				emitStage("quick_note.deadline.validating", "正在校验并归一化任务时间。")

				// 仍以“用户原句”的本地解析与校验作为硬约束，防止模型给出非法时间。
				userDeadline, userHasTimeHint, userDeadlineErr := parseOptionalDeadlineFromUserInput(st.UserInput, st.RequestNow)
				if userHasTimeHint && userDeadlineErr != nil {
					st.DeadlineValidationError = userDeadlineErr.Error()
					st.AssistantReply = "我识别到你给了时间信息，但这个时间格式我没法准确解析，请改成例如：2026-03-20 18:30、明天下午3点、下周一上午9点。"
					emitStage("quick_note.failed", "时间校验失败，未执行写入。")
					return st, nil
				}
				if userDeadline != nil {
					st.ExtractedDeadline = userDeadline
					st.ExtractedDeadlineText = strings.TrimSpace(st.UserInput)
				}
				return st, nil
			}

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
			raw, callErr := callModelForJSON(ctx, input.Model, QuickNoteIntentPrompt, prompt)
			if callErr != nil {
				st.IsQuickNoteIntent = false
				st.IntentJudgeReason = "意图识别失败，回退普通聊天"
				return st, nil
			}

			parsed, parseErr := parseJSONPayload[quickNoteIntentModelOutput](raw)
			if parseErr != nil {
				st.IsQuickNoteIntent = false
				st.IntentJudgeReason = "意图识别结果不可解析，回退普通聊天"
				return st, nil
			}

			st.IsQuickNoteIntent = parsed.IsQuickNote
			st.IntentJudgeReason = strings.TrimSpace(parsed.Reason)
			if !st.IsQuickNoteIntent {
				return st, nil
			}

			title := strings.TrimSpace(parsed.Title)
			if title == "" {
				title = strings.TrimSpace(st.UserInput)
			}
			st.ExtractedTitle = title

			emitStage("quick_note.deadline.validating", "正在校验并归一化任务时间。")

			// Step A：优先尝试解析模型抽取出来的 deadline。
			st.ExtractedDeadlineText = strings.TrimSpace(parsed.DeadlineAt)
			if st.ExtractedDeadlineText != "" {
				if deadline, deadlineErr := parseOptionalDeadlineWithNow(st.ExtractedDeadlineText, st.RequestNow); deadlineErr == nil {
					st.ExtractedDeadline = deadline
				}
			}

			// Step B：基于用户原句执行“本地时间解析 + 合法性校验”。
			userDeadline, userHasTimeHint, userDeadlineErr := parseOptionalDeadlineFromUserInput(st.UserInput, st.RequestNow)
			if userHasTimeHint && userDeadlineErr != nil {
				st.DeadlineValidationError = userDeadlineErr.Error()
				st.AssistantReply = "我识别到你给了时间信息，但这个时间格式我没法准确解析，请改成例如：2026-03-20 18:30、明天下午3点、下周一上午9点。"
				emitStage("quick_note.failed", "时间校验失败，未执行写入。")
				return st, nil
			}

			if st.ExtractedDeadline == nil && userDeadline != nil {
				st.ExtractedDeadline = userDeadline
				if st.ExtractedDeadlineText == "" {
					st.ExtractedDeadlineText = strings.TrimSpace(st.UserInput)
				}
			}
			return st, nil
		})); err != nil {
		return nil, err
	}

	// 节点2：优先级评估。
	if err = graph.AddLambdaNode(quickNoteGraphNodeRank, compose.InvokableLambda(
		func(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
			if st == nil {
				return nil, errors.New("quick note graph: nil state in priority node")
			}
			if !st.IsQuickNoteIntent || strings.TrimSpace(st.DeadlineValidationError) != "" {
				return st, nil
			}

			// 命中“单请求聚合规划”时，优先复用其优先级结果，避免重复模型调用。
			if IsValidTaskPriority(st.ExtractedPriority) {
				if strings.TrimSpace(st.ExtractedPriorityReason) == "" {
					st.ExtractedPriorityReason = "复用聚合规划优先级"
				}
				emitStage("quick_note.priority.evaluating", "已复用聚合规划结果中的优先级。")
				return st, nil
			}
			if input.SkipIntentVerification || st.PlannedBySingleCall {
				st.ExtractedPriority = fallbackPriority(st)
				st.ExtractedPriorityReason = "聚合规划未给出合法优先级，使用本地兜底"
				emitStage("quick_note.priority.evaluating", "聚合优先级缺失，已使用本地兜底。")
				return st, nil
			}

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

			raw, callErr := callModelForJSON(ctx, input.Model, QuickNotePriorityPrompt, prompt)
			if callErr != nil {
				fallback := fallbackPriority(st)
				st.ExtractedPriority = fallback
				st.ExtractedPriorityReason = "优先级评估失败，使用兜底策略"
				return st, nil
			}

			parsed, parseErr := parseJSONPayload[quickNotePriorityModelOutput](raw)
			if parseErr != nil || !IsValidTaskPriority(parsed.PriorityGroup) {
				fallback := fallbackPriority(st)
				st.ExtractedPriority = fallback
				st.ExtractedPriorityReason = "优先级结果异常，使用兜底策略"
				return st, nil
			}

			st.ExtractedPriority = parsed.PriorityGroup
			st.ExtractedPriorityReason = strings.TrimSpace(parsed.Reason)
			return st, nil
		})); err != nil {
		return nil, err
	}

	// 节点3：调用“写库工具”执行持久化。
	if err = graph.AddLambdaNode(quickNoteGraphNodePersist, compose.InvokableLambda(
		func(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
			if st == nil {
				return nil, errors.New("quick note graph: nil state in persist node")
			}
			if !st.IsQuickNoteIntent || strings.TrimSpace(st.DeadlineValidationError) != "" {
				return st, nil
			}

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

			rawOutput, invokeErr := createTaskTool.InvokableRun(ctx, string(rawInput))
			if invokeErr != nil {
				st.RecordToolError(invokeErr.Error())
				if !st.CanRetryTool() {
					st.AssistantReply = "抱歉，我尝试了多次仍未能成功记录这条任务，请稍后再试。"
					emitStage("quick_note.failed", "多次重试后仍未完成写入。")
				}
				return st, nil
			}

			toolOutput, parseErr := parseJSONPayload[QuickNoteCreateTaskToolOutput](rawOutput)
			if parseErr != nil {
				st.RecordToolError("解析工具返回失败: " + parseErr.Error())
				if !st.CanRetryTool() {
					st.AssistantReply = "抱歉，我拿到了异常结果，没能确认任务是否记录成功，请稍后再试。"
					emitStage("quick_note.failed", "结果解析异常，无法确认写入结果。")
				}
				return st, nil
			}

			// 成功判定加硬门槛：必须拿到有效 task_id。
			// 目的：
			// 1) 防止工具返回结构异常时被误判为“写入成功”；
			// 2) 避免出现“回复已安排，但数据库实际没记录”的错误体验；
			// 3) 命中该分支时会走既有重试策略，重试耗尽后明确报错给用户。
			if toolOutput.TaskID <= 0 {
				st.RecordToolError(fmt.Sprintf("工具返回非法 task_id=%d", toolOutput.TaskID))
				if !st.CanRetryTool() {
					st.AssistantReply = "抱歉，这次我没能确认任务写入成功，请再发一次我立刻补上。"
					emitStage("quick_note.failed", "写入结果缺少有效 task_id，已终止成功回包。")
				}
				return st, nil
			}

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
		})); err != nil {
		return nil, err
	}

	if err = graph.AddLambdaNode(quickNoteGraphNodeExit, compose.InvokableLambda(
		func(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
			return st, nil
		})); err != nil {
		return nil, err
	}

	if err = graph.AddEdge(compose.START, quickNoteGraphNodeIntent); err != nil {
		return nil, err
	}
	if err = graph.AddBranch(quickNoteGraphNodeIntent, compose.NewGraphBranch(
		func(ctx context.Context, st *QuickNoteState) (string, error) {
			if st == nil || !st.IsQuickNoteIntent {
				return quickNoteGraphNodeExit, nil
			}
			if strings.TrimSpace(st.DeadlineValidationError) != "" {
				return quickNoteGraphNodeExit, nil
			}
			return quickNoteGraphNodeRank, nil
		},
		map[string]bool{quickNoteGraphNodeRank: true, quickNoteGraphNodeExit: true},
	)); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(quickNoteGraphNodeExit, compose.END); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(quickNoteGraphNodeRank, quickNoteGraphNodePersist); err != nil {
		return nil, err
	}
	if err = graph.AddBranch(quickNoteGraphNodePersist, compose.NewGraphBranch(
		func(ctx context.Context, st *QuickNoteState) (string, error) {
			if st == nil {
				return compose.END, nil
			}
			if st.Persisted {
				return compose.END, nil
			}
			if st.CanRetryTool() {
				return quickNoteGraphNodePersist, nil
			}
			if strings.TrimSpace(st.AssistantReply) == "" {
				st.AssistantReply = "抱歉，我尝试了多次仍未能成功记录这条任务，请稍后再试。"
			}
			return compose.END, nil
		},
		map[string]bool{quickNoteGraphNodePersist: true, compose.END: true},
	)); err != nil {
		return nil, err
	}

	maxSteps := input.State.MaxToolRetry + 10
	if maxSteps < 12 {
		maxSteps = 12
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("QuickNoteGraph"),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}

	return runnable.Invoke(ctx, input.State)
}

func getInvokableToolByName(bundle *QuickNoteToolBundle, name string) (tool.InvokableTool, error) {
	if bundle == nil {
		return nil, errors.New("tool bundle is nil")
	}
	if len(bundle.Tools) == 0 || len(bundle.ToolInfos) == 0 {
		return nil, errors.New("tool bundle is empty")
	}
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
	return callModelForJSONWithMaxTokens(ctx, chatModel, systemPrompt, userPrompt, 256)
}

func callModelForJSONWithMaxTokens(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, maxTokens int) (string, error) {
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
// 设计原则：
// 1) 路由已命中 quick_note 时优先走该函数，减少串行模型调用；
// 2) 输出字段解析失败时返回 error，让上层回退到本地/后续节点兜底；
// 3) 对 banter 做轻量清洗，避免多行输出污染最终回复。
func planQuickNoteInSingleCall(
	ctx context.Context,
	chatModel *ark.ChatModel,
	nowText string,
	now time.Time,
	userInput string,
) (*quickNotePlannedResult, error) {
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

	raw, err := callModelForJSONWithMaxTokens(ctx, chatModel, QuickNotePlanPrompt, prompt, 220)
	if err != nil {
		return nil, err
	}
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

	if result.Banter != "" {
		if idx := strings.Index(result.Banter, "\n"); idx >= 0 {
			result.Banter = strings.TrimSpace(result.Banter[:idx])
		}
	}

	if result.DeadlineText != "" {
		if deadline, deadlineErr := parseOptionalDeadlineWithNow(result.DeadlineText, now); deadlineErr == nil {
			result.Deadline = deadline
		}
	}
	return result, nil
}

func parseJSONPayload[T any](raw string) (*T, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil, errors.New("empty response")
	}

	if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```json")
		clean = strings.TrimPrefix(clean, "```")
		clean = strings.TrimSuffix(clean, "```")
		clean = strings.TrimSpace(clean)
	}

	var out T
	if err := json.Unmarshal([]byte(clean), &out); err == nil {
		return &out, nil
	}

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
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start == -1 || end == -1 || end <= start {
		return ""
	}
	return text[start : end+1]
}

func fallbackPriority(st *QuickNoteState) int {
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
// 设计原则：
// 1) 不依赖模型，避免再引入一次额外 LLM 调用；
// 2) 优先保守提取，宁可稍长，也不要误删关键信息；
// 3) 只做轻量清洗，不做复杂语义改写，保持可预期。
func deriveQuickNoteTitleFromInput(userInput string) string {
	text := strings.TrimSpace(userInput)
	if text == "" {
		return "这条任务"
	}

	// 去掉常见前缀口头语，保留核心任务描述。
	prefixes := []string{
		"请帮我", "麻烦帮我", "麻烦你", "帮我", "提醒我", "请提醒我", "记一下", "记个", "帮我记一下",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}

	// 去掉常见尾部提醒口头语，避免把“记得喊我/q我”也写入标题。
	suffixSeparators := []string{
		"，记得", ",记得", "，到时候", ",到时候", " 到时候", "，别忘了", ",别忘了", "。记得",
	}
	for _, sep := range suffixSeparators {
		if idx := strings.Index(text, sep); idx > 0 {
			text = strings.TrimSpace(text[:idx])
			break
		}
	}

	text = strings.Trim(text, "，,。.!！？；; ")
	if text == "" {
		return strings.TrimSpace(userInput)
	}
	return text
}
