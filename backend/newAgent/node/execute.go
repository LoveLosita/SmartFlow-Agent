package newagentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	newagentllm "github.com/LoveLosita/smartflow/backend/newAgent/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/google/uuid"
)

const (
	executeStageName     = "execute"
	executeStatusBlockID = "execute.status"
	executeSpeakBlockID  = "execute.speak"
	executePinnedKey     = "execution_context"
)

// ExecuteNodeInput 描述执行节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 只承载"本轮执行"需要的输入，不负责持久化；
// 2. RuntimeState 提供 plan 步骤与轮次预算；
// 3. ConversationContext 提供历史对话与置顶上下文；
// 4. ToolExecutor 后续由业务层注入，当前先留空。
type ExecuteNodeInput struct {
	RuntimeState        *newagentmodel.AgentRuntimeState
	ConversationContext *newagentmodel.ConversationContext
	UserInput           string
	Client              *newagentllm.Client
	ChunkEmitter        *newagentstream.ChunkEmitter
	ResumeNode          string
}

// ExecuteRoundObservation 记录执行阶段每轮的关键观察。
//
// 设计说明：
// 1. 参考 coding agent 模式，后端只记录事实，不做语义校验；
// 2. ToolResult 存储工具调用的原始返回，供 LLM 下一轮决策；
// 3. 该结构后续可扩展用于调试、回放、审计。
type ExecuteRoundObservation struct {
	Round       int    `json:"round"`
	StepIndex   int    `json:"step_index"`
	GoalCheck   string `json:"goal_check,omitempty"`
	Decision    string `json:"decision,omitempty"`
	ToolName    string `json:"tool_name,omitempty"`
	ToolParams  string `json:"tool_params,omitempty"`
	ToolSuccess bool   `json:"tool_success"`
	ToolResult  string `json:"tool_result,omitempty"`
}

// RunExecuteNode 执行一轮执行节点逻辑。
//
// 核心设计原则：
// 1. LLM 主导：LLM 自己判断 done_when 是否满足，自己决定何时推进/完成；
// 2. 后端兜底：只做资源控制（轮次预算）、安全兜底（防无限循环）、证据记录；
// 3. 不做硬校验：后端不质疑 LLM 的 advance/complete 决策，信任 LLM 判断。
//
// 步骤说明：
//  1. 校验最小依赖，推送"正在执行"状态，避免用户空等；
//  2. 检查当前是否有可执行的 plan 步骤，无计划则报错；
//  3. 构造执行阶段 prompt，调用 LLM 获取决策；
//  4. 若 LLM 先对用户说话，则伪流式推送并写回历史；
//  5. 按 LLM 决策执行动作：
//     5.1 call_tool：执行工具调用，记录证据，推进轮次；
//     5.2 ask_user：打开追问交互，等待用户回复；
//     5.3 advance：LLM 判定当前步骤完成，推进到下一步；
//     5.4 complete：LLM 判定整个任务完成，进入交付阶段；
//  6. 安全兜底：轮次耗尽时强制进入交付，避免无限循环。
func RunExecuteNode(ctx context.Context, input ExecuteNodeInput) error {
	// 1. 校验依赖并准备运行态。
	runtimeState, conversationContext, emitter, err := prepareExecuteNodeInput(input)
	if err != nil {
		return err
	}
	flowState := runtimeState.EnsureCommonState()

	// 2. 检查是否有可执行的 plan 步骤。
	if !flowState.HasCurrentPlanStep() {
		return fmt.Errorf("execute node: 当前无有效 plan 步骤，无法执行")
	}

	// 3. 推送执行阶段状态，让前端知道当前进度。
	current, total := flowState.PlanProgress()
	currentStep, _ := flowState.CurrentPlanStep()
	if err := emitter.EmitStatus(
		executeStatusBlockID,
		executeStageName,
		"executing",
		fmt.Sprintf("正在执行第 %d/%d 步：%s", current, total, truncateText(currentStep.Content, 60)),
		false,
	); err != nil {
		return fmt.Errorf("执行阶段状态推送失败: %w", err)
	}

	// 4. 消耗一轮预算，并检查是否耗尽。
	if !flowState.NextRound() {
		// 轮次耗尽，强制进入交付阶段。
		flowState.Done()
		return nil
	}

	// 5. 构造本轮执行输入，请求 LLM 输出 ExecuteDecision。
	messages := newagentprompt.BuildExecuteMessages(flowState, conversationContext)
	decision, rawResult, err := newagentllm.GenerateJSON[newagentmodel.ExecuteDecision](
		ctx,
		input.Client,
		messages,
		newagentllm.GenerateOptions{
			Temperature: 0.3,
			MaxTokens:   1200,
			Thinking:    newagentllm.ThinkingModeEnabled,
			Metadata: map[string]any{
				"stage":      executeStageName,
				"step_index": flowState.CurrentStep,
				"round_used": flowState.RoundUsed,
			},
		},
	)
	if err != nil {
		if rawResult != nil && strings.TrimSpace(rawResult.Text) != "" {
			return fmt.Errorf("执行决策解析失败，原始输出=%s，错误=%w", strings.TrimSpace(rawResult.Text), err)
		}
		return fmt.Errorf("执行阶段模型调用失败: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return fmt.Errorf("执行决策不合法: %w", err)
	}

	// 自省校验：next_plan / done 必须附带 goal_check，否则不推进，追加修正让 LLM 重试。
	if decision.Action == newagentmodel.ExecuteActionNextPlan ||
		decision.Action == newagentmodel.ExecuteActionDone {
		if strings.TrimSpace(decision.GoalCheck) == "" {
			AppendLLMCorrectionWithHint(
				conversationContext,
				decision.Speak,
				fmt.Sprintf("你输出了 action=%s，但 goal_check 为空。", decision.Action),
				fmt.Sprintf("输出 %s 时，必须在 goal_check 中对照 done_when 逐条说明完成依据。", decision.Action),
			)
			return nil
		}
	}

	// 6. 若 LLM 先对用户说话，则伪流式推送并写回历史。
	if strings.TrimSpace(decision.Speak) != "" {
		if err := emitter.EmitPseudoAssistantText(
			ctx,
			executeSpeakBlockID,
			executeStageName,
			decision.Speak,
			newagentstream.DefaultPseudoStreamOptions(),
		); err != nil {
			return fmt.Errorf("执行文案推送失败: %w", err)
		}
		// 将 LLM 的话追加到对话历史，保证下一轮上下文连续。
		// TODO: 后续需要把工具调用结果也追加到历史，这里先留占位。
	}

	// 7. 按 LLM 决策执行动作，后端信任 LLM 判断，不做语义校验。
	switch decision.Action {
	case newagentmodel.ExecuteActionContinue:
		// 继续当前步骤的 ReAct 循环。
		// 若有工具调用意图，则执行工具并记录证据。
		if decision.ToolCall != nil {
			return executeToolCall(ctx, flowState, conversationContext, decision.ToolCall, emitter)
		}
		// 无工具调用，仅对话，继续下一轮。
		return nil

	case newagentmodel.ExecuteActionAskUser:
		// LLM 判定缺少关键信息，打开追问交互。
		question := resolveExecuteAskUserText(decision)
		runtimeState.OpenAskUserInteraction(uuid.NewString(), question, strings.TrimSpace(input.ResumeNode))
		return nil

	case newagentmodel.ExecuteActionConfirm:
		// LLM 申报了写操作意图，需要用户确认后才能真正执行。
		// 步骤：1) 把 ToolCallIntent 转成快照暂存；2) 设 Phase → 下游 confirm 节点接管。
		return handleExecuteActionConfirm(decision, runtimeState, flowState)

	case newagentmodel.ExecuteActionNextPlan:
		// LLM 判定当前步骤已完成，推进到下一步。
		// 后端信任 LLM 判断，不做硬校验。
		if !flowState.AdvanceStep() {
			// 所有步骤已完成，进入交付阶段。
			flowState.Done()
		}
		return nil

	case newagentmodel.ExecuteActionDone:
		// LLM 判定整个任务已完成，直接进入交付阶段。
		// 后端信任 LLM 判断，不做硬校验。
		flowState.Done()
		return nil

	default:
		// 1. LLM 输出了不支持的 action，不应直接报错终止，而应给它修正机会。
		// 2. 使用通用修正函数追加错误反馈，让 Graph 继续循环。
		// 3. LLM 下一轮会看到错误反馈并修正自己的输出。
		llmOutput := decision.Speak
		if strings.TrimSpace(llmOutput) == "" {
			llmOutput = decision.Reason
		}
		AppendLLMCorrectionWithHint(
			conversationContext,
			llmOutput,
			fmt.Sprintf("你输出的 action \"%s\" 不是合法的执行动作。", decision.Action),
			"合法的 action 包括：continue（继续当前步骤）、ask_user（追问用户）、next_plan（推进到下一步）、done（任务完成）。",
		)
		return nil
	}
}

// prepareExecuteNodeInput 校验并准备执行节点的运行态依赖。
//
// 职责边界：
// 1. 校验必要依赖是否注入；
// 2. 为空依赖提供兜底值，避免空指针；
// 3. 不负责持久化，不负责业务逻辑。
func prepareExecuteNodeInput(input ExecuteNodeInput) (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext, *newagentstream.ChunkEmitter, error) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("execute node: runtime state 不能为空")
	}
	if input.Client == nil {
		return nil, nil, nil, fmt.Errorf("execute node: execute client 未注入")
	}

	input.RuntimeState.EnsureCommonState()
	if input.ConversationContext == nil {
		input.ConversationContext = newagentmodel.NewConversationContext("")
	}
	if input.ChunkEmitter == nil {
		input.ChunkEmitter = newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", time.Now().Unix())
	}
	return input.RuntimeState, input.ConversationContext, input.ChunkEmitter, nil
}

// resolveExecuteAskUserText 解析追问用户的文案。
//
// 优先级：
// 1. 优先使用 LLM 输出的 speak；
// 2. 其次使用 reason；
// 3. 最后使用默认文案。
func resolveExecuteAskUserText(decision *newagentmodel.ExecuteDecision) string {
	if decision == nil {
		return "执行过程中遇到不确定的情况，需要向你确认。"
	}
	if strings.TrimSpace(decision.Speak) != "" {
		return strings.TrimSpace(decision.Speak)
	}
	if strings.TrimSpace(decision.Reason) != "" {
		return strings.TrimSpace(decision.Reason)
	}
	return "执行过程中遇到不确定的情况，需要向你确认。"
}

// handleExecuteActionConfirm 处理 LLM 申报的写操作确认请求。
//
// 步骤：
// 1. 把 ToolCallIntent 转成 PendingToolCallSnapshot 暂存到运行态；
// 2. 设 Phase = PhaseWaitingConfirm，让下游 confirm 节点接管；
// 3. 不执行工具，也不生成确认事件 — 这些都是 confirm 节点的职责。
func handleExecuteActionConfirm(
	decision *newagentmodel.ExecuteDecision,
	runtimeState *newagentmodel.AgentRuntimeState,
	flowState *newagentmodel.CommonState,
) error {
	toolCall := decision.ToolCall

	// 序列化工具参数。
	argsJSON := ""
	if toolCall.Arguments != nil {
		if raw, err := json.Marshal(toolCall.Arguments); err == nil {
			argsJSON = string(raw)
		}
	}

	// 暂存到运行态邮箱，confirm 节点会读出来。
	runtimeState.PendingConfirmTool = &newagentmodel.PendingToolCallSnapshot{
		ToolName: toolCall.Name,
		ArgsJSON: argsJSON,
		Summary:  strings.TrimSpace(decision.Speak),
	}

	// 设 Phase，让 branchAfterExecute 路由到 confirm 节点。
	flowState.Phase = newagentmodel.PhaseWaitingConfirm
	return nil
}

// executeToolCall 执行工具调用并记录证据。
//
// 职责边界：
// 1. 只负责执行工具调用，记录结果；
// 2. 不负责判断工具调用是否成功（由 LLM 下一轮判断）；
// 3. 不负责重试（由外层 Graph 循环控制）。
//
// TODO: 当前为骨架实现，后续需要：
// 1. 接入真实的工具执行器；
// 2. 把工具调用结果追加到对话历史；
// 3. 记录 ExecuteEvidenceReceipt。
func executeToolCall(
	ctx context.Context,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
	toolCall *newagentmodel.ToolCallIntent,
	emitter *newagentstream.ChunkEmitter,
) error {
	if toolCall == nil {
		return nil
	}

	// 当前为骨架实现，仅记录工具调用意图。
	// 后续需要：
	// 1. 根据 toolCall.Name 路由到具体工具执行器；
	// 2. 执行工具调用，获取结果；
	// 3. 记录 ExecuteEvidenceReceipt；
	// 4. 把工具调用结果追加到 conversationContext.History。

	toolName := strings.TrimSpace(toolCall.Name)
	if toolName == "" {
		return fmt.Errorf("工具调用缺少工具名称")
	}

	// 推送工具调用状态，让前端知道当前在做什么。
	if err := emitter.EmitStatus(
		executeStatusBlockID,
		executeStageName,
		"tool_call",
		fmt.Sprintf("正在调用工具：%s", toolName),
		false,
	); err != nil {
		return fmt.Errorf("工具调用状态推送失败: %w", err)
	}

	// TODO: 执行真实工具调用，并记录证据。
	// 伪代码：
	// result := toolRegistry.Execute(ctx, toolCall.Name, toolCall.Arguments)
	// evidence := ExecuteEvidenceReceipt{
	//     StepIndex:       flowState.CurrentStep,
	//     Source:          ExecuteEvidenceSourceToolObservation,
	//     Name:            toolCall.Name,
	//     Success:         result.Success,
	//     Summary:         result.Summary,
	// }
	// flowState.RecordEvidence(evidence)

	return nil
}

// truncateText 截断文本到指定长度。
//
// 用于状态推送时避免超长文本影响前端展示。
func truncateText(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 3 {
		return text[:maxLen]
	}
	return text[:maxLen-3] + "..."
}
