package agentexecute

import (
	"context"
	"fmt"
	agentshared "github.com/LoveLosita/smartflow/backend/services/agent/shared"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agentprompt "github.com/LoveLosita/smartflow/backend/services/agent/prompt"
	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	"github.com/LoveLosita/smartflow/backend/services/agent/tools/schedule"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
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
	RuntimeState          *agentmodel.AgentRuntimeState
	ConversationContext   *agentmodel.ConversationContext
	UserInput             string
	Client                *llmservice.Client
	ChunkEmitter          *agentstream.ChunkEmitter
	ResumeNode            string
	ToolRegistry          *agenttools.ToolRegistry
	ScheduleState         *schedule.ScheduleState
	CompactionStore       agentmodel.CompactionStore
	WriteSchedulePreview  agentmodel.WriteSchedulePreviewFunc
	OriginalScheduleState *schedule.ScheduleState
	AlwaysExecute         bool
	ThinkingEnabled       bool
	PersistVisibleMessage agentmodel.PersistVisibleMessageFunc
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

	messages := agentprompt.BuildExecuteMessages(flowState, conversationContext)
	messages = agentshared.CompactUnifiedMessagesIfNeeded(ctx, messages, agentshared.UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       executeStageName,
		StatusBlockID:   executeStatusBlockID,
	})

	agentshared.LogNodeLLMContext(executeStageName, "decision", flowState, messages)

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
