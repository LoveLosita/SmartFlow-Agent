package newagentnode

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	newagentllm "github.com/LoveLosita/smartflow/backend/newAgent/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
)

const (
	chatStageName     = "chat"
	chatStatusBlockID = "chat.status"
	chatSpeakBlockID  = "chat.speak"
)

// ChatNodeInput 描述聊天节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 只承载"本轮 chat"需要的输入，不负责持久化；
// 2. RuntimeState 提供 pending interaction 与流程状态；
// 3. ConversationContext 提供历史对话；
// 4. ConfirmAction 仅在 confirm 恢复场景下由前端传入 "accept" / "reject"。
type ChatNodeInput struct {
	RuntimeState        *newagentmodel.AgentRuntimeState
	ConversationContext *newagentmodel.ConversationContext
	UserInput           string
	ConfirmAction       string
	Client              *newagentllm.Client
	ChunkEmitter        *newagentstream.ChunkEmitter
}

// RunChatNode 执行一轮聊天节点逻辑。
//
// 核心职责：
// 1. 恢复判定：有 pending interaction 则处理恢复；
// 2. 路由分流：无 pending 时，调 LLM 判断复杂度并路由；
// 3. direct_reply：简单任务，直接输出回复 → END；
// 4. execute：中等任务，推 Execute ReAct；
// 5. deep_answer：复杂问答，原地开 thinking 深度回答 → END；
// 6. plan：复杂规划，推 Plan 节点。
func RunChatNode(ctx context.Context, input ChatNodeInput) error {
	runtimeState, conversationContext, emitter, err := prepareChatNodeInput(input)
	if err != nil {
		return err
	}

	// 1. 有 pending interaction → 纯状态传递，处理恢复。
	if runtimeState.HasPendingInteraction() {
		return handleChatResume(input, runtimeState, conversationContext, emitter)
	}

	// 2. 无 pending → 路由决策（一次快速 LLM 调用，不开 thinking）。
	flowState := runtimeState.EnsureCommonState()
	messages := newagentprompt.BuildChatRoutingMessages(conversationContext, input.UserInput, flowState)

	decision, rawResult, err := newagentllm.GenerateJSON[newagentmodel.ChatRoutingDecision](
		ctx,
		input.Client,
		messages,
		newagentllm.GenerateOptions{
			Temperature: 0.1,
			MaxTokens:   500,
			Thinking:    newagentllm.ThinkingModeDisabled,
			Metadata: map[string]any{
				"stage": chatStageName,
				"phase": "routing",
			},
		},
	)

	rawText := ""
	if rawResult != nil {
		rawText = strings.TrimSpace(rawResult.Text)
	}

	if err != nil {
		// 路由失败 → 保守：走 plan。
		log.Printf("[WARN] chat routing LLM failed chat=%s raw=%s err=%v",
			flowState.ConversationID, rawText, err)
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	}

	if validateErr := decision.Validate(); validateErr != nil {
		log.Printf("[WARN] chat routing decision invalid chat=%s raw=%s err=%v",
			flowState.ConversationID, rawText, validateErr)
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	}

	log.Printf("[DEBUG] chat routing chat=%s route=%s reason=%s",
		flowState.ConversationID, decision.Route, decision.Reason)

	// 3. 按路由决策推进。
	switch decision.Route {
	case newagentmodel.ChatRouteDirectReply:
		return handleDirectReply(ctx, decision, conversationContext, emitter, flowState)

	case newagentmodel.ChatRouteExecute:
		return handleRouteExecute(decision, emitter, flowState)

	case newagentmodel.ChatRouteDeepAnswer:
		return handleDeepAnswer(ctx, input, decision, conversationContext, emitter, flowState)

	case newagentmodel.ChatRoutePlan:
		return handleRoutePlan(decision, emitter, flowState)

	default:
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	}
}

// handleDirectReply 处理简单任务：直接输出回复。
func handleDirectReply(
	ctx context.Context,
	decision *newagentmodel.ChatRoutingDecision,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
) error {
	if strings.TrimSpace(decision.Speak) != "" {
		if err := emitter.EmitPseudoAssistantText(
			ctx, chatSpeakBlockID, chatStageName,
			decision.Speak,
			newagentstream.DefaultPseudoStreamOptions(),
		); err != nil {
			return fmt.Errorf("闲聊回复推送失败: %w", err)
		}
		conversationContext.AppendHistory(schema.AssistantMessage(decision.Speak, nil))
	}

	flowState.Phase = newagentmodel.PhaseChatting
	return nil
}

// handleRouteExecute 处理中等任务：推送简短确认，设 PhaseExecuting。
//
// 不把 speak 写入 history，因为真正的回复由 Execute 节点产出。
func handleRouteExecute(
	decision *newagentmodel.ChatRoutingDecision,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
) error {
	speak := strings.TrimSpace(decision.Speak)
	if speak == "" {
		speak = "好的，我来处理。"
	}

	// 推送轻量状态通知，让前端知道请求已接收。
	_ = emitter.EmitStatus(chatStatusBlockID, chatStageName, "accepted", speak, false)

	// 清空旧 PlanSteps 并设 PhaseExecuting，避免上一次任务残留的步骤被 HasPlan() 误判。
	flowState.StartDirectExecute()

	// 安全兜底：只有真正持有 task_class_ids 时才开粗排。
	if decision.NeedsRoughBuild && len(flowState.TaskClassIDs) > 0 {
		flowState.NeedsRoughBuild = true
	}

	return nil
}

// handleDeepAnswer 处理复杂问答：推送过渡语 → 原地开 thinking 再调一次 LLM → 输出深度回答。
func handleDeepAnswer(
	ctx context.Context,
	input ChatNodeInput,
	decision *newagentmodel.ChatRoutingDecision,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
) error {
	// 1. 推送过渡语。
	briefSpeak := strings.TrimSpace(decision.Speak)
	if briefSpeak == "" {
		briefSpeak = "让我想想。"
	}
	if err := emitter.EmitPseudoAssistantText(
		ctx, chatSpeakBlockID, chatStageName,
		briefSpeak,
		newagentstream.DefaultPseudoStreamOptions(),
	); err != nil {
		return fmt.Errorf("过渡文案推送失败: %w", err)
	}

	// 2. 第二次 LLM 调用：开 thinking，深度回答。
	deepMessages := newagentprompt.BuildDeepAnswerMessages(conversationContext, input.UserInput)
	deepResult, err := input.Client.GenerateText(ctx, deepMessages, newagentllm.GenerateOptions{
		Temperature: 0.5,
		MaxTokens:   2000,
		Thinking:    newagentllm.ThinkingModeEnabled,
		Metadata: map[string]any{
			"stage": chatStageName,
			"phase": "deep_answer",
		},
	})

	if err != nil || deepResult == nil {
		// 深度回答失败 → 降级，只保留过渡语。
		log.Printf("[WARN] deep answer LLM failed chat=%s err=%v", flowState.ConversationID, err)
		conversationContext.AppendHistory(schema.AssistantMessage(briefSpeak, nil))
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	// 3. 输出深度回答。
	deepText := strings.TrimSpace(deepResult.Text)
	if deepText == "" {
		conversationContext.AppendHistory(schema.AssistantMessage(briefSpeak, nil))
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	if err := emitter.EmitPseudoAssistantText(
		ctx, chatSpeakBlockID, chatStageName,
		deepText,
		newagentstream.DefaultPseudoStreamOptions(),
	); err != nil {
		return fmt.Errorf("深度回答推送失败: %w", err)
	}

	// 将完整回复（过渡语 + 深度回答）写入 history。
	fullReply := briefSpeak + "\n\n" + deepText
	conversationContext.AppendHistory(schema.AssistantMessage(fullReply, nil))

	flowState.Phase = newagentmodel.PhaseChatting
	return nil
}

// handleRoutePlan 处理复杂规划：推送确认语，设 PhasePlanning。
func handleRoutePlan(
	decision *newagentmodel.ChatRoutingDecision,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
) error {
	speak := strings.TrimSpace(decision.Speak)
	if speak == "" {
		speak = "好的，让我来规划一下。"
	}

	_ = emitter.EmitStatus(chatStatusBlockID, chatStageName, "planning", speak, false)

	flowState.Phase = newagentmodel.PhasePlanning
	return nil
}

// ─── 恢复处理（保持原有逻辑不变）───

// handleChatResume 处理 pending interaction 恢复。
//
// 职责边界：
// 1. 只做状态传递：吞掉用户输入、写回历史、恢复 phase；
// 2. 不生成 speak，真正的回复由下游 Plan / Execute 节点产出；
// 3. 只推送轻量 status 通知前端"已收到回复，正在继续"。
func handleChatResume(
	input ChatNodeInput,
	runtimeState *newagentmodel.AgentRuntimeState,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
) error {
	pending := runtimeState.PendingInteraction
	flowState := runtimeState.EnsureCommonState()

	// 把用户本轮输入写回历史（ask_user 回复、confirm 附言等）。
	if strings.TrimSpace(input.UserInput) != "" {
		conversationContext.AppendHistory(schema.UserMessage(input.UserInput))
	}

	switch pending.Type {
	case newagentmodel.PendingInteractionTypeAskUser:
		// 用户回答了问题 → 恢复 phase，交给下游节点继续。
		runtimeState.ResumeFromPending()
		_ = emitter.EmitStatus(
			chatStatusBlockID, chatStageName,
			"resumed", "收到回复，继续处理。", false,
		)
		return nil

	case newagentmodel.PendingInteractionTypeConfirm:
		return handleConfirmResume(input, runtimeState, flowState, pending, emitter)

	default:
		// connection_lost 等其他类型 → 直接恢复。
		runtimeState.ResumeFromPending()
		return nil
	}
}

// handleConfirmResume 处理 confirm 类型恢复。
//
// 分支逻辑：
// 1. accept → 恢复后 phase 设为 executing，下游 Execute 节点接管；
// 2. reject + 有 PendingTool（工具确认）→ 回到 executing 让 Execute 节点换策略；
// 3. reject + 无 PendingTool（计划确认）→ 清空计划，回到 planning 重新规划。
func handleConfirmResume(
	input ChatNodeInput,
	runtimeState *newagentmodel.AgentRuntimeState,
	flowState *newagentmodel.CommonState,
	pending *newagentmodel.PendingInteraction,
	emitter *newagentstream.ChunkEmitter,
) error {
	action := strings.ToLower(strings.TrimSpace(input.ConfirmAction))

	switch action {
	case "accept":
		// 恢复前保存待执行工具，Execute 节点需要它。
		pendingTool := pending.PendingTool
		runtimeState.ResumeFromPending()
		// 将待执行工具放回临时邮箱，供 Execute 节点执行。
		if pendingTool != nil {
			copied := *pendingTool
			runtimeState.PendingConfirmTool = &copied
		}
		flowState.Phase = newagentmodel.PhaseExecuting
		_ = emitter.EmitStatus(
			chatStatusBlockID, chatStageName,
			"confirmed", "已确认，开始执行。", false,
		)

	case "reject":
		runtimeState.ResumeFromPending()
		if pending.PendingTool != nil {
			// 工具确认被拒 → 回到 executing 换策略。
			flowState.Phase = newagentmodel.PhaseExecuting
		} else {
			// 计划确认被拒 → 清空计划，回到 planning。
			flowState.RejectPlan()
		}
		_ = emitter.EmitStatus(
			chatStatusBlockID, chatStageName,
			"rejected", "已取消，准备重新规划。", false,
		)

	default:
		// 无合法 confirm action → 保守：等同于 reject。
		runtimeState.ResumeFromPending()
		if pending.PendingTool != nil {
			flowState.Phase = newagentmodel.PhaseExecuting
		} else {
			flowState.RejectPlan()
		}
	}
	return nil
}

// prepareChatNodeInput 校验并准备聊天节点的运行态依赖。
func prepareChatNodeInput(input ChatNodeInput) (
	*newagentmodel.AgentRuntimeState,
	*newagentmodel.ConversationContext,
	*newagentstream.ChunkEmitter,
	error,
) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("chat node: runtime state 不能为空")
	}
	if input.Client == nil {
		return nil, nil, nil, fmt.Errorf("chat node: chat client 未注入")
	}

	input.RuntimeState.EnsureCommonState()
	if input.ConversationContext == nil {
		input.ConversationContext = newagentmodel.NewConversationContext("")
	}
	if input.ChunkEmitter == nil {
		input.ChunkEmitter = newagentstream.NewChunkEmitter(
			newagentstream.NoopPayloadEmitter(), "", "", time.Now().Unix(),
		)
	}
	return input.RuntimeState, input.ConversationContext, input.ChunkEmitter, nil
}
