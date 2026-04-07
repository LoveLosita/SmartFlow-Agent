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
	RuntimeState        *newagentmodel.AgentRuntimeState
	ConversationContext *newagentmodel.ConversationContext
	Client              *newagentllm.Client
	ChunkEmitter        *newagentstream.ChunkEmitter
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

	// 2. 调 LLM 生成交付总结。
	summary := generateDeliverSummary(ctx, input.Client, flowState, conversationContext)

	// 3. 伪流式推送总结。
	if strings.TrimSpace(summary) != "" {
		if err := emitter.EmitPseudoAssistantText(
			ctx,
			deliverSpeakBlockID,
			deliverStageName,
			summary,
			newagentstream.DefaultPseudoStreamOptions(),
		); err != nil {
			return fmt.Errorf("交付总结推送失败: %w", err)
		}
		conversationContext.AppendHistory(schema.AssistantMessage(summary, nil))
	}

	// 4. 推送最终完成状态。
	_ = emitter.EmitStatus(
		deliverStatusBlockID,
		deliverStageName,
		"done",
		"任务已完成。",
		true,
	)

	// 5. 标记流程结束。
	flowState.Done()
	return nil
}

// generateDeliverSummary 尝试调用 LLM 生成交付总结，失败时降级到机械格式化。
func generateDeliverSummary(
	ctx context.Context,
	client *newagentllm.Client,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
) string {
	if client == nil {
		return buildMechanicalSummary(flowState)
	}

	messages := newagentprompt.BuildDeliverMessages(flowState, conversationContext)
	result, err := client.GenerateText(
		ctx,
		messages,
		newagentllm.GenerateOptions{
			Temperature: 0.5,
			MaxTokens:   800,
			Thinking:    newagentllm.ThinkingModeDisabled,
			Metadata: map[string]any{
				"stage": deliverStageName,
			},
		},
	)
	if err != nil || result == nil || strings.TrimSpace(result.Text) == "" {
		return buildMechanicalSummary(flowState)
	}

	return normalizeSpeak(result.Text)
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

	if state.Exhausted() {
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

	if state.Exhausted() && current < total {
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
