package agentnode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agentllm "github.com/LoveLosita/smartflow/backend/agent/llm"
	agentmodel "github.com/LoveLosita/smartflow/backend/agent/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	// QuickNoteGraphNodeIntent 是随口记图中的“意图识别”节点名。
	QuickNoteGraphNodeIntent = "quick_note_intent"
	// QuickNoteGraphNodeRank 是随口记图中的“优先级评估”节点名。
	QuickNoteGraphNodeRank = "quick_note_priority"
	// QuickNoteGraphNodePersist 是随口记图中的“持久化写库”节点名。
	QuickNoteGraphNodePersist = "quick_note_persist"
	// QuickNoteGraphNodeExit 是随口记图中的“提前退出”节点名。
	QuickNoteGraphNodeExit = "quick_note_exit"
)

// QuickNoteGraphRunInput 描述一次随口记图运行所需的请求级依赖。
//
// 职责边界：
// 1. 负责把模型、初始状态、工具依赖和阶段回调打包给 graph 层。
// 2. 不负责做依赖校验，校验逻辑由 graph/node 构造阶段处理。
type QuickNoteGraphRunInput struct {
	Model                  *ark.ChatModel
	State                  *agentmodel.QuickNoteState
	Deps                   QuickNoteToolDeps
	SkipIntentVerification bool
	EmitStage              func(stage, detail string)
}

// QuickNoteNodes 是随口记图的节点容器。
//
// 职责边界：
// 1. 负责承接节点运行时依赖，并向 graph 暴露可直接挂载的方法。
// 2. 不负责 graph 编译，也不负责 service 层接口接线。
type QuickNoteNodes struct {
	input          QuickNoteGraphRunInput
	createTaskTool tool.InvokableTool
	emitStage      func(stage, detail string)
}

// NewQuickNoteNodes 负责构造随口记节点容器。
//
// 输入输出语义：
// 1. createTaskTool 不能为空，否则 persist 节点无法落库。
// 2. EmitStage 为空时会回退到空实现，避免节点内部到处判空。
func NewQuickNoteNodes(input QuickNoteGraphRunInput, createTaskTool tool.InvokableTool) (*QuickNoteNodes, error) {
	if createTaskTool == nil {
		return nil, errors.New("quick note nodes: createTaskTool is nil")
	}

	emitStage := input.EmitStage
	if emitStage == nil {
		emitStage = func(stage, detail string) {}
	}

	return &QuickNoteNodes{
		input:          input,
		createTaskTool: createTaskTool,
		emitStage:      emitStage,
	}, nil
}

// Exit 是图中的显式退出节点。
//
// 职责边界：
// 1. 仅作为图收口占位，保持状态原样透传。
// 2. 不做额外业务处理，避免退出节点再引入副作用。
func (n *QuickNoteNodes) Exit(ctx context.Context, st *agentmodel.QuickNoteState) (*agentmodel.QuickNoteState, error) {
	_ = ctx
	return st, nil
}

// NextAfterIntent 根据意图识别结果决定 intent 节点后的分支走向。
//
// 步骤说明：
// 1. 非随口记意图时直接退出，避免误把普通聊天写成任务。
// 2. 截止时间校验失败时同样直接退出，让上层优先把错误提示给用户。
// 3. 只有意图成立且时间合法，才进入优先级评估节点。
func (n *QuickNoteNodes) NextAfterIntent(ctx context.Context, st *agentmodel.QuickNoteState) (string, error) {
	_ = ctx
	if st == nil || !st.IsQuickNoteIntent {
		return QuickNoteGraphNodeExit, nil
	}
	if st.DeadlineValidationError != "" {
		return QuickNoteGraphNodeExit, nil
	}
	return QuickNoteGraphNodeRank, nil
}

// NextAfterPersist 根据持久化结果决定 persist 节点后的分支走向。
//
// 输入输出语义：
// 1. Persisted=true 表示已经成功写库，可以直接结束。
// 2. Persisted=false 且 CanRetryTool()=true 表示继续重试写库。
// 3. 重试用尽后会补齐兜底回复，再结束链路，避免用户拿到空响应。
func (n *QuickNoteNodes) NextAfterPersist(ctx context.Context, st *agentmodel.QuickNoteState) (string, error) {
	_ = ctx
	if st == nil {
		return compose.END, nil
	}
	if st.Persisted {
		return compose.END, nil
	}
	if st.CanRetryTool() {
		return QuickNoteGraphNodePersist, nil
	}
	if st.AssistantReply == "" {
		st.AssistantReply = "抱歉，我已经重试了多次，还是没能成功记录这条任务，请稍后再试。"
	}
	return compose.END, nil
}

// Intent 负责“意图识别 + 聚合规划 + 时间校验”。
//
// 职责边界：
// 1. 负责判断本次请求是否属于随口记；
// 2. 负责把模型规划结果回填到 state；
// 3. 负责做最后一层本地时间硬校验，避免非法时间被静默写成 NULL；
// 4. 不负责真正写库。
func (n *QuickNoteNodes) Intent(ctx context.Context, st *agentmodel.QuickNoteState) (*agentmodel.QuickNoteState, error) {
	if st == nil {
		return nil, errors.New("quick note graph: nil state in intent node")
	}

	// 1. 若上游路由已经高置信命中 quick_note，则直接进入单次聚合规划。
	// 1.1 目的：尽量把“标题 / 时间 / 优先级 / banter”压缩到一次模型往返内；
	// 1.2 失败处理：若聚合规划失败，不中断整条链路，而是回退到本地兜底，保证可用性优先。
	if n.input.SkipIntentVerification {
		n.emitStage("quick_note.intent.analyzing", "已由上游路由判定为任务请求，跳过二次意图判断。")
		st.IsQuickNoteIntent = true
		st.IntentJudgeReason = "上游路由已命中 quick_note，跳过二次意图判定"
		st.PlannedBySingleCall = true

		n.emitStage("quick_note.plan.generating", "正在一次性生成时间归一化、优先级与回复润色。")
		plan, planErr := planQuickNoteInSingleCall(ctx, n.input.Model, st.RequestNowText, st.RequestNow, st.UserInput)
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
			if plan.UrgencyThreshold != nil {
				st.ExtractedUrgencyThreshold = normalizeUrgencyThreshold(plan.UrgencyThreshold, plan.Deadline)
			}
			if agentmodel.IsValidTaskPriority(plan.PriorityGroup) {
				st.ExtractedPriority = plan.PriorityGroup
				st.ExtractedPriorityReason = strings.TrimSpace(plan.PriorityReason)
			}
			st.ExtractedBanter = strings.TrimSpace(plan.Banter)
		}

		// 1.3 如果聚合规划没能给出标题，则回退到本地标题抽取，避免后续 persist 节点拿到空标题。
		if strings.TrimSpace(st.ExtractedTitle) == "" {
			st.ExtractedTitle = deriveQuickNoteTitleFromInput(st.UserInput)
		}

		// 1.4 最后一定要做一轮本地时间硬校验。
		// 1.4.1 原因：模型即使给了时间，也可能和用户原句不一致，或者用户原句本身就是非法时间；
		// 1.4.2 若检测到“用户给了时间线索但格式非法”，直接退出图并给用户明确修正提示。
		n.emitStage("quick_note.deadline.validating", "正在校验并归一化任务时间。")
		userDeadline, userHasTimeHint, userDeadlineErr := parseOptionalDeadlineFromUserInput(st.UserInput, st.RequestNow)
		if userHasTimeHint && userDeadlineErr != nil {
			st.DeadlineValidationError = userDeadlineErr.Error()
			st.AssistantReply = "我识别到你给了时间信息，但这个时间格式我没法准确解析，请改成例如：2026-03-20 18:30、明天下午3点、下周一上午9点。"
			n.emitStage("quick_note.failed", "时间校验失败，未执行写入。")
			return st, nil
		}
		if userDeadline != nil {
			st.ExtractedDeadline = userDeadline
			st.ExtractedDeadlineText = strings.TrimSpace(st.UserInput)
		}
		return st, nil
	}

	// 2. 常规路径：先做一次意图识别，再做本地时间硬校验。
	n.emitStage("quick_note.intent.analyzing", "正在分析用户输入是否属于任务安排请求。")
	parsed, callErr := agentllm.IdentifyQuickNoteIntent(ctx, n.input.Model, st.RequestNowText, st.UserInput)
	if callErr != nil {
		// 2.1 这里不直接返回 error，而是把它视为“本次未能确认是 quick note”，交给上层回退普通聊天。
		st.IsQuickNoteIntent = false
		st.IntentJudgeReason = "意图识别失败，回退普通聊天"
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

	n.emitStage("quick_note.deadline.validating", "正在校验并归一化任务时间。")

	// 2.2 先尝试吃模型返回的 deadline_at，用于减少后续重复推理。
	st.ExtractedDeadlineText = strings.TrimSpace(parsed.DeadlineAt)
	if st.ExtractedDeadlineText != "" {
		if deadline, deadlineErr := ParseOptionalDeadlineWithNow(st.ExtractedDeadlineText, st.RequestNow); deadlineErr == nil {
			st.ExtractedDeadline = deadline
		}
	}

	// 2.3 再强制对用户原句做一次时间线索校验。
	userDeadline, userHasTimeHint, userDeadlineErr := parseOptionalDeadlineFromUserInput(st.UserInput, st.RequestNow)
	if userHasTimeHint && userDeadlineErr != nil {
		st.DeadlineValidationError = userDeadlineErr.Error()
		st.AssistantReply = "我识别到你给了时间信息，但这个时间格式我没法准确解析，请改成例如：2026-03-20 18:30、明天下午3点、下周一上午9点。"
		n.emitStage("quick_note.failed", "时间校验失败，未执行写入。")
		return st, nil
	}

	// 2.4 若模型没提到 deadline，但用户原句能解析出来，则以用户原句为准补齐。
	if st.ExtractedDeadline == nil && userDeadline != nil {
		st.ExtractedDeadline = userDeadline
		if st.ExtractedDeadlineText == "" {
			st.ExtractedDeadlineText = strings.TrimSpace(st.UserInput)
		}
	}
	return st, nil
}

// Priority 负责“优先级评估”。
//
// 职责边界：
// 1. 负责在 intent 节点之后补齐 priority_group；
// 2. 若聚合规划已经给出合法优先级，则直接复用，不再重复调用模型；
// 3. 若模型评估失败，则使用本地兜底策略，保证链路继续可走；
// 4. 不负责写库。
func (n *QuickNoteNodes) Priority(ctx context.Context, st *agentmodel.QuickNoteState) (*agentmodel.QuickNoteState, error) {
	if st == nil {
		return nil, errors.New("quick note graph: nil state in priority node")
	}
	if !st.IsQuickNoteIntent || strings.TrimSpace(st.DeadlineValidationError) != "" {
		return st, nil
	}

	// 1. 聚合规划已经给出合法优先级时，直接复用，避免重复调模型。
	if agentmodel.IsValidTaskPriority(st.ExtractedPriority) {
		if strings.TrimSpace(st.ExtractedPriorityReason) == "" {
			st.ExtractedPriorityReason = "复用聚合规划优先级"
		}
		n.emitStage("quick_note.priority.evaluating", "已复用聚合规划结果中的优先级。")
		return st, nil
	}

	// 2. 单请求聚合路径若没有给出合法 priority，则直接走本地兜底，优先保证低时延。
	if n.input.SkipIntentVerification || st.PlannedBySingleCall {
		st.ExtractedPriority = fallbackPriority(st)
		st.ExtractedPriorityReason = "聚合规划未给出合法优先级，使用本地兜底"
		n.emitStage("quick_note.priority.evaluating", "聚合优先级缺失，已使用本地兜底。")
		return st, nil
	}

	n.emitStage("quick_note.priority.evaluating", "正在评估任务优先级。")
	deadlineText := "无"
	if st.ExtractedDeadline != nil {
		deadlineText = formatQuickNoteTimeToMinute(*st.ExtractedDeadline)
	}
	deadlineClue := strings.TrimSpace(st.ExtractedDeadlineText)
	if deadlineClue == "" {
		deadlineClue = "无"
	}

	parsed, callErr := agentllm.PlanQuickNotePriority(ctx, n.input.Model, st.RequestNowText, st.ExtractedTitle, st.UserInput, deadlineClue, deadlineText)
	if callErr != nil {
		st.ExtractedPriority = fallbackPriority(st)
		st.ExtractedPriorityReason = "优先级评估失败，使用兜底策略"
		return st, nil
	}
	if parsed == nil || !agentmodel.IsValidTaskPriority(parsed.PriorityGroup) {
		st.ExtractedPriority = fallbackPriority(st)
		st.ExtractedPriorityReason = "优先级结果异常，使用兜底策略"
		return st, nil
	}

	st.ExtractedPriority = parsed.PriorityGroup
	st.ExtractedPriorityReason = strings.TrimSpace(parsed.Reason)
	if strings.TrimSpace(parsed.UrgencyThresholdAt) != "" {
		urgencyThreshold, thresholdErr := ParseOptionalDeadlineWithNow(strings.TrimSpace(parsed.UrgencyThresholdAt), st.RequestNow)
		if thresholdErr == nil {
			st.ExtractedUrgencyThreshold = normalizeUrgencyThreshold(urgencyThreshold, st.ExtractedDeadline)
		}
	}
	return st, nil
}

// Persist 负责“调工具写库 + 有限次重试状态回填”。
//
// 职责边界：
// 1. 负责把 state 中已提取出的标题、时间、优先级组装成工具入参；
// 2. 负责调用 createTaskTool 执行真正写库；
// 3. 负责把成功/失败结果回填到 state，供后续分支与回复使用；
// 4. 不负责最终回复润色，不负责 service 层的 Redis 与持久化收尾。
func (n *QuickNoteNodes) Persist(ctx context.Context, st *agentmodel.QuickNoteState) (*agentmodel.QuickNoteState, error) {
	if st == nil {
		return nil, errors.New("quick note graph: nil state in persist node")
	}
	if !st.IsQuickNoteIntent || strings.TrimSpace(st.DeadlineValidationError) != "" {
		return st, nil
	}

	n.emitStage("quick_note.persisting", "正在写入任务数据。")
	priority := st.ExtractedPriority
	if !agentmodel.IsValidTaskPriority(priority) {
		priority = fallbackPriority(st)
		st.ExtractedPriority = priority
	}

	deadlineText := ""
	if st.ExtractedDeadline != nil {
		deadlineText = st.ExtractedDeadline.In(QuickNoteLocation()).Format(time.RFC3339)
	}
	urgencyThresholdText := ""
	if st.ExtractedUrgencyThreshold != nil {
		urgencyThresholdText = st.ExtractedUrgencyThreshold.In(QuickNoteLocation()).Format(time.RFC3339)
	}

	toolInput := QuickNoteCreateTaskToolInput{
		Title:              st.ExtractedTitle,
		PriorityGroup:      priority,
		DeadlineAt:         deadlineText,
		UrgencyThresholdAt: urgencyThresholdText,
	}
	rawInput, marshalErr := json.Marshal(toolInput)
	if marshalErr != nil {
		st.RecordToolError("构造工具参数失败: " + marshalErr.Error())
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，记录任务时参数处理失败，请稍后重试。"
			n.emitStage("quick_note.failed", "参数构造失败，未完成写入。")
		}
		return st, nil
	}

	rawOutput, invokeErr := n.createTaskTool.InvokableRun(ctx, string(rawInput))
	if invokeErr != nil {
		st.RecordToolError(invokeErr.Error())
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，我尝试了多次仍未能成功记录这条任务，请稍后再试。"
			n.emitStage("quick_note.failed", "多次重试后仍未完成写入。")
		}
		return st, nil
	}

	toolOutput, parseErr := agentllm.ParseJSONObject[QuickNoteCreateTaskToolOutput](rawOutput)
	if parseErr != nil {
		st.RecordToolError("解析工具返回失败: " + parseErr.Error())
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，我拿到了异常结果，没能确认任务是否记录成功，请稍后再试。"
			n.emitStage("quick_note.failed", "结果解析异常，无法确认写入结果。")
		}
		return st, nil
	}
	if toolOutput.TaskID <= 0 {
		st.RecordToolError(fmt.Sprintf("工具返回非法 task_id=%d", toolOutput.TaskID))
		if !st.CanRetryTool() {
			st.AssistantReply = "抱歉，这次我没能确认任务写入成功，请再发一次我立刻补上。"
			n.emitStage("quick_note.failed", "写入结果缺少有效 task_id，已终止成功回包。")
		}
		return st, nil
	}

	// 1. 只有拿到有效 task_id，才视为真正写入成功；
	// 2. 这样可以避免出现“返回成功文案，但数据库里根本没记录”的假成功。
	st.RecordToolSuccess(toolOutput.TaskID)
	if strings.TrimSpace(toolOutput.Title) != "" {
		st.ExtractedTitle = strings.TrimSpace(toolOutput.Title)
	}
	if agentmodel.IsValidTaskPriority(toolOutput.PriorityGroup) {
		st.ExtractedPriority = toolOutput.PriorityGroup
	}

	reply := strings.TrimSpace(toolOutput.Message)
	if reply == "" {
		reply = fmt.Sprintf("已为你记录：%s（%s）", st.ExtractedTitle, agentmodel.PriorityLabelCN(st.ExtractedPriority))
	}
	st.AssistantReply = reply
	n.emitStage("quick_note.persisted", "任务写入成功，正在组织回复内容。")
	return st, nil
}

type quickNotePlannedResult struct {
	Title                string
	Deadline             *time.Time
	DeadlineText         string
	UrgencyThreshold     *time.Time
	UrgencyThresholdText string
	PriorityGroup        int
	PriorityReason       string
	Banter               string
}

// planQuickNoteInSingleCall 在一次模型调用里完成“时间 / 优先级 / banter”聚合规划。
func planQuickNoteInSingleCall(ctx context.Context, chatModel *ark.ChatModel, nowText string, now time.Time, userInput string) (*quickNotePlannedResult, error) {
	parsed, err := agentllm.PlanQuickNoteInSingleCall(ctx, chatModel, nowText, userInput)
	if err != nil {
		return nil, err
	}

	result := &quickNotePlannedResult{
		Title:                strings.TrimSpace(parsed.Title),
		DeadlineText:         strings.TrimSpace(parsed.DeadlineAt),
		UrgencyThresholdText: strings.TrimSpace(parsed.UrgencyThresholdAt),
		PriorityGroup:        parsed.PriorityGroup,
		PriorityReason:       strings.TrimSpace(parsed.PriorityReason),
		Banter:               strings.TrimSpace(parsed.Banter),
	}
	if result.Banter != "" {
		if idx := strings.Index(result.Banter, "\n"); idx >= 0 {
			result.Banter = strings.TrimSpace(result.Banter[:idx])
		}
	}
	if result.DeadlineText != "" {
		if deadline, deadlineErr := ParseOptionalDeadlineWithNow(result.DeadlineText, now); deadlineErr == nil {
			result.Deadline = deadline
		}
	}
	if result.UrgencyThresholdText != "" {
		if urgencyThreshold, thresholdErr := ParseOptionalDeadlineWithNow(result.UrgencyThresholdText, now); thresholdErr == nil {
			result.UrgencyThreshold = normalizeUrgencyThreshold(urgencyThreshold, result.Deadline)
		}
	}
	return result, nil
}

func normalizeUrgencyThreshold(threshold *time.Time, deadline *time.Time) *time.Time {
	if threshold == nil {
		return nil
	}
	if deadline == nil {
		return threshold
	}
	if threshold.After(*deadline) {
		normalized := *deadline
		return &normalized
	}
	return threshold
}

func fallbackPriority(st *agentmodel.QuickNoteState) int {
	if st == nil {
		return agentmodel.QuickNotePrioritySimpleNotImportant
	}
	if st.ExtractedDeadline != nil {
		if time.Until(*st.ExtractedDeadline) <= 48*time.Hour {
			return agentmodel.QuickNotePriorityImportantUrgent
		}
		return agentmodel.QuickNotePriorityImportantNotUrgent
	}
	return agentmodel.QuickNotePrioritySimpleNotImportant
}

// deriveQuickNoteTitleFromInput 在“跳过二次意图判定”场景下，从用户原句提取任务标题。
func deriveQuickNoteTitleFromInput(userInput string) string {
	text := strings.TrimSpace(userInput)
	if text == "" {
		return "这条任务"
	}

	prefixes := []string{
		"请帮我", "麻烦帮我", "麻烦你", "帮我", "提醒我", "请提醒我", "记一个", "记个", "帮我记一个",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(text, prefix) {
			text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
			break
		}
	}

	suffixSeparators := []string{
		"，记得", ",记得", "，到时候", ",到时候", " 到时候", "，别忘了", ",别忘了", "。记得",
	}
	for _, sep := range suffixSeparators {
		if idx := strings.Index(text, sep); idx > 0 {
			text = strings.TrimSpace(text[:idx])
			break
		}
	}

	text = strings.Trim(text, "，。?!！？ ")
	if text == "" {
		return strings.TrimSpace(userInput)
	}
	return text
}
