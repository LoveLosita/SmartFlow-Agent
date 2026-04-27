package newagentexecute

import (
	"context"
	"fmt"
	newagentshared "github.com/LoveLosita/smartflow/backend/newAgent/shared"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

const (
	executeStageName               = "execute"
	executeStatusBlockID           = "execute.status"
	executeSpeakBlockID            = "execute.speak"
	executePinnedKey               = "execution_context"
	toolAnalyzeHealth              = "analyze_health"
	executeHistoryKindKey          = "newagent_history_kind"
	executeHistoryKindStepAdvanced = "execute_step_advanced"

	maxConsecutiveCorrections = 3
)

type ExecuteNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	Client                *infrallm.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	ResumeNode            string
	ToolRegistry          *newagenttools.ToolRegistry
	ScheduleState         *schedule.ScheduleState
	CompactionStore       newagentmodel.CompactionStore
	WriteSchedulePreview  newagentmodel.WriteSchedulePreviewFunc
	OriginalScheduleState *schedule.ScheduleState
	AlwaysExecute         bool
	ThinkingEnabled       bool
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
}

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

func RunExecuteNode(ctx context.Context, input ExecuteNodeInput) error {
	runtimeState, conversationContext, emitter, err := prepareExecuteNodeInput(input)
	if err != nil {
		return err
	}

	flowState := runtimeState.EnsureCommonState()
	applyPendingContextHook(flowState)

	if runtimeState.PendingConfirmTool != nil {
		return executePendingTool(
			ctx,
			runtimeState,
			conversationContext,
			input.ToolRegistry,
			input.ScheduleState,
			input.OriginalScheduleState,
			input.WriteSchedulePreview,
			emitter,
		)
	}

	if input.ScheduleState != nil && flowState.RoundUsed == 0 {
		schedule.ResetTaskProcessingQueue(input.ScheduleState)
	}

	syncExecutePinnedContext(conversationContext, flowState)

	if flowState.HasCurrentPlanStep() {
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
	} else {
		if err := emitter.EmitStatus(
			executeStatusBlockID,
			executeStageName,
			"executing",
			"正在处理你的请求...",
			false,
		); err != nil {
			return fmt.Errorf("执行阶段状态推送失败: %w", err)
		}
	}

	if !flowState.NextRound() {
		flowState.Exhaust(
			executeStageName,
			"本轮执行已达到安全轮次上限，当前先停止继续操作。如需继续，我可以在你确认后接着处理剩余步骤。",
			"execute rounds exhausted before task completion",
		)
		return nil
	}

	messages := newagentprompt.BuildExecuteMessages(flowState, conversationContext)
	messages = newagentshared.CompactUnifiedMessagesIfNeeded(ctx, messages, newagentshared.UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       executeStageName,
		StatusBlockID:   executeStatusBlockID,
	})

	newagentshared.LogNodeLLMContext(executeStageName, "decision", flowState, messages)

	decisionOutput, err := collectExecuteDecisionFromLLM(
		ctx,
		input,
		flowState,
		conversationContext,
		emitter,
		messages,
	)
	if err != nil {
		return err
	}

	return handleExecuteDecision(
		ctx,
		input,
		runtimeState,
		flowState,
		conversationContext,
		emitter,
		decisionOutput,
	)
}
