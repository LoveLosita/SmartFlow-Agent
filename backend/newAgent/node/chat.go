package newagentnode

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentrouter "github.com/LoveLosita/smartflow/backend/newAgent/router"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
)

const (
	chatStageName     = "chat"
	chatStatusBlockID = "chat.status"
	chatSpeakBlockID  = "chat.speak"
	// chatHistoryKindKey 用于在 history 中打运行态标记，供 prompt 层做上下文分层。
	chatHistoryKindKey = "newagent_history_kind"
	// chatHistoryKindExecuteLoopClosed 表示"上一轮 execute loop 已正常收口"。
	// prompt 侧会据此把旧 loop 归档到 msg1，而不是继续占用 msg2 窗口。
	chatHistoryKindExecuteLoopClosed = "execute_loop_closed"
)

type reorderPreference int

const (
	reorderUnknown reorderPreference = iota
	reorderAllow
	reorderDisallow
)

// ChatNodeInput 描述聊天节点单轮运行所需的最小依赖。
//
// 职责边界：
// 1. 只承载"本轮 chat"需要的输入，不负责持久化；
// 2. RuntimeState 提供 pending interaction 与流程状态；
// 3. ConversationContext 提供历史对话；
// 4. ConfirmAction 仅在 confirm 恢复场景下由前端传入 "accept" / "reject"。
type ChatNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	ConfirmAction         string
	ResumeInteractionID   string
	Client                *llmservice.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	CompactionStore       newagentmodel.CompactionStore // 上下文压缩持久化
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
}

// RunChatNode 执行一轮聊天节点逻辑。
//
// 核心职责：
// 1. 恢复判定：有 pending interaction 则处理恢复；
// 2. 路由分流：无 pending 时，调 LLM 判断复杂度并路由；
// 3. direct_reply：简单任务，直接输出回复 → END；
// 4. execute：中等任务，推 Execute ReAct；
// 5. deep_answer：复杂问答，原地开 thinking 深度回答 → END；
// 6. plan：复杂规划，推 Plan 节点。
func RunChatNode(ctx context.Context, input ChatNodeInput) error {
	runtimeState, conversationContext, emitter, err := prepareChatNodeInput(input)
	if err != nil {
		return err
	}

	// 1. 有 pending interaction → 纯状态传递，处理恢复。
	if runtimeState.HasPendingInteraction() {
		return handleChatResume(input, runtimeState, emitter)
	}

	// 2. 无 pending → 路由决策（一次快速 LLM 调用，不开 thinking）。
	flowState := runtimeState.EnsureCommonState()
	if !runtimeState.HasPendingInteraction() && flowState.Phase == newagentmodel.PhaseDone {
		terminalBefore := flowState.TerminalStatus()
		roundBefore := flowState.RoundUsed
		// 1. 只有"正常完成(completed)"才打 loop 收口标记：
		// 1.1 这样下一轮进入 execute 时，msg2 会只保留"当前活跃循环"窗口；
		// 1.2 异常收口（exhausted/aborted）不打标记，允许后续"继续"时沿用上一轮 loop 轨迹。
		if terminalBefore == newagentmodel.FlowTerminalStatusCompleted {
			appendExecuteLoopClosedMarker(conversationContext)
		}
		flowState.ResetForNextRun()
		log.Printf(
			"[DEBUG] chat reset runtime for next run chat=%s round_before=%d terminal_before=%s",
			flowState.ConversationID,
			roundBefore,
			terminalBefore,
		)
	}
	nonce := uuid.NewString()
	messages := newagentprompt.BuildChatRoutingMessages(conversationContext, input.UserInput, flowState, nonce)
	messages = compactUnifiedMessagesIfNeeded(ctx, messages, UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       chatStageName,
		StatusBlockID:   chatStatusBlockID,
	})
	logNodeLLMContext(chatStageName, "routing", flowState, messages)

	reader, err := input.Client.Stream(ctx, messages, llmservice.GenerateOptions{
		Temperature: 0.7,
		Thinking:    llmservice.ThinkingModeDisabled,
		Metadata: map[string]any{
			"stage": chatStageName,
			"phase": "routing",
		},
	})
	if err != nil {
		log.Printf("[WARN] chat routing stream failed chat=%s err=%v", flowState.ConversationID, err)
		flowState.Phase = newagentmodel.PhasePlanning
		return nil
	}

	parser := newagentrouter.NewStreamRouteParser(nonce)
	return streamAndDispatch(ctx, reader, parser, input, emitter, flowState, conversationContext)
}

// appendExecuteLoopClosedMarker 在 history 中写入"execute loop 已正常收口"标记。
//
// 职责边界：
// 1. 只负责写一个轻量 marker，供 prompt 分层；
// 2. 不负责历史裁剪，不负责消息摘要；
// 3. 若末尾已经是同类 marker，则幂等跳过，避免重复写入。
func appendExecuteLoopClosedMarker(conversationContext *newagentmodel.ConversationContext) {
	if conversationContext == nil {
		return
	}

	history := conversationContext.HistorySnapshot()
	if len(history) > 0 {
		last := history[len(history)-1]
		if isExecuteLoopClosedMarker(last) {
			return
		}
	}

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		Extra: map[string]any{
			chatHistoryKindKey: chatHistoryKindExecuteLoopClosed,
		},
	})
}

func isExecuteLoopClosedMarker(msg *schema.Message) bool {
	if msg == nil || msg.Extra == nil {
		return false
	}
	kind, ok := msg.Extra[chatHistoryKindKey].(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(kind) == chatHistoryKindExecuteLoopClosed
}

// streamAndDispatch 是流式路由分发的核心循环。
//
// 步骤说明：
// 1. 从 StreamReader 逐 chunk 读取，喂给 StreamRouteParser 增量解析控制码；
// 2. 控制码解析完成后，根据 route 进入对应的流式处理分支；
// 3. 控制码解析超时或流异常结束 → fallback 到 plan。
func streamAndDispatch(
	ctx context.Context,
	reader llmservice.StreamReader,
	parser *newagentrouter.StreamRouteParser,
	input ChatNodeInput,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
	conversationContext *newagentmodel.ConversationContext,
) error {
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			if !parser.RouteReady() {
				log.Printf("[WARN] chat stream ended before route resolved chat=%s", flowState.ConversationID)
				flowState.Phase = newagentmodel.PhasePlanning
				return nil
			}
			break
		}
		if err != nil {
			log.Printf("[WARN] chat stream recv error chat=%s err=%v", flowState.ConversationID, err)
			flowState.Phase = newagentmodel.PhasePlanning
			return nil
		}

		content := ""
		if chunk != nil {
			content = chunk.Content
		}

		visible, routeReady, _ := parser.Feed(content)
		if !routeReady {
			continue
		}

		// 控制码解析完成，进入路由分发。
		decision := parser.Decision()

		// 二次粗排硬闸门：若上下文已存在 rough_build_done 且用户未明确要求"重新粗排"，
		// 则强制关闭 needs_rough_build，避免"微调请求被误判成再次粗排"。
		if shouldDisableRoughBuildForRefine(conversationContext, input.UserInput, decision) {
			decision.NeedsRoughBuild = false
			decision.NeedsRefineAfterRoughBuild = false
		}
		// 首次粗排兜底：若用户未明确要求"只要初稿不优化"，则粗排后默认进入主动微调。
		if shouldForceRefineAfterFirstRoughBuild(conversationContext, input.UserInput, decision) {
			decision.NeedsRefineAfterRoughBuild = true
		}

		log.Printf(
			"[DEBUG] chat routing chat=%s route=%s needs_rough_build=%v needs_refine_after_rough_build=%v allow_reorder=%v thinking=%v has_rough_build_done=%v task_class_count=%d raw=%s",
			flowState.ConversationID,
			decision.Route,
			decision.NeedsRoughBuild,
			decision.NeedsRefineAfterRoughBuild,
			decision.AllowReorder,
			decision.Thinking,
			hasRoughBuildDoneMarker(conversationContext),
			len(flowState.TaskClassIDs),
			decision.Raw,
		)

		flowState.AllowReorder = resolveAllowReorder(input.UserInput, decision.AllowReorder)
		effectiveThinking := resolveEffectiveThinking(flowState.ThinkingMode, decision.Route, decision.Thinking)

		switch decision.Route {
		case newagentmodel.ChatRouteDirectReply:
			return handleDirectReplyStream(ctx, reader, input, emitter, conversationContext, flowState, effectiveThinking, visible)

		case newagentmodel.ChatRouteExecute:
			return handleRouteExecuteStream(reader, emitter, flowState, decision, input.UserInput, effectiveThinking, visible)

		case newagentmodel.ChatRouteDeepAnswer:
			return handleDeepAnswerStream(ctx, reader, input, emitter, conversationContext, flowState, effectiveThinking)

		case newagentmodel.ChatRoutePlan:
			return handleRoutePlanStream(reader, emitter, flowState, effectiveThinking, visible)

		case newagentmodel.ChatRouteQuickTask:
			// 关闭路由流，后续由 QuickTask 节点自行处理。
			_ = reader.Close()
			flowState.Phase = newagentmodel.PhaseQuickTask
			return nil

		default:
			flowState.Phase = newagentmodel.PhasePlanning
			return nil
		}
	}
	return nil
}

// resolveEffectiveThinking 根据前端 ThinkingMode 和路由决策合并出最终 thinking 状态。
//
// 规则：
// 1. "true"：前端强制开启，所有路由统一开；
// 2. "false"：前端强制关闭，所有路由统一关；
// 3. "auto"/""：按路由语义兜底；
// 3.1 deep_answer 的语义本身就是"复杂问答 + 原地深度思考"，因此默认开启；
// 3.2 execute 继续沿用路由模型给出的 decisionThinking；
// 3.3 其余路由默认关闭，避免把轻量闲聊误升成高成本推理。
func resolveEffectiveThinking(mode string, route newagentmodel.ChatRoute, decisionThinking bool) bool {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "true":
		return true
	case "false":
		return false
	default:
		if route == newagentmodel.ChatRouteDeepAnswer {
			return true
		}
		return decisionThinking
	}
}

// handleDirectReplyStream 处理闲聊回复。
//
// 两种模式：
// 1. thinking=false：同一流续传，逐 chunk 推送；
// 2. thinking=true：关闭路由流，发起第二次 thinking 流式调用。
func handleDirectReplyStream(
	ctx context.Context,
	reader llmservice.StreamReader,
	input ChatNodeInput,
	emitter *newagentstream.ChunkEmitter,
	conversationContext *newagentmodel.ConversationContext,
	flowState *newagentmodel.CommonState,
	effectiveThinking bool,
	firstVisible string,
) error {
	if effectiveThinking {
		return handleThinkingReplyStream(ctx, reader, input, emitter, conversationContext, flowState)
	}
	return handleDirectReplyContinueStream(ctx, reader, input, emitter, conversationContext, flowState, firstVisible)
}

// handleThinkingReplyStream 处理需要思考的回复：关闭路由流 → 第二次 thinking 流式调用。
func handleThinkingReplyStream(
	ctx context.Context,
	reader llmservice.StreamReader,
	input ChatNodeInput,
	emitter *newagentstream.ChunkEmitter,
	conversationContext *newagentmodel.ConversationContext,
	flowState *newagentmodel.CommonState,
) error {
	_ = reader.Close()

	deepMessages := newagentprompt.BuildDeepAnswerMessages(flowState, conversationContext, input.UserInput)
	deepMessages = compactUnifiedMessagesIfNeeded(ctx, deepMessages, UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       chatStageName,
		StatusBlockID:   chatStatusBlockID,
	})
	logNodeLLMContext(chatStageName, "direct_reply_thinking", flowState, deepMessages)
	deepReader, err := input.Client.Stream(ctx, deepMessages, llmservice.GenerateOptions{
		Temperature: 0.5,
		MaxTokens:   2000,
		Thinking:    llmservice.ThinkingModeEnabled,
		Metadata: map[string]any{
			"stage": chatStageName,
			"phase": "direct_reply_thinking",
		},
	})
	if err != nil {
		log.Printf("[WARN] thinking reply stream failed chat=%s err=%v", flowState.ConversationID, err)
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	deepText, err := emitter.EmitStreamAssistantText(ctx, deepReader, chatSpeakBlockID, chatStageName)
	_ = deepReader.Close()
	if err != nil {
		log.Printf("[WARN] thinking reply emit error chat=%s err=%v", flowState.ConversationID, err)
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	deepText = strings.TrimSpace(deepText)
	if deepText != "" {
		conversationContext.AppendHistory(schema.AssistantMessage(deepText, nil))
		persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, schema.AssistantMessage(deepText, nil))
	}

	flowState.Phase = newagentmodel.PhaseChatting
	return nil
}

// handleDirectReplyContinueStream 处理无思考的闲聊：同一流续传。
func handleDirectReplyContinueStream(
	ctx context.Context,
	reader llmservice.StreamReader,
	input ChatNodeInput,
	emitter *newagentstream.ChunkEmitter,
	conversationContext *newagentmodel.ConversationContext,
	flowState *newagentmodel.CommonState,
	firstVisible string,
) error {
	var fullText strings.Builder
	fullText.WriteString(firstVisible)

	// 推送控制码之后的第一段内容。
	if strings.TrimSpace(firstVisible) != "" {
		if err := emitter.EmitAssistantText(chatSpeakBlockID, chatStageName, firstVisible, true); err != nil {
			return fmt.Errorf("闲聊回复推送失败: %w", err)
		}
	}

	firstChunk := firstVisible == ""
	// 继续读同一个流，逐 chunk 推送。
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("[WARN] direct_reply stream error chat=%s err=%v", flowState.ConversationID, err)
			break
		}
		if chunk == nil || chunk.Content == "" {
			continue
		}
		if err := emitter.EmitAssistantText(chatSpeakBlockID, chatStageName, chunk.Content, firstChunk); err != nil {
			return fmt.Errorf("闲聊回复推送失败: %w", err)
		}
		fullText.WriteString(chunk.Content)
		firstChunk = false
	}

	text := fullText.String()
	if strings.TrimSpace(text) != "" {
		msg := schema.AssistantMessage(text, nil)
		conversationContext.AppendHistory(msg)
		persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
	}

	flowState.Phase = newagentmodel.PhaseChatting
	return nil
}

// handleRouteExecuteStream 处理工具调用路由：推送状态确认 → 设 PhaseExecuting。
//
// 说明：
// 1. 关闭路由流（后续内容不需要）；
// 2. 推送轻量状态通知；
// 3. 设置流程状态，进入 Execute 或 RoughBuild。
func handleRouteExecuteStream(
	reader llmservice.StreamReader,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
	decision *newagentmodel.ChatRoutingDecision,
	userInput string,
	effectiveThinking bool,
	speak string,
) error {
	// 关闭路由流。
	_ = reader.Close()

	if strings.TrimSpace(speak) == "" {
		speak = "好的，我来处理。"
	}

	// 推送轻量状态通知。
	_ = emitter.EmitStatus(chatStatusBlockID, chatStageName, "accepted", speak, false)

	// 清空旧 PlanSteps 并设 PhaseExecuting。
	flowState.StartDirectExecute()

	// 粗排开关逻辑。
	flowState.NeedsRoughBuild = false
	flowState.NeedsRefineAfterRoughBuild = false
	if decision.NeedsRoughBuild && len(flowState.TaskClassIDs) > 0 {
		flowState.NeedsRoughBuild = true
		flowState.NeedsRefineAfterRoughBuild = decision.NeedsRefineAfterRoughBuild
	}

	flowState.ExecuteThinking = effectiveThinking
	flowState.OptimizationMode = resolveOptimizationMode(userInput, decision, flowState)

	return nil
}

// resolveAllowReorder 统一计算"本轮是否允许打乱顺序"。
//
// 步骤化说明：
// 1. 后端先做显式语义判定：用户明确允许/明确禁止时，直接以后端判定为准；
// 2. 若后端未识别到显式语义，再回退到路由模型的 allow_reorder 字段；
// 3. 默认返回 false，确保"保持顺序"是系统默认行为。
func resolveAllowReorder(userInput string, modelAllowReorder bool) bool {
	switch detectReorderPreference(userInput) {
	case reorderAllow:
		return true
	case reorderDisallow:
		return false
	default:
		return modelAllowReorder
	}
}

// detectReorderPreference 识别用户是否"明确授权打乱顺序"。
//
// 职责边界：
// 1. 只负责关键词级别的显式意图识别，不做复杂语义推理；
// 2. 若同时命中"允许"与"禁止"，优先按"禁止"处理，避免误放开顺序约束；
// 3. 未命中显式表达时返回 unknown，交给上层兜底策略。
func detectReorderPreference(userInput string) reorderPreference {
	text := strings.ToLower(strings.TrimSpace(userInput))
	if text == "" {
		return reorderUnknown
	}

	disallowPhrases := []string{
		"不要打乱顺序",
		"不允许打乱顺序",
		"保持顺序",
		"顺序不变",
		"按原顺序",
		"不要乱序",
		"别打乱",
	}
	if containsAnyPhrase(text, disallowPhrases) {
		return reorderDisallow
	}

	allowPhrases := []string{
		"可以打乱顺序",
		"允许打乱顺序",
		"顺序不重要",
		"顺序无所谓",
		"顺序不限",
		"允许乱序",
		"可以乱序",
		"允许重排顺序",
		"reorder is fine",
		"any order",
	}
	if containsAnyPhrase(text, allowPhrases) {
		return reorderAllow
	}

	return reorderUnknown
}

// resolveOptimizationMode 统一确定当前 execute 的优化模式。
func resolveOptimizationMode(
	userInput string,
	decision *newagentmodel.ChatRoutingDecision,
	flowState *newagentmodel.CommonState,
) string {
	if decision != nil && decision.NeedsRoughBuild && flowState != nil && len(flowState.TaskClassIDs) > 0 {
		return "first_full"
	}
	if isExplicitGlobalReoptRequest(userInput) {
		return "global_reopt"
	}
	return "local_adjust"
}

// isExplicitGlobalReoptRequest 识别用户是否明确要求全局重优化。
func isExplicitGlobalReoptRequest(userInput string) bool {
	text := strings.ToLower(strings.TrimSpace(userInput))
	if text == "" {
		return false
	}
	keywords := []string{
		"全局优化",
		"整体优化",
		"全局重排",
		"整体重排",
		"重新优化全部",
		"重新优化整体",
		"全面优化",
		"整体体检",
		"全局体检",
		"重新体检",
		"global optimize",
		"global reopt",
		"overall optimize",
	}
	return containsAnyPhrase(text, keywords)
}

func containsAnyPhrase(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

// shouldDisableRoughBuildForRefine 判断是否应在 chat 路由阶段关闭"再次粗排"。
//
// 判定规则：
// 1. 当前决策未请求粗排时，直接不干预；
// 2. 上下文不存在 rough_build_done 时，不干预（首次粗排仍可走）；
// 3. 若用户未明确要求"重新粗排/从头重排"，则关闭粗排开关，避免误触发。
func shouldDisableRoughBuildForRefine(
	conversationContext *newagentmodel.ConversationContext,
	userInput string,
	decision *newagentmodel.ChatRoutingDecision,
) bool {
	if decision == nil || !decision.NeedsRoughBuild {
		return false
	}
	if !hasRoughBuildDoneMarker(conversationContext) {
		return false
	}
	return !isExplicitRoughBuildRequest(userInput)
}

// shouldForceRefineAfterFirstRoughBuild 判断是否应在"首次粗排"场景下强制开启 refine。
//
// 判定规则：
// 1. 仅在当前决策仍然请求粗排时生效；
// 2. 仅在首次粗排（上下文不存在 rough_build_done）时生效；
// 3. 若用户明确表达"只要初稿/先不优化"，则不强制开启；
// 4. 其余首次粗排场景一律开启，确保符合 PRD 的默认主动优化策略。
func shouldForceRefineAfterFirstRoughBuild(
	conversationContext *newagentmodel.ConversationContext,
	userInput string,
	decision *newagentmodel.ChatRoutingDecision,
) bool {
	if decision == nil || !decision.NeedsRoughBuild {
		return false
	}
	if hasRoughBuildDoneMarker(conversationContext) {
		return false
	}
	return !isExplicitNoRefineAfterRoughBuildRequest(userInput)
}

func hasRoughBuildDoneMarker(conversationContext *newagentmodel.ConversationContext) bool {
	if conversationContext == nil {
		return false
	}
	for _, block := range conversationContext.PinnedBlocksSnapshot() {
		if strings.TrimSpace(block.Key) == "rough_build_done" {
			return true
		}
	}
	return false
}

// isExplicitRoughBuildRequest 识别用户是否明确要求"重新粗排/从头重排"。
func isExplicitRoughBuildRequest(userInput string) bool {
	text := strings.ToLower(strings.TrimSpace(userInput))
	if text == "" {
		return false
	}
	keywords := []string{
		"重新粗排",
		"重做粗排",
		"从头排",
		"从头重排",
		"重新排一遍",
		"重新排课",
		"重排全部",
		"全部重排",
		"重置排程",
		"重置后重排",
		"重新生成初稿",
		"rebuild",
		"from scratch",
	}
	return containsAnyPhrase(text, keywords)
}

// isExplicitNoRefineAfterRoughBuildRequest 识别用户是否明确要求"粗排后先不要自动微调"。
func isExplicitNoRefineAfterRoughBuildRequest(userInput string) bool {
	text := strings.ToLower(strings.TrimSpace(userInput))
	if text == "" {
		return false
	}
	keywords := []string{
		"只要初稿",
		"先给初稿",
		"先排进去就行",
		"先排进去",
		"先不优化",
		"先别优化",
		"先不微调",
		"先别微调",
		"排完就收口",
		"粗排就行",
		"草稿就行",
		"draft only",
		"no refine",
		"no optimization",
	}
	return containsAnyPhrase(text, keywords)
}

// handleDeepAnswerStream 处理复杂问答：关闭路由流 → 第二次流式调用。
//
// 步骤说明：
// 1. 关闭第一个路由流；
// 2. 发起第二次流式 LLM 调用（thinking 由 effectiveThinking 控制）；
// 3. 真流式推送 reasoning + 正文；
// 4. 完整回复写入 history。
func handleDeepAnswerStream(
	ctx context.Context,
	reader llmservice.StreamReader,
	input ChatNodeInput,
	emitter *newagentstream.ChunkEmitter,
	conversationContext *newagentmodel.ConversationContext,
	flowState *newagentmodel.CommonState,
	effectiveThinking bool,
) error {
	// 1. 关闭第一个路由流。
	_ = reader.Close()

	// 2. 第二次流式调用。
	thinkingOpt := llmservice.ThinkingModeDisabled
	if effectiveThinking {
		thinkingOpt = llmservice.ThinkingModeEnabled
	}
	deepMessages := newagentprompt.BuildDeepAnswerMessages(flowState, conversationContext, input.UserInput)
	deepMessages = compactUnifiedMessagesIfNeeded(ctx, deepMessages, UnifiedCompactInput{
		Client:          input.Client,
		CompactionStore: input.CompactionStore,
		FlowState:       flowState,
		Emitter:         emitter,
		StageName:       chatStageName,
		StatusBlockID:   chatStatusBlockID,
	})
	logNodeLLMContext(chatStageName, "deep_answer", flowState, deepMessages)
	deepReader, err := input.Client.Stream(ctx, deepMessages, llmservice.GenerateOptions{
		Temperature: 0.5,
		MaxTokens:   2000,
		Thinking:    thinkingOpt,
		Metadata: map[string]any{
			"stage": chatStageName,
			"phase": "deep_answer",
		},
	})
	if err != nil {
		// 深度回答失败 → 降级返回。
		log.Printf("[WARN] deep answer stream failed chat=%s err=%v", flowState.ConversationID, err)
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	// 3. 真流式推送 reasoning + 正文。
	deepText, err := emitter.EmitStreamAssistantText(ctx, deepReader, chatSpeakBlockID, chatStageName)
	_ = deepReader.Close()
	if err != nil {
		log.Printf("[WARN] deep answer stream emit error chat=%s err=%v", flowState.ConversationID, err)
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	deepText = strings.TrimSpace(deepText)
	if deepText == "" {
		flowState.Phase = newagentmodel.PhaseChatting
		return nil
	}

	// 4. 完整回复写入 history。
	msg := schema.AssistantMessage(deepText, nil)
	conversationContext.AppendHistory(msg)
	persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)

	flowState.Phase = newagentmodel.PhaseChatting
	return nil
}

// handleRoutePlanStream 处理规划路由：推送状态确认 → 设 PhasePlanning。
func handleRoutePlanStream(
	reader llmservice.StreamReader,
	emitter *newagentstream.ChunkEmitter,
	flowState *newagentmodel.CommonState,
	effectiveThinking bool,
	speak string,
) error {
	// 关闭路由流。
	_ = reader.Close()

	if strings.TrimSpace(speak) == "" {
		speak = "好的，让我来规划一下。"
	}

	_ = emitter.EmitStatus(chatStatusBlockID, chatStageName, "planning", speak, false)

	flowState.Phase = newagentmodel.PhasePlanning
	return nil
}

// ─── 恢复处理（保持原有逻辑不变）───

// handleChatResume 处理 pending interaction 恢复。
//
// 职责边界：
// 1. 只做状态传递：吞掉用户输入、写回历史、恢复 phase；
// 2. 不生成 speak，真正的回复由下游 Plan / Execute 节点产出；
// 3. 只推送轻量 status 通知前端"已收到回复，正在继续"。
func handleChatResume(
	input ChatNodeInput,
	runtimeState *newagentmodel.AgentRuntimeState,
	emitter *newagentstream.ChunkEmitter,
) error {
	pending := runtimeState.PendingInteraction
	flowState := runtimeState.EnsureCommonState()

	if isMismatchedResumeInteraction(input.ResumeInteractionID, pending) {
		_ = emitter.EmitStatus(
			chatStatusBlockID, chatStageName,
			"stale_resume", "当前确认已过期，请刷新后重试。", false,
		)
		return nil
	}

	// 用户输入在 service 层进入 graph 前已经统一追加到 ConversationContext。
	// 这里不再二次写入，避免 pending 恢复路径把同一轮 user message 追加两次。

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
	if isMismatchedResumeInteraction(input.ResumeInteractionID, pending) {
		_ = emitter.EmitStatus(
			chatStatusBlockID, chatStageName,
			"stale_resume", "当前确认已过期，请刷新后重试。", false,
		)
		return nil
	}

	action := strings.ToLower(strings.TrimSpace(input.ConfirmAction))

	switch action {
	case "accept", "approve":
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

	case "reject", "cancel":
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
		_ = emitter.EmitStatus(
			chatStatusBlockID, chatStageName,
			"invalid_confirm_action", "未识别确认动作，请重试。", false,
		)
	}
	return nil
}

func isMismatchedResumeInteraction(resumeInteractionID string, pending *newagentmodel.PendingInteraction) bool {
	if pending == nil {
		return false
	}
	resumeID := strings.TrimSpace(resumeInteractionID)
	pendingID := strings.TrimSpace(pending.InteractionID)
	if resumeID == "" || pendingID == "" {
		return false
	}
	return resumeID != pendingID
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
