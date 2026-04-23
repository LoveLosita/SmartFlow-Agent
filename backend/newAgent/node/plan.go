package newagentnode

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentrouter "github.com/LoveLosita/smartflow/backend/newAgent/router"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/cloudwego/eino/schema"
)

const (
	planStageName        = "plan"
	planStatusBlockID    = "plan.status"
	planSpeakBlockID     = "plan.speak"
	planSummaryBlockID   = "plan.summary"
	planPinnedKey        = "current_plan"
	planCurrentStepKey   = "current_step"
	planCurrentStepTitle = "当前步骤"
	planFullPlanTitle    = "当前完整计划"
)

// PlanNodeInput 描述单轮规划节点执行所需的最小依赖。
type PlanNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	Client                *infrallm.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	ResumeNode            string
	AlwaysExecute         bool                          // true 时计划生成后自动确认，不进入 confirm 节点
	ThinkingEnabled       bool                          // 是否开启 thinking，由 config.yaml 的 agent.thinking.plan 注入
	CompactionStore       newagentmodel.CompactionStore // 上下文压缩持久化
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
}

// RunPlanNode 执行一轮规划节点逻辑。
//
// 步骤说明：
//  1. 先校验最小依赖，并推送一条"正在规划"的状态，避免用户空等；
//  2. 构造本轮规划输入，调用 LLM Stream 接口；
//  3. 从流中提取 <SMARTFLOW_DECISION> 标签内的 JSON 决策，同时流式推送 speak 正文；
//  4. 按 action 推进流程：
//     4.1 continue：继续停留在 planning；
//     4.2 ask_user：打开 pending interaction，后续交给 interrupt 收口；
//     4.3 plan_done：固化完整计划，刷新 pinned context，并进入 waiting_confirm。
func RunPlanNode(ctx context.Context, input PlanNodeInput) error {
	runtimeState, conversationContext, emitter, err := preparePlanNodeInput(input)
	if err != nil {
		return err
	}
	flowState := runtimeState.EnsureCommonState()

	// 1. 先发一条阶段状态，让前端知道当前已经进入规划环节。
	if err := emitter.EmitStatus(
		planStatusBlockID,
		planStageName,
		"planning",
		"正在梳理目标并补全执行计划。",
		false,
	); err != nil {
		return fmt.Errorf("规划阶段状态推送失败: %w", err)
	}

	// 2. 构造本轮规划输入。
	messages := newagentprompt.BuildPlanMessages(flowState, conversationContext, input.UserInput)
	messages = compactUnifiedMessagesIfNeeded(ctx, messages, UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       planStageName,
		StatusBlockID:   planStatusBlockID,
	})
	logNodeLLMContext(planStageName, "planning", flowState, messages)

	// 3. 两阶段流式规划：从 LLM 流中先提取 <SMARTFLOW_DECISION> 决策标签，再流式推送 speak 正文。
	reader, err := input.Client.Stream(
		ctx,
		messages,
		infrallm.GenerateOptions{
			Temperature: 0.2,
			Thinking:    resolveThinkingMode(input.ThinkingEnabled),
			Metadata: map[string]any{
				"stage": planStageName,
				"phase": "planning",
			},
		},
	)
	if err != nil {
		return fmt.Errorf("规划阶段 Stream 调用失败: %w", err)
	}

	parser := newagentrouter.NewStreamDecisionParser()
	firstChunk := true

	// 3.1 阶段一：解析决策标签。
	for {
		chunk, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			log.Printf("[WARN] plan stream recv error chat=%s err=%v", flowState.ConversationID, recvErr)
			break
		}

		// thinking 内容独立推流。
		if chunk != nil && strings.TrimSpace(chunk.ReasoningContent) != "" {
			if emitErr := emitter.EmitReasoningText(planSpeakBlockID, planStageName, chunk.ReasoningContent, firstChunk); emitErr != nil {
				return fmt.Errorf("规划 thinking 推送失败: %w", emitErr)
			}
			firstChunk = false
		}

		content := ""
		if chunk != nil {
			content = chunk.Content
		}

		visible, ready, _ := parser.Feed(content)
		if !ready {
			continue
		}

		result := parser.Result()
		if result.Fallback || result.ParseFailed {
			return fmt.Errorf("规划解析失败，原始输出=%s", result.RawBuffer)
		}

		decision, parseErr := infrallm.ParseJSONObject[newagentmodel.PlanDecision](result.DecisionJSON)
		if parseErr != nil {
			return fmt.Errorf("规划决策 JSON 解析失败: %w (raw=%s)", parseErr, result.RawBuffer)
		}
		if validateErr := decision.Validate(); validateErr != nil {
			return fmt.Errorf("规划决策不合法: %w", validateErr)
		}

		// 3.2 阶段二：流式推送 speak（同一 reader 继续读取）。
		var fullText strings.Builder
		if visible != "" {
			if emitErr := emitter.EmitAssistantText(planSpeakBlockID, planStageName, visible, firstChunk); emitErr != nil {
				return fmt.Errorf("规划文案推送失败: %w", emitErr)
			}
			fullText.WriteString(visible)
			firstChunk = false
		}
		for {
			chunk2, recvErr2 := reader.Recv()
			if recvErr2 == io.EOF {
				break
			}
			if recvErr2 != nil {
				log.Printf("[WARN] plan speak stream error chat=%s err=%v", flowState.ConversationID, recvErr2)
				break
			}
			if chunk2 == nil {
				continue
			}
			if strings.TrimSpace(chunk2.ReasoningContent) != "" {
				_ = emitter.EmitReasoningText(planSpeakBlockID, planStageName, chunk2.ReasoningContent, false)
			}
			if chunk2.Content != "" {
				if emitErr := emitter.EmitAssistantText(planSpeakBlockID, planStageName, chunk2.Content, firstChunk); emitErr != nil {
					return fmt.Errorf("规划文案推送失败: %w", emitErr)
				}
				fullText.WriteString(chunk2.Content)
				firstChunk = false
			}
		}
		decision.Speak = fullText.String()

		// 4. 若有 speak 且不是 ask_user（ask_user 交给 interrupt 收口），写入历史。
		if strings.TrimSpace(decision.Speak) != "" && decision.Action != newagentmodel.PlanActionAskUser {
			msg := schema.AssistantMessage(decision.Speak, nil)
			conversationContext.AppendHistory(msg)
			persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		}

		// 5. 按规划动作推进流程状态。
		return handlePlanAction(ctx, input, runtimeState, conversationContext, emitter, flowState, decision)
	}

	// 流结束但未找到决策标签。
	return fmt.Errorf("规划阶段流结束但未提取到决策标签")
}

// handlePlanAction 根据 PlanDecision.Action 推进流程状态。
func handlePlanAction(
	ctx context.Context,
	input PlanNodeInput,
	runtimeState *newagentmodel.AgentRuntimeState,
	conversationContext *newagentmodel.ConversationContext,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
	decision *newagentmodel.PlanDecision,
) error {
	switch decision.Action {
	case newagentmodel.PlanActionContinue:
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	case newagentmodel.PlanActionAskUser:
		question := resolvePlanAskUserText(decision)
		runtimeState.OpenAskUserInteraction(uuid.NewString(), question, strings.TrimSpace(input.ResumeNode))
		return nil
	case newagentmodel.PlanActionDone:
		flowState.FinishPlan(decision.PlanSteps)
		writePlanPinnedBlocks(conversationContext, decision.PlanSteps)
		if decision.NeedsRoughBuild {
			flowState.NeedsRoughBuild = true
			if len(decision.TaskClassIDs) > 0 {
				flowState.TaskClassIDs = decision.TaskClassIDs
			}
		}
		// always_execute 开启时，计划层跳过确认闸门，直接进入执行阶段。
		if input.AlwaysExecute {
			summary := strings.TrimSpace(buildPlanSummary(decision.PlanSteps))
			if summary != "" {
				msg := schema.AssistantMessage(summary, nil)
				if err := emitter.EmitPseudoAssistantText(
					ctx,
					planSummaryBlockID,
					planStageName,
					summary,
					newagentstream.DefaultPseudoStreamOptions(),
				); err != nil {
					return fmt.Errorf("自动执行前计划摘要推送失败: %w", err)
				}
				conversationContext.AppendHistory(msg)
				persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
			}

			flowState.ConfirmPlan()
			_ = emitter.EmitStatus(
				planStatusBlockID,
				planStageName,
				"plan_auto_confirmed",
				"计划已自动确认，开始执行。",
				false,
			)
		}
		return nil
	default:
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

func preparePlanNodeInput(input PlanNodeInput) (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext, *newagentstream.ChunkEmitter, error) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("plan node: runtime state 不能为空")
	}
	if input.Client == nil {
		return nil, nil, nil, fmt.Errorf("plan node: plan client 未注入")
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

func resolvePlanAskUserText(decision *newagentmodel.PlanDecision) string {
	if decision == nil {
		return "我还缺一点关键信息，想先向你确认一下。"
	}
	if strings.TrimSpace(decision.Speak) != "" {
		return strings.TrimSpace(decision.Speak)
	}
	if strings.TrimSpace(decision.Reason) != "" {
		return strings.TrimSpace(decision.Reason)
	}
	return "我还缺一点关键信息，想先向你确认一下。"
}

func writePlanPinnedBlocks(ctx *newagentmodel.ConversationContext, steps []newagentmodel.PlanStep) {
	if ctx == nil {
		return
	}

	fullPlanText := buildPinnedPlanText(steps)
	if strings.TrimSpace(fullPlanText) != "" {
		ctx.UpsertPinnedBlock(newagentmodel.ContextBlock{
			Key:     planPinnedKey,
			Title:   planFullPlanTitle,
			Content: fullPlanText,
		})
	}

	if len(steps) == 0 {
		return
	}

	firstStep := strings.TrimSpace(steps[0].Content)
	if strings.TrimSpace(steps[0].DoneWhen) != "" {
		firstStep = fmt.Sprintf("%s\n完成判定：%s", firstStep, strings.TrimSpace(steps[0].DoneWhen))
	}
	ctx.UpsertPinnedBlock(newagentmodel.ContextBlock{
		Key:     planCurrentStepKey,
		Title:   planCurrentStepTitle,
		Content: firstStep,
	})
}

func buildPinnedPlanText(steps []newagentmodel.PlanStep) string {
	if len(steps) == 0 {
		return ""
	}

	lines := make([]string, 0, len(steps))
	for i, step := range steps {
		content := strings.TrimSpace(step.Content)
		if content == "" {
			continue
		}

		line := fmt.Sprintf("%d. %s", i+1, content)
		if strings.TrimSpace(step.DoneWhen) != "" {
			line += fmt.Sprintf("\n完成判定：%s", strings.TrimSpace(step.DoneWhen))
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

// resolveThinkingMode 根据配置布尔值返回对应的 ThinkingMode。
// 供 plan / execute / deliver 节点统一使用。
func resolveThinkingMode(enabled bool) infrallm.ThinkingMode {
	if enabled {
		return infrallm.ThinkingModeEnabled
	}
	return infrallm.ThinkingModeDisabled
}
