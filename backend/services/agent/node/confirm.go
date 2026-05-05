package agentnode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
)

const (
	confirmStageName     = "confirm"
	confirmStatusBlockID = "confirm.status"
)

// ConfirmNodeInput 描述确认节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 不需要 LLM Client — 确认内容由已有状态机械格式化，不调模型；
// 2. RuntimeState 提供计划步骤和待确认工具快照；
// 3. ChunkEmitter 负责推送确认事件到前端。
type ConfirmNodeInput struct {
	RuntimeState        *agentmodel.AgentRuntimeState
	ConversationContext *agentmodel.ConversationContext
	ChunkEmitter        *agentstream.ChunkEmitter
}

// RunConfirmNode 执行一轮确认节点逻辑。
//
// 核心职责：
// 1. 判断确认来源：有 PendingConfirmTool → 工具确认；有 PlanSteps → 计划确认；
// 2. 机械格式化确认内容（不需要 LLM 调用）；
// 3. 推送确认事件 EmitConfirmRequest → 前端渲染确认卡片；
// 4. 调用 OpenConfirmInteraction 固化中断快照，Phase 自动变为 interrupted。
//
// 设计原则：
// 1. 不等待用户响应 — 等待是 interruptNode 的职责；
// 2. 不执行任何工具 — 只固化"意图"，执行留给恢复后的 Execute；
// 3. Confirm 是图里唯一负责"生成确认事件 + 固化快照"的地方，上游节点只设 Phase。
func RunConfirmNode(ctx context.Context, input ConfirmNodeInput) error {
	runtimeState, _, emitter, err := prepareConfirmNodeInput(input)
	if err != nil {
		return err
	}
	flowState := runtimeState.EnsureCommonState()

	// 优先处理工具确认（Execute 发起的写操作确认）。
	if runtimeState.PendingConfirmTool != nil {
		return handleToolConfirm(ctx, runtimeState, flowState, emitter)
	}

	// 其次处理计划确认（Plan 完成后的整体验收）。
	if flowState.HasPlan() {
		return handlePlanConfirm(ctx, runtimeState, flowState, emitter)
	}

	// 既没有工具也没有计划 → 异常状态，不应到达此处。
	return fmt.Errorf("confirm node: 没有可确认的内容（无计划、无待确认工具）")
}

// handlePlanConfirm 处理计划确认。
//
// 流程：
// 1. 从 flowState.PlanSteps 格式化可读摘要；
// 2. 推送确认事件到前端；
// 3. 调用 OpenConfirmInteraction 固化快照（无 PendingTool）。
func handlePlanConfirm(
	ctx context.Context,
	runtimeState *agentmodel.AgentRuntimeState,
	flowState *agentmodel.CommonState,
	emitter *agentstream.ChunkEmitter,
) error {
	summary := buildPlanSummary(flowState.PlanSteps)
	interactionID := generateConfirmInteractionID(flowState)

	if err := emitter.EmitConfirmRequest(
		ctx, confirmStatusBlockID, confirmStageName,
		interactionID,
		"计划确认",
		summary,
		agentstream.DefaultPseudoStreamOptions(),
	); err != nil {
		return fmt.Errorf("计划确认事件推送失败: %w", err)
	}

	runtimeState.OpenConfirmInteraction(
		interactionID,
		summary,
		"plan",
		nil,
	)

	_ = emitter.EmitStatus(
		confirmStatusBlockID, confirmStageName,
		"plan_confirm", "计划已生成，等待用户确认。", false,
	)
	return nil
}

// handleToolConfirm 处理工具确认。
//
// 流程：
// 1. 从 PendingConfirmTool 构建确认摘要；
// 2. 推送确认事件到前端；
// 3. 调用 OpenConfirmInteraction 固化快照（含 PendingTool）；
// 4. 清空 PendingConfirmTool 临时邮箱。
func handleToolConfirm(
	ctx context.Context,
	runtimeState *agentmodel.AgentRuntimeState,
	flowState *agentmodel.CommonState,
	emitter *agentstream.ChunkEmitter,
) error {
	pendingTool := runtimeState.PendingConfirmTool
	summary := buildToolConfirmSummary(pendingTool)
	interactionID := generateConfirmInteractionID(flowState)

	if err := emitter.EmitConfirmRequest(
		ctx, confirmStatusBlockID, confirmStageName,
		interactionID,
		"操作确认",
		summary,
		agentstream.DefaultPseudoStreamOptions(),
	); err != nil {
		return fmt.Errorf("工具确认事件推送失败: %w", err)
	}

	runtimeState.OpenConfirmInteraction(
		interactionID,
		summary,
		"execute",
		pendingTool,
	)

	// 确认快照已固化到 PendingInteraction，清空临时邮箱。
	runtimeState.PendingConfirmTool = nil

	_ = emitter.EmitStatus(
		confirmStatusBlockID, confirmStageName,
		"tool_confirm", "操作等待确认。", false,
	)
	return nil
}

// buildPlanSummary 把 PlanSteps 格式化成人类可读的确认摘要。
func buildPlanSummary(steps []agentmodel.PlanStep) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("共 %d 步：\n", len(steps)))
	for i, step := range steps {
		sb.WriteString(fmt.Sprintf("%d. %s", i+1, step.Content))
		if step.DoneWhen != "" {
			sb.WriteString(fmt.Sprintf("（完成条件：%s）", step.DoneWhen))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// buildToolConfirmSummary 从工具快照构建确认摘要。
func buildToolConfirmSummary(tool *agentmodel.PendingToolCallSnapshot) string {
	if tool == nil {
		return "待确认操作"
	}
	if tool.Summary != "" {
		return tool.Summary
	}
	detail := fmt.Sprintf("即将执行工具：%s", tool.ToolName)
	if tool.ArgsJSON != "" {
		var args map[string]any
		if json.Unmarshal([]byte(tool.ArgsJSON), &args) == nil && len(args) > 0 {
			detail += fmt.Sprintf("，参数：%s", tool.ArgsJSON)
		}
	}
	return detail
}

// generateConfirmInteractionID 生成确认交互的唯一标识。
func generateConfirmInteractionID(flowState *agentmodel.CommonState) string {
	prefix := flowState.TraceID
	if prefix == "" {
		prefix = "confirm"
	}
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixMilli())
}

// prepareConfirmNodeInput 校验并准备确认节点的运行态依赖。
func prepareConfirmNodeInput(input ConfirmNodeInput) (
	*agentmodel.AgentRuntimeState,
	*agentmodel.ConversationContext,
	*agentstream.ChunkEmitter,
	error,
) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("confirm node: runtime state 不能为空")
	}
	input.RuntimeState.EnsureCommonState()
	if input.ConversationContext == nil {
		input.ConversationContext = agentmodel.NewConversationContext("")
	}
	if input.ChunkEmitter == nil {
		input.ChunkEmitter = agentstream.NewChunkEmitter(
			agentstream.NoopPayloadEmitter(), "", "", time.Now().Unix(),
		)
	}
	return input.RuntimeState, input.ConversationContext, input.ChunkEmitter, nil
}
