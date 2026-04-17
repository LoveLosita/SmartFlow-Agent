package newagentnode

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
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
//  2. 单轮深度规划：开 thinking、无 token 上限，让 LLM 一步到位产出完整计划；
//  3. 若模型先对用户说了话，则先把 speak 伪流式推给前端，并写回 history；
//  4. 最后按 action 推进流程：
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

	// 3. 单轮深度规划：由配置决定是否开启 thinking，不做 token 上限约束。
	decision, rawResult, err := infrallm.GenerateJSON[newagentmodel.PlanDecision](
		ctx,
		input.Client,
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
		if rawResult != nil && strings.TrimSpace(rawResult.Text) != "" {
			return fmt.Errorf("规划解析失败，原始输出=%s，错误=%w", strings.TrimSpace(rawResult.Text), err)
		}
		return fmt.Errorf("规划阶段模型调用失败: %w", err)
	}
	if err := decision.Validate(); err != nil {
		return fmt.Errorf("规划决策不合法: %w", err)
	}

	// 4. 若模型先对用户说了话，且不是 ask_user（ask_user 交给 interrupt 收口），则先以伪流式推送，再写回 history。
	if strings.TrimSpace(decision.Speak) != "" && decision.Action != newagentmodel.PlanActionAskUser {
		msg := schema.AssistantMessage(decision.Speak, nil)
		if err := emitter.EmitPseudoAssistantText(
			ctx,
			planSpeakBlockID,
			planStageName,
			decision.Speak,
			newagentstream.DefaultPseudoStreamOptions(),
		); err != nil {
			return fmt.Errorf("规划文案推送失败: %w", err)
		}
		conversationContext.AppendHistory(msg)
		persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
	}

	// 5. 按规划动作推进流程状态。
	switch decision.Action {
	case newagentmodel.PlanActionContinue:
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	case newagentmodel.PlanActionAskUser:
		question := resolvePlanAskUserText(decision)
		runtimeState.OpenAskUserInteraction(uuid.NewString(), question, strings.TrimSpace(input.ResumeNode))
		return nil
	case newagentmodel.PlanActionDone:
		// 4.1 直接把结构化 PlanStep 固化到 CommonState，避免 state 层丢失 done_when。
		// 4.2 再把完整自然语言计划写入 pinned context，保证后续 execute 优先看到。
		// 4.3 若 LLM 识别到批量排课意图，把 NeedsRoughBuild 标记写入 CommonState，
		//     Confirm 节点后的路由会据此决定是否跳入 RoughBuild 节点。
		// 4.4 最后进入 waiting_confirm，等待用户确认整体计划。
		flowState.FinishPlan(decision.PlanSteps)
		writePlanPinnedBlocks(conversationContext, decision.PlanSteps)
		if decision.NeedsRoughBuild {
			flowState.NeedsRoughBuild = true
			// 以 LLM 决策中的 task_class_ids 为准（若非空则覆盖前端传入值）。
			if len(decision.TaskClassIDs) > 0 {
				flowState.TaskClassIDs = decision.TaskClassIDs
			}
		}
		// always_execute 开启时，计划层跳过确认闸门，直接进入执行阶段。
		// 这样可以与 Execute 节点的"写工具跳过确认"语义保持一致。
		if input.AlwaysExecute {
			// 1. 自动执行模式不会经过 Confirm 卡片，因此这里先把完整计划明确展示给用户。
			// 2. 摘要格式复用 Confirm 节点，保证"手动确认"和"自动执行"两条链路文案一致。
			// 3. 推流后同步写入历史，确保后续 Execute 阶段的上下文也能看到这份计划。
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
