package newagentnode

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/schema"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
)

const (
	interruptStageName     = "interrupt"
	interruptSpeakBlockID  = "interrupt.speak"
	interruptStatusBlockID = "interrupt.status"
)

// InterruptNodeInput 描述中断节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 不需要 LLM Client — 所有文本已在 PendingInteraction.DisplayText 里；
// 2. RuntimeState 提供 PendingInteraction；
// 3. ChunkEmitter 负责推送收尾消息。
type InterruptNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	ChunkEmitter          *newagentstream.ChunkEmitter
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
}

// RunInterruptNode 执行一轮中断节点逻辑。
//
// 核心职责：
// 1. ask_user → 把 DisplayText 当普通 assistant 消息伪流式输出，说完就停；
// 2. confirm  → 确认卡片已由 confirm 节点推送，无需额外输出；
// 3. 状态持久化已由 agent_nodes 层统一处理，Interrupt 不再需要自行存快照；
// 4. 节点结束后 graph 走 END，当前连接断开。
//
// 设计原则：
// 1. 中断就是正常对话的结束 — 助手说了问题/确认卡片，然后停下来等用户回复；
// 2. 用户下次回复时走正常 chat 入口，chat 节点负责 resume；
// 3. 不做特殊 UI，不需要前端适配新的交互模式。
func RunInterruptNode(ctx context.Context, input InterruptNodeInput) error {
	runtimeState, conversationContext, emitter, err := prepareInterruptNodeInput(input)
	if err != nil {
		return err
	}

	pending := runtimeState.PendingInteraction
	if pending == nil {
		// 无 pending interaction → 不应到达此处，防御性返回。
		return fmt.Errorf("interrupt node: 无待处理交互")
	}

	switch pending.Type {
	case newagentmodel.PendingInteractionTypeAskUser:
		return handleInterruptAskUser(ctx, runtimeState, input.PersistVisibleMessage, pending, conversationContext, emitter)
	case newagentmodel.PendingInteractionTypeConfirm:
		return handleInterruptConfirm(pending, emitter)
	default:
		// connection_lost 等其他类型 → 仅持久化，不输出。
		return handleInterruptDefault(pending, emitter)
	}
}

// handleInterruptAskUser 处理追问型中断。
//
// 把 PendingInteraction.DisplayText 当普通 assistant 消息伪流式输出，
// 写入历史，然后结束。用户体验和正常对话一样 — 助手问了问题，停下来等回复。
func handleInterruptAskUser(
	ctx context.Context,
	runtimeState *newagentmodel.AgentRuntimeState,
	persist newagentmodel.PersistVisibleMessageFunc,
	pending *newagentmodel.PendingInteraction,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
) error {
	text := pending.DisplayText
	if text == "" {
		text = "请补充更多信息。"
	}

	// 伪流式输出，和 chatReply 一样的体感。
	if err := emitter.EmitPseudoAssistantText(
		ctx, interruptSpeakBlockID, interruptStageName,
		text,
		newagentstream.DefaultPseudoStreamOptions(),
	); err != nil {
		return fmt.Errorf("追问消息推送失败: %w", err)
	}

	// 写入对话历史，下一轮 resume 时 LLM 能看到这个上下文。
	msg := schema.AssistantMessage(text, nil)
	conversationContext.AppendHistory(msg)
	persistVisibleAssistantMessage(ctx, persist, runtimeState.EnsureCommonState(), msg)

	// 状态持久化已由 agent_nodes 层统一处理，此处不再需要自行存快照。

	_ = emitter.EmitStatus(
		interruptStatusBlockID, interruptStageName,
		"ask_user", "已追问用户，等待回复。", false,
	)
	return nil
}

// handleInterruptConfirm 处理确认型中断。
//
// 确认卡片已由 confirm 节点推送，这里只需推送状态通知并持久化。
func handleInterruptConfirm(
	pending *newagentmodel.PendingInteraction,
	emitter *newagentstream.ChunkEmitter,
) error {
	// 状态持久化已由 agent_nodes 层统一处理，此处不再需要自行存快照。

	_ = emitter.EmitStatus(
		interruptStatusBlockID, interruptStageName,
		"confirm", "等待用户确认。", false,
	)
	return nil
}

// handleInterruptDefault 处理其他类型的中断（如 connection_lost）。
func handleInterruptDefault(
	pending *newagentmodel.PendingInteraction,
	emitter *newagentstream.ChunkEmitter,
) error {
	// 状态持久化已由 agent_nodes 层统一处理，此处不再需要自行存快照。

	_ = emitter.EmitStatus(
		interruptStatusBlockID, interruptStageName,
		"interrupted", "会话已中断。", false,
	)
	return nil
}

// prepareInterruptNodeInput 校验并准备中断节点的运行态依赖。
func prepareInterruptNodeInput(input InterruptNodeInput) (
	*newagentmodel.AgentRuntimeState,
	*newagentmodel.ConversationContext,
	*newagentstream.ChunkEmitter,
	error,
) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("interrupt node: runtime state 不能为空")
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
