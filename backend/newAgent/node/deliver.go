package newagentnode

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
)

const (
	deliverStageName     = "deliver"
	deliverStatusBlockID = "deliver.status"
	deliverSpeakBlockID  = "deliver.speak"
)

// DeliverNodeInput 描述交付节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 只负责生成交付总结并推送给用户，不负责后续流程推进；
// 2. RuntimeState 提供计划步骤和执行状态；
// 3. ConversationContext 提供执行阶段的对话历史；
// 4. 交付完成后标记流程结束。
type DeliverNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	Client                *infrallm.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	ThinkingEnabled       bool                          // 是否开启 thinking，由 config.yaml 的 agent.thinking.deliver 注入
	CompactionStore       newagentmodel.CompactionStore // 上下文压缩持久化
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
}

// RunDeliverNode 执行一轮交付节点逻辑。
//
// 核心职责：
// 1. 调 LLM 基于原始计划 + 执行历史生成交付总结；
// 2. 伪流式推送总结给用户；
// 3. 写入对话历史，保证上下文连续；
// 4. 标记流程结束。
//
// 降级策略：
// 1. LLM 调用失败时，回退到机械格式化总结，不中断流程；
// 2. 机械总结包含计划步骤列表和完成进度。
func RunDeliverNode(ctx context.Context, input DeliverNodeInput) error {
	runtimeState, conversationContext, emitter, err := prepareDeliverNodeInput(input)
	if err != nil {
		return err
	}
	flowState := runtimeState.EnsureCommonState()

	// 1. 推送交付阶段状态，让前端知道正在生成总结。
	if err := emitter.EmitStatus(
		deliverStatusBlockID,
		deliverStageName,
		"summarizing",
		"正在生成交付总结。",
		false,
	); err != nil {
		return fmt.Errorf("交付阶段状态推送失败: %w", err)
	}

	// 2. 在线流式消息会把 execute / deliver 的正文追加到同一条 assistant 气泡。
	// 2.1 deliver 的 LLM 真流式路径不会经过 normalizeSpeak，因此第一段总结可能贴住上一段 execute 正文。
	// 2.2 这里先发一个仅用于 SSE 展示的段落分隔；不写入 history，避免历史回放和持久化消息额外多空行。
	// 2.3 若本轮 deliver 前没有任何正文，前端 Markdown 渲染会 trim 掉开头空行，不影响首段展示。
	if err := emitter.EmitAssistantText(deliverSpeakBlockID, deliverStageName, "\n\n", false); err != nil {
		return fmt.Errorf("交付总结段落分隔推送失败: %w", err)
	}

	// 3. 调 LLM 生成交付总结。
	summary, streamed := generateDeliverSummary(ctx, input.Client, flowState, conversationContext, input.ThinkingEnabled, input.CompactionStore, emitter)

	// 3.1 排程完毕卡片信号：
	// 1. 仅在流程正常完成且确实产生过日程变更（粗排或写工具）时推送；
	// 2. 前端收到 kind=schedule_completed 后，自行用对话 ID 调用现有接口拉取排程数据渲染卡片；
	// 3. 不携带 Redis key 或排程数据，保持信号职责单一。
	if flowState.IsCompleted() && flowState.HasScheduleChanges {
		_ = emitter.EmitScheduleCompleted(deliverStatusBlockID, deliverStageName)
	}

	// 4. 推送总结。LLM 路径已在 generateDeliverSummary 内部真流式推送，
	//    仅机械/降级路径需要在此伪流式补推。
	if strings.TrimSpace(summary) != "" {
		if !streamed {
			msg := schema.AssistantMessage(summary, nil)
			if err := emitter.EmitPseudoAssistantText(
				ctx,
				deliverSpeakBlockID,
				deliverStageName,
				summary,
				newagentstream.DefaultPseudoStreamOptions(),
			); err != nil {
				return fmt.Errorf("交付总结推送失败: %w", err)
			}
			conversationContext.AppendHistory(msg)
			persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		} else {
			msg := schema.AssistantMessage(summary, nil)
			conversationContext.AppendHistory(msg)
			persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		}
	}

	// 5. 推送最终完成状态。
	_ = emitter.EmitStatus(
		deliverStatusBlockID,
		deliverStageName,
		"done",
		"本轮流程已结束。",
		true,
	)

	return nil
}

// generateDeliverSummary 尝试调用 LLM 生成交付总结，失败时降级到机械格式化。
//
// 返回值：
//   - summary：完整总结文本（用于历史写入）；
//   - streamed：true 表示文本已通过 EmitStreamAssistantText 真流式推送到前端，调用方无需再伪流式。
func generateDeliverSummary(
	ctx context.Context,
	client *infrallm.Client,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
	thinkingEnabled bool,
	compactionStore newagentmodel.CompactionStore,
	emitter *newagentstream.ChunkEmitter,
) (string, bool) {
	if flowState != nil {
		switch {
		case flowState.IsAborted():
			return normalizeSpeak(buildAbortSummary(flowState)), false
		case flowState.IsExhaustedTerminal():
			return normalizeSpeak(buildExhaustedSummary(flowState)), false
		}
	}

	if client == nil {
		return buildMechanicalSummary(flowState), false
	}

	messages := newagentprompt.BuildDeliverMessages(flowState, conversationContext)
	messages = compactUnifiedMessagesIfNeeded(ctx, messages, UnifiedCompactInput{
		Client:          client,
		CompactionStore: compactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       deliverStageName,
		StatusBlockID:   deliverStatusBlockID,
	})
	logNodeLLMContext(deliverStageName, "summarizing", flowState, messages)

	reader, err := client.Stream(
		ctx,
		messages,
		infrallm.GenerateOptions{
			Temperature: 0.5,
			MaxTokens:   800,
			Thinking:    resolveThinkingMode(thinkingEnabled),
			Metadata: map[string]any{
				"stage": deliverStageName,
			},
		},
	)
	if err != nil {
		log.Printf("[WARN] deliver Stream 调用失败，降级到机械总结: %v", err)
		return buildMechanicalSummary(flowState), false
	}

	fullText, streamErr := emitter.EmitStreamAssistantText(ctx, reader, deliverSpeakBlockID, deliverStageName)
	if streamErr != nil || strings.TrimSpace(fullText) == "" {
		log.Printf("[WARN] deliver 流式推送失败或结果为空，降级到机械总结: streamErr=%v textLen=%d", streamErr, len(fullText))
		return buildMechanicalSummary(flowState), false
	}

	return normalizeSpeak(fullText), true
}

// buildAbortSummary 生成“流程已终止”的统一交付文案。
//
// 说明：
// 1. 第二轮开始，abort 的用户可见文案由终止方提前写入 CommonState；
// 2. deliver 不再重新猜测或改写业务异常，只做最终收口；
// 3. 若历史快照缺失 user_message，则回退到一份通用说明，避免前端收到空白结果。
func buildAbortSummary(state *newagentmodel.CommonState) string {
	if state == nil || state.TerminalOutcome == nil {
		return "本轮流程已终止。"
	}
	if msg := strings.TrimSpace(state.TerminalOutcome.UserMessage); msg != "" {
		return msg
	}
	return "本轮流程已终止，请根据当前提示检查后再继续。"
}

// buildExhaustedSummary 生成“轮次耗尽”的统一收口文案。
func buildExhaustedSummary(state *newagentmodel.CommonState) string {
	if state == nil {
		return "本轮执行已达到安全轮次上限，当前先停止继续操作。"
	}

	prefix := "本轮执行已达到安全轮次上限，当前先停止继续操作。"
	if state.TerminalOutcome != nil && strings.TrimSpace(state.TerminalOutcome.UserMessage) != "" {
		prefix = strings.TrimSpace(state.TerminalOutcome.UserMessage)
	}
	if !state.HasPlan() {
		return prefix
	}
	return prefix + "\n\n" + strings.TrimSpace(buildMechanicalSummary(state))
}

// buildMechanicalSummary 在 LLM 不可用时，机械拼接一份最小可用总结。
func buildMechanicalSummary(state *newagentmodel.CommonState) string {
	if state == nil {
		return "任务流程已结束。"
	}

	var sb strings.Builder
	current, total := state.PlanProgress()

	if !state.HasPlan() {
		return "任务流程已结束。"
	}

	if state.IsExhaustedTerminal() {
		sb.WriteString(fmt.Sprintf("任务因执行轮次耗尽提前结束，已完成 %d/%d 步。\n", current, total))
	} else {
		sb.WriteString("所有计划步骤已执行完毕。\n")
	}

	sb.WriteString("\n执行情况：\n")
	for i, step := range state.PlanSteps {
		marker := "[ ]"
		if i < current {
			marker = "[x]"
		}
		sb.WriteString(fmt.Sprintf("%s %s\n", marker, strings.TrimSpace(step.Content)))
	}

	if state.IsExhaustedTerminal() && current < total {
		sb.WriteString("\n如需继续完成剩余步骤，可以告诉我继续。")
	}

	return sb.String()
}

// prepareDeliverNodeInput 校验并准备交付节点的运行态依赖。
func prepareDeliverNodeInput(input DeliverNodeInput) (
	*newagentmodel.AgentRuntimeState,
	*newagentmodel.ConversationContext,
	*newagentstream.ChunkEmitter,
	error,
) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("deliver node: runtime state 不能为空")
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
