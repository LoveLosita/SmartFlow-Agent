package newagentnode

import (
	"context"
	"fmt"
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

// chatIntentDecision 是意图分类的结构化输出。
type chatIntentDecision struct {
	Intent string `json:"intent"`
	Reply  string `json:"reply,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Normalize 清洗意图分类结果中的字符串字段。
func (d *chatIntentDecision) Normalize() {
	if d == nil {
		return
	}
	d.Intent = strings.TrimSpace(d.Intent)
	d.Reply = strings.TrimSpace(d.Reply)
	d.Reason = strings.TrimSpace(d.Reason)
}

// Validate 校验意图分类结果的最小合法性。
func (d *chatIntentDecision) Validate() error {
	if d == nil {
		return fmt.Errorf("chat intent decision 不能为空")
	}
	d.Normalize()
	switch d.Intent {
	case "chat", "task":
		return nil
	default:
		return fmt.Errorf("未知 intent: %s", d.Intent)
	}
}

// RunChatNode 执行一轮聊天节点逻辑。
//
// 核心职责：
// 1. 恢复判定：有 pending interaction 则处理恢复，不生成 speak；
// 2. 意图分流：无 pending 时，调 LLM 分类 chat / task；
// 3. 闲聊回复：纯 chat 场景直接生成回复并流式推送，phase → chatting → END；
// 4. 任务路由：task 场景 phase → planning，交给后续 Plan 节点处理。
//
// 保守原则：分类失败或意图不明时，一律走 task，不丢失用户意图。
func RunChatNode(ctx context.Context, input ChatNodeInput) error {
	runtimeState, conversationContext, emitter, err := prepareChatNodeInput(input)
	if err != nil {
		return err
	}

	// 1. 有 pending interaction → 纯状态传递，不生成 speak。
	if runtimeState.HasPendingInteraction() {
		return handleChatResume(input, runtimeState, conversationContext, emitter)
	}

	// 2. 无 pending → 调 LLM 做意图分类。
	messages := newagentprompt.BuildChatIntentMessages(conversationContext, input.UserInput)
	decision, _, err := newagentllm.GenerateJSON[chatIntentDecision](
		ctx,
		input.Client,
		messages,
		newagentllm.GenerateOptions{
			Temperature: 0.1,
			MaxTokens:   300,
			Thinking:    newagentllm.ThinkingModeDisabled,
		},
	)
	if err != nil || decision.Validate() != nil {
		// 分类失败 → 保守：走 task。
		runtimeState.EnsureCommonState().Phase = newagentmodel.PhasePlanning
		return nil
	}

	// 3. 按意图分流。
	flowState := runtimeState.EnsureCommonState()
	switch decision.Intent {
	case "task":
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	case "chat":
		return handleChatReply(ctx, decision, conversationContext, emitter, flowState)
	default:
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	}
}

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

// handleChatReply 处理纯闲聊意图 — 把分类时产出的 reply 流式推给前端。
func handleChatReply(
	ctx context.Context,
	decision *chatIntentDecision,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
) error {
	reply := strings.TrimSpace(decision.Reply)

	if reply != "" {
		if err := emitter.EmitPseudoAssistantText(
			ctx, chatSpeakBlockID, chatStageName,
			reply,
			newagentstream.DefaultPseudoStreamOptions(),
		); err != nil {
			return fmt.Errorf("闲聊回复推送失败: %w", err)
		}
		conversationContext.AppendHistory(schema.AssistantMessage(reply, nil))
	}

	flowState.Phase = newagentmodel.PhaseChatting
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
