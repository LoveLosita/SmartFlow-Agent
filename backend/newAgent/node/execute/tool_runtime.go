package newagentexecute

import (
	"context"
	"encoding/json"
	"fmt"
	newagentshared "github.com/LoveLosita/smartflow/backend/newAgent/shared"
	"log"
	"regexp"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

func appendToolCallResultHistory(
	conversationContext *newagentmodel.ConversationContext,
	toolName string,
	args map[string]any,
	result newagenttools.ToolExecutionResult,
) {
	if conversationContext == nil {
		return
	}

	argsJSON := "{}"
	if args != nil {
		if raw, err := json.Marshal(args); err == nil {
			argsJSON = string(raw)
		}
	}
	toolCallID := uuid.NewString()
	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		ToolCalls: []schema.ToolCall{
			{
				ID:   toolCallID,
				Type: "function",
				Function: schema.FunctionCall{
					Name:      toolName,
					Arguments: argsJSON,
				},
			},
		},
	})
	conversationContext.AppendHistory(&schema.Message{
		Role:       schema.Tool,
		Content:    result.ObservationText,
		ToolCallID: toolCallID,
		ToolName:   toolName,
	})
}

func executeToolCall(
	ctx context.Context,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
	toolCall *newagentmodel.ToolCallIntent,
	emitter *newagentstream.ChunkEmitter,
	registry *newagenttools.ToolRegistry,
	scheduleState *schedule.ScheduleState,
	writePreview newagentmodel.WriteSchedulePreviewFunc,
) error {
	if toolCall == nil {
		return nil
	}

	toolName := strings.TrimSpace(toolCall.Name)
	if toolName == "" {
		return fmt.Errorf("工具调用缺少工具名称")
	}

	if err := emitter.EmitToolCallStart(
		executeStatusBlockID,
		executeStageName,
		toolName,
		buildToolCallStartSummary(toolName, toolCall.Arguments),
		buildToolArgumentsPreviewCN(toolCall.Arguments),
		false,
	); err != nil {
		return fmt.Errorf("工具调用开始事件发送失败: %w", err)
	}

	if registry == nil {
		return fmt.Errorf("工具注册表未注入")
	}
	if scheduleState == nil && registry.RequiresScheduleState(toolName) {
		return fmt.Errorf("日程状态未加载，无法执行工具 %q", toolName)
	}
	if registry.IsToolTemporarilyDisabled(toolName) {
		flowState.ConsecutiveCorrections++
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次调用临时禁用工具，终止执行: %s",
				flowState.ConsecutiveCorrections, toolName)
		}
		blockedText := buildTemporarilyDisabledToolResult(toolName)
		blockedResult := newagenttools.BlockedResult(toolName, toolCall.Arguments, blockedText, "tool_temporarily_disabled", blockedText)
		emitToolCallResultEvent(emitter, executeStatusBlockID, executeStageName, blockedResult, toolCall.Arguments)
		appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, blockedResult)
		newagentshared.AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("工具 %q 当前暂时禁用。", toolName),
			"请改用 move/swap/batch_move/unplace 等排程微调工具继续推进。",
		)
		return nil
	}
	if !registry.HasTool(toolName) {
		flowState.ConsecutiveCorrections++
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次调用未知工具，终止执行: %s；可用工具：%s。",
				flowState.ConsecutiveCorrections, toolName, strings.Join(registry.ToolNames(), "、"))
		}
		log.Printf("[WARN] execute 工具名不合法 chat=%s round=%d tool=%s consecutive=%d/%d available=%v",
			flowState.ConversationID, flowState.RoundUsed, toolName,
			flowState.ConsecutiveCorrections, maxConsecutiveCorrections, registry.ToolNames())
		newagentshared.AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("你调用的工具 %q 不存在。", toolName),
			fmt.Sprintf("可用工具：%s。请检查拼写后重试。", strings.Join(registry.ToolNames(), "、")),
		)
		return nil
	}
	if !isToolVisibleForCurrentExecuteMode(flowState, registry, toolName) {
		flowState.ConsecutiveCorrections++
		if flowState.ConsecutiveCorrections >= maxConsecutiveCorrections {
			return fmt.Errorf("连续 %d 次调用未激活工具，终止执行: %s（active_domain=%q active_packs=%v）",
				flowState.ConsecutiveCorrections,
				toolName,
				flowState.ActiveToolDomain,
				newagenttools.ResolveEffectiveToolPacks(flowState.ActiveToolDomain, flowState.ActiveToolPacks))
		}

		addHint := `请先调用 context_tools_add 激活目标工具域后再继续。`
		if flowState != nil && flowState.ActiveOptimizeOnly {
			addHint = `当前处于“粗排后主动优化专用模式”，只允许使用 analyze_health、move、swap；不要再尝试 query_target_tasks / query_available_slots 等全窗搜索工具。`
		} else if domain, pack, ok := newagenttools.ResolveToolDomainPack(toolName); ok {
			if newagenttools.IsFixedToolPack(domain, pack) {
				addHint = fmt.Sprintf(`请先调用 context_tools_add，参数 domain="%s"。`, domain)
			} else {
				addHint = fmt.Sprintf(`请先调用 context_tools_add，参数 domain="%s", packs=["%s"]。`, domain, pack)
			}
		}

		newagentshared.AppendLLMCorrectionWithHint(
			conversationContext,
			"",
			fmt.Sprintf("你调用的工具 %q 当前不在已激活工具域内。", toolName),
			addHint,
		)
		return nil
	}

	if shouldForceFeasibilityNegotiation(flowState, registry, toolName) {
		blockedText := buildInfeasibleBlockedResult(flowState)
		blockedResult := newagenttools.BlockedResult(toolName, toolCall.Arguments, blockedText, "health_negotiation_required", blockedText)
		emitToolCallResultEvent(emitter, executeStatusBlockID, executeStageName, blockedResult, toolCall.Arguments)
		appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, blockedResult)
		return nil
	}

	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	if !registry.RequiresScheduleState(toolName) {
		if toolCall.Arguments == nil {
			toolCall.Arguments = make(map[string]any)
		}
		toolCall.Arguments["_user_id"] = flowState.UserID
	}
	result := registry.Execute(scheduleState, toolName, toolCall.Arguments)
	result = newagenttools.EnsureToolResultDefaults(result, toolCall.Arguments)
	updateHealthSnapshotV2(flowState, toolName, result.ObservationText)
	updateTaskClassUpsertSnapshot(flowState, toolName, result.ObservationText)
	updateActiveToolDomainSnapshot(flowState, toolName, result.ObservationText)
	afterDigest := summarizeScheduleStateForDebug(scheduleState)
	log.Printf(
		"[DEBUG] execute tool chat=%s round=%d tool=%s args=%s before=%s after=%s result_preview=%.200s",
		flowState.ConversationID,
		flowState.RoundUsed,
		toolName,
		marshalArgsForDebug(toolCall.Arguments),
		beforeDigest,
		afterDigest,
		flattenForLog(result.ObservationText),
	)
	emitToolCallResultEvent(emitter, executeStatusBlockID, executeStageName, result, toolCall.Arguments)

	appendToolCallResultHistory(conversationContext, toolName, toolCall.Arguments, result)

	if registry.IsScheduleMutationTool(toolName) {
		flowState.HasScheduleWriteOps = true
		flowState.HasScheduleChanges = true
	}

	tryWritePreviewAfterWriteTool(ctx, flowState, scheduleState, registry, toolName, writePreview)

	return nil
}

func applyPendingContextHook(flowState *newagentmodel.CommonState) {
	if flowState == nil || flowState.PendingContextHook == nil {
		return
	}
	hook := flowState.PendingContextHook
	domain := newagenttools.NormalizeToolDomain(hook.Domain)
	if domain == "" {
		flowState.PendingContextHook = nil
		return
	}
	flowState.ActiveToolDomain = domain
	flowState.ActiveToolPacks = newagenttools.ResolveEffectiveToolPacks(domain, hook.Packs)
	flowState.PendingContextHook = nil
}

func isToolVisibleForCurrentExecuteMode(
	flowState *newagentmodel.CommonState,
	registry *newagenttools.ToolRegistry,
	toolName string,
) bool {
	if registry == nil {
		return false
	}
	activeDomain := ""
	var activePacks []string
	if flowState != nil {
		activeDomain = flowState.ActiveToolDomain
		activePacks = flowState.ActiveToolPacks
	}
	if !registry.IsToolVisibleInDomain(activeDomain, activePacks, toolName) {
		return false
	}
	if flowState != nil && flowState.ActiveOptimizeOnly && !newagenttools.IsToolAllowedInActiveOptimize(toolName) {
		return false
	}
	return true
}

func buildTemporarilyDisabledToolResult(toolName string) string {
	return fmt.Sprintf("工具 %q 当前暂时禁用。请改用 move/swap/batch_move/unplace 等排程微调工具。", strings.TrimSpace(toolName))
}

func executePendingTool(
	ctx context.Context,
	runtimeState *newagentmodel.AgentRuntimeState,
	conversationContext *newagentmodel.ConversationContext,
	registry *newagenttools.ToolRegistry,
	scheduleState *schedule.ScheduleState,
	originalState *schedule.ScheduleState,
	writePreview newagentmodel.WriteSchedulePreviewFunc,
	emitter *newagentstream.ChunkEmitter,
) error {
	pending := runtimeState.PendingConfirmTool
	if pending == nil {
		return nil
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(pending.ArgsJSON), &args); err != nil {
		return fmt.Errorf("解析待确认工具参数失败: %w", err)
	}

	if err := emitter.EmitToolCallStart(
		executeStatusBlockID,
		executeStageName,
		pending.ToolName,
		buildToolCallStartSummary(pending.ToolName, args),
		buildToolArgumentsPreviewCN(args),
		false,
	); err != nil {
		return fmt.Errorf("工具调用开始事件发送失败: %w", err)
	}

	if scheduleState == nil {
		return fmt.Errorf("日程状态未加载，无法执行已确认的写工具 %s", pending.ToolName)
	}
	flowState := runtimeState.EnsureCommonState()
	if registry.IsToolTemporarilyDisabled(pending.ToolName) {
		blockedText := buildTemporarilyDisabledToolResult(pending.ToolName)
		blockedResult := newagenttools.BlockedResult(pending.ToolName, args, blockedText, "tool_temporarily_disabled", blockedText)
		emitToolCallResultEvent(emitter, executeStatusBlockID, executeStageName, blockedResult, args)
		appendToolCallResultHistory(conversationContext, pending.ToolName, args, blockedResult)
		runtimeState.PendingConfirmTool = nil
		return nil
	}

	if shouldForceFeasibilityNegotiation(flowState, registry, pending.ToolName) {
		blockedText := buildInfeasibleBlockedResult(flowState)
		blockedResult := newagenttools.BlockedResult(pending.ToolName, args, blockedText, "health_negotiation_required", blockedText)
		emitToolCallResultEvent(emitter, executeStatusBlockID, executeStageName, blockedResult, args)
		appendToolCallResultHistory(conversationContext, pending.ToolName, args, blockedResult)
		runtimeState.PendingConfirmTool = nil
		return nil
	}

	beforeDigest := summarizeScheduleStateForDebug(scheduleState)
	if !registry.RequiresScheduleState(pending.ToolName) {
		if args == nil {
			args = make(map[string]any)
		}
		args["_user_id"] = flowState.UserID
	}
	result := registry.Execute(scheduleState, pending.ToolName, args)
	result = newagenttools.EnsureToolResultDefaults(result, args)
	updateHealthSnapshotV2(flowState, pending.ToolName, result.ObservationText)
	updateTaskClassUpsertSnapshot(flowState, pending.ToolName, result.ObservationText)
	updateActiveToolDomainSnapshot(flowState, pending.ToolName, result.ObservationText)
	afterDigest := summarizeScheduleStateForDebug(scheduleState)
	log.Printf(
		"[DEBUG] execute pending tool chat=%s round=%d tool=%s args=%s before=%s after=%s result_preview=%.200s",
		flowState.ConversationID,
		flowState.RoundUsed,
		pending.ToolName,
		marshalArgsForDebug(args),
		beforeDigest,
		afterDigest,
		flattenForLog(result.ObservationText),
	)
	emitToolCallResultEvent(emitter, executeStatusBlockID, executeStageName, result, args)

	appendToolCallResultHistory(conversationContext, pending.ToolName, args, result)

	if registry.IsScheduleMutationTool(pending.ToolName) {
		flowState.HasScheduleWriteOps = true
		flowState.HasScheduleChanges = true
	}

	tryWritePreviewAfterWriteTool(ctx, flowState, scheduleState, registry, pending.ToolName, writePreview)

	runtimeState.PendingConfirmTool = nil

	return nil
}

func tryWritePreviewAfterWriteTool(
	ctx context.Context,
	flowState *newagentmodel.CommonState,
	scheduleState *schedule.ScheduleState,
	registry *newagenttools.ToolRegistry,
	toolName string,
	writePreview newagentmodel.WriteSchedulePreviewFunc,
) {
	if flowState == nil || scheduleState == nil || registry == nil || writePreview == nil {
		return
	}
	if !registry.IsScheduleMutationTool(toolName) {
		return
	}

	if err := writePreview(ctx, scheduleState, flowState.UserID, flowState.ConversationID, flowState.TaskClassIDs); err != nil {
		log.Printf(
			"[WARN] execute realtime preview write failed chat=%s tool=%s err=%v",
			flowState.ConversationID,
			toolName,
			err,
		)
		return
	}

	log.Printf(
		"[DEBUG] execute realtime preview write success chat=%s tool=%s",
		flowState.ConversationID,
		toolName,
	)
}

var listItemRe = regexp.MustCompile(`([^\n])([2-9][\.、]\s)`)

func normalizeSpeak(speak string) string {
	speak = strings.TrimSpace(speak)
	if speak == "" {
		return speak
	}
	if !strings.Contains(speak, "\n") {
		speak = listItemRe.ReplaceAllString(speak, "$1\n$2")
	}
	return speak + "\n"
}

func buildExecuteNormalizedSpeakTail(streamed, normalized string) string {
	streamed = strings.ReplaceAll(streamed, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r\n", "\n")
	if streamed == "" || normalized == "" {
		return ""
	}
	if !strings.HasPrefix(normalized, streamed) {
		return ""
	}
	return normalized[len(streamed):]
}

func emitToolCallResultEvent(
	emitter *newagentstream.ChunkEmitter,
	blockID string,
	stage string,
	result newagenttools.ToolExecutionResult,
	args map[string]any,
) {
	if emitter == nil {
		return
	}
	result = newagenttools.EnsureToolResultDefaults(result, args)
	_ = emitter.EmitToolCallResult(
		blockID,
		stage,
		result.Tool,
		result.Status,
		result.Summary,
		result.ArgumentsPreview,
		newagenttools.ToolArgumentViewToMap(result.ArgumentView),
		newagenttools.ToolDisplayViewToMap(result.ResultView),
		false,
	)
}
