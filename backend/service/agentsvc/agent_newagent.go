package agentsvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	newagentconv "github.com/LoveLosita/smartflow/backend/newAgent/conv"
	newagentgraph "github.com/LoveLosita/smartflow/backend/newAgent/graph"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	schedule "github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
	"github.com/cloudwego/eino/schema"
	"github.com/spf13/viper"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/respond"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
)

const (
	newAgentHistoryKindKey        = "newagent_history_kind"
	newAgentHistoryKindLoopClosed = "execute_loop_closed"
)

// runNewAgentGraph 运行 newAgent 通用 graph，直接替换旧 agent 路由逻辑。
//
// 职责边界：
// 1. 负责构造 AgentGraphRunInput（RuntimeState、ConversationContext、Request、Deps）；
// 2. 负责将 outChan 适配为 ChunkEmitter；
// 3. 负责调用 graph.RunAgentGraph；
// 4. 负责持久化聊天历史（复用现有逻辑）。
//
// 设计原则：
// 1. 直接走 newAgent graph，不再经过旧的 agentrouter 路由决策；
// 2. 所有任务类型（chat、task、quick_note）都由 graph 内部 LLM 决策；
// 3. 状态恢复、工具执行、确认流程全部由 graph 节点处理。
func (s *AgentService) runNewAgentGraph(
	ctx context.Context,
	userMessage string,
	thinkingMode string,
	modelName string,
	userID int,
	chatID string,
	extra map[string]any,
	traceID string,
	requestStart time.Time,
	outChan chan<- string,
	errChan chan error,
) {
	requestCtx, _ := withRequestTokenMeter(ctx)
	if s == nil || s.llmService == nil {
		// 0. newAgent 主链强依赖 llm-service；装配漏传时直接返回错误，避免 nil receiver panic。
		pushErrNonBlocking(errChan, errors.New("agent llm service is not initialized"))
		return
	}

	// 1. 规范会话 ID 和模型选择。
	chatID = normalizeConversationID(chatID)
	_, resolvedModelName := s.pickChatModel(modelName)

	// 2. 确保会话存在（优先缓存，必要时回源 DB）。
	result, err := s.agentCache.GetConversationStatus(requestCtx, chatID)
	if err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}
	if !result {
		innerResult, ifErr := s.repo.IfChatExists(requestCtx, userID, chatID)
		if ifErr != nil {
			pushErrNonBlocking(errChan, ifErr)
			return
		}
		if !innerResult {
			if _, err = s.repo.CreateNewChat(userID, chatID); err != nil {
				pushErrNonBlocking(errChan, err)
				return
			}
		}
		if err = s.agentCache.SetConversationStatus(requestCtx, chatID); err != nil {
			log.Printf("设置会话状态缓存失败 chat=%s: %v", chatID, err)
		}
	}

	// 3. retry 机制已下线，不再构建重试元数据。

	// 4. 如果当前 conversation 被主动调度 session 占管，先走 session 分支，不进入普通 newAgent。
	//    这样 waiting_user_reply / rerunning 期间，用户消息会先推动主动调度闭环，而不是误进自由聊天。
	if handled, sessionErr := s.handleActiveScheduleSessionChat(requestCtx, userMessage, traceID, requestStart, userID, chatID, resolvedModelName, outChan, errChan); sessionErr != nil {
		pushErrNonBlocking(errChan, sessionErr)
		return
	} else if handled {
		return
	}

	// 5. 从 StateStore 加载或创建 RuntimeState。
	//    恢复场景（confirm/ask_user）同时拿到快照中保存的 ConversationContext，
	//    其中包含工具调用/结果等中间消息，保证后续 LLM 调用的消息链完整。
	runtimeState, savedConversationContext, savedScheduleState, savedOriginalScheduleState := s.loadOrCreateRuntimeState(requestCtx, chatID, userID)

	// 6. 构造 ConversationContext。
	//    优先使用快照中恢复的 ConversationContext（含工具调用/结果），
	//    无快照时从 Redis LLM 历史缓存加载。
	var conversationContext *newagentmodel.ConversationContext
	if savedConversationContext != nil {
		conversationContext = savedConversationContext
		// 把用户本轮输入追加到恢复的上下文中（与 loadConversationContext 行为一致）。
		if strings.TrimSpace(userMessage) != "" {
			conversationContext.AppendHistory(schema.UserMessage(userMessage))
		}
	} else {
		conversationContext = s.loadConversationContext(requestCtx, chatID, userMessage)
	}
	// 6.1. 在 graph 执行前统一补充与当前输入相关的记忆上下文（预取管线模式）。
	// 6.1.1 先读 Redis 预取缓存注入到 ConversationContext，再启动后台 goroutine 做完整检索；
	// 6.1.2 返回的 channel 传入 Deps，供 Execute/Plan 节点在启动前消费最新记忆；
	// 6.1.3 检索失败只降级为"本轮不注入记忆"，不阻断主链路。
	memoryFuture := s.injectMemoryContext(requestCtx, conversationContext, userID, chatID, userMessage)

	// 6.5 将前端传入的 thinkingMode 写入 CommonState，供 ChatNode 及下游节点读取。
	cs := runtimeState.EnsureCommonState()
	cs.ThinkingMode = thinkingMode

	// 6.6 若 extra 携带 task_class_ids，校验后写入 CommonState（仅首轮/尚未设置时生效，跨轮持久化）。
	if taskClassIDs := readAgentExtraIntSlice(extra, "task_class_ids"); len(taskClassIDs) > 0 {
		cs := runtimeState.EnsureCommonState()
		if len(cs.TaskClassIDs) == 0 {
			if s.scheduleProvider == nil {
				pushErrNonBlocking(errChan, respond.WrongTaskClassID)
				return
			}
			metas, metaErr := s.scheduleProvider.LoadTaskClassMetas(requestCtx, userID, taskClassIDs)
			if metaErr != nil {
				pushErrNonBlocking(errChan, respond.WrongTaskClassID)
				return
			}
			cs.TaskClassIDs = taskClassIDs
			cs.TaskClasses = metas
		}
	}

	cs = runtimeState.EnsureCommonState()

	// 6.7 先把本轮用户输入落库，确保后续可见 assistant 消息按真实时间线追加。
	userMsg := schema.UserMessage(userMessage)
	if err := s.persistNewAgentConversationMessage(requestCtx, userID, chatID, userMsg, 0); err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}

	persistVisibleMessage := func(persistCtx context.Context, state *newagentmodel.CommonState, msg *schema.Message) error {
		targetState := state
		if targetState == nil {
			targetState = runtimeState.EnsureCommonState()
		}
		if targetState != nil {
			if targetState.UserID <= 0 {
				targetState.UserID = userID
			}
			if strings.TrimSpace(targetState.ConversationID) == "" {
				targetState.ConversationID = chatID
			}
		}
		return s.persistNewAgentConversationMessage(persistCtx, userID, chatID, msg, 0)
	}

	// 7. 构造 AgentGraphRequest。
	var (
		confirmAction       string
		resumeInteractionID string
	)
	if len(extra) > 0 {
		confirmAction = readAgentExtraString(extra, "confirm_action")
		resumeInteractionID = readAgentExtraString(extra, "resume_interaction_id")
	}
	graphRequest := newagentmodel.AgentGraphRequest{
		UserInput:           userMessage,
		ConfirmAction:       confirmAction,
		ResumeInteractionID: resumeInteractionID,
		AlwaysExecute:       readAgentExtraBool(extra, "always_execute"),
	}
	graphRequest.Normalize()

	// 8. 适配 LLM clients（统一从 llm-service 取出 newAgent 图所需模型，不再直接碰 AIHub）。
	// 8.1 Chat/Deliver 使用 Pro 模型：路由分流、闲聊、交付总结属于标准复杂度。
	// 8.2 Plan/Execute 使用 Max 模型：规划和 ReAct 循环需要深度推理能力。
	llmClients := s.llmService.NewAgentModelClients()
	chatClient := llmClients.Chat
	planClient := llmClients.Plan
	executeClient := llmClients.Execute
	deliverClient := llmClients.Deliver
	summaryClient := llmClients.Summary

	// 9. 适配 SSE emitter。
	sseEmitter := newagentstream.NewSSEPayloadEmitter(outChan)
	chunkEmitter := newagentstream.NewChunkEmitter(sseEmitter, traceID, resolvedModelName, requestStart.Unix())
	chunkEmitter.SetReasoningSummaryFunc(s.makeReasoningSummaryFunc(summaryClient))
	// 关键卡片事件走统一时间线持久化，保证刷新后可重建。
	chunkEmitter.SetExtraEventHook(func(extra *newagentstream.OpenAIChunkExtra) {
		s.persistNewAgentTimelineExtraEvent(context.Background(), userID, chatID, extra)
	})

	// 10. 构造 AgentGraphDeps（由 cmd/start.go 注入的依赖）。
	deps := newagentmodel.AgentGraphDeps{
		ChatClient:            chatClient,
		PlanClient:            planClient,
		ExecuteClient:         executeClient,
		DeliverClient:         deliverClient,
		ChunkEmitter:          chunkEmitter,
		StateStore:            s.agentStateStore,
		ToolRegistry:          s.toolRegistry,
		ScheduleProvider:      s.scheduleProvider,
		CompactionStore:       s.compactionStore,
		RoughBuildFunc:        s.makeRoughBuildFunc(),
		WriteSchedulePreview:  s.makeWriteSchedulePreviewFunc(),
		MemoryFuture:          memoryFuture,
		ThinkingPlan:          viper.GetBool("agent.thinking.plan"),
		ThinkingExecute:       viper.GetBool("agent.thinking.execute"),
		ThinkingDeliver:       viper.GetBool("agent.thinking.deliver"),
		PersistVisibleMessage: persistVisibleMessage,
		QuickTaskDeps:         s.quickTaskDeps,
	}

	// 11. 构造 AgentGraphRunInput 并运行 graph。
	runInput := newagentmodel.AgentGraphRunInput{
		RuntimeState:          runtimeState,
		ConversationContext:   conversationContext,
		ScheduleState:         savedScheduleState,
		OriginalScheduleState: savedOriginalScheduleState,
		Request:               graphRequest,
		Deps:                  deps,
	}

	finalState, graphErr := newagentgraph.RunAgentGraph(requestCtx, runInput)
	if graphErr != nil {
		// 1. 客户端断连导致的 context 取消，属于正常场景，不推错误通道也不跑 fallback。
		//    否则会刷 "错误通道已满" 日志噪音，且 fallback 在 ctx 已取消时也会失败。
		if errors.Is(graphErr, context.Canceled) || requestCtx.Err() != nil {
			log.Printf("[WARN] newAgent graph 因客户端断连中止 trace=%s chat=%s", traceID, chatID)
			return
		}
		log.Printf("[ERROR] newAgent graph 执行失败 trace=%s chat=%s: %v", traceID, chatID, graphErr)
		pushErrNonBlocking(errChan, fmt.Errorf("graph 执行失败: %w", graphErr))

		// Graph 出错时回退普通聊天，保证可用性。回退使用 llm-service 的 Pro 模型。
		s.runNormalChatFlow(requestCtx, chatClient, resolvedModelName, userMessage, true, "", nil, thinkingModeToBool(thinkingMode), userID, chatID, traceID, requestStart, outChan, errChan)
		return
	}

	// 12. 持久化聊天历史（用户消息 + 助手回复）。
	requestTotalTokens := snapshotRequestTokenMeter(requestCtx).TotalTokens
	s.adjustNewAgentRequestTokenUsage(requestCtx, userID, chatID, requestTotalTokens)
	// 12.5. 将最终状态快照异步写入 MySQL（通过 outbox）。
	// Deliver 节点已将快照保存到 Redis（2h TTL），此处通过 outbox 异步写入 MySQL 做永久存储。
	if finalState != nil {
		snapshot := &newagentmodel.AgentStateSnapshot{
			RuntimeState:        finalState.EnsureRuntimeState(),
			ConversationContext: finalState.EnsureConversationContext(),
		}
		eventsvc.PublishAgentStateSnapshot(requestCtx, s.eventPublisher, snapshot, chatID, userID)
	}

	// 12.6. graph 完成后条件触发记忆抽取。
	// 说明：
	// 1. 只有本轮未走快捷随口记任务路径时才触发记忆抽取；
	// 2. 避免随口记创建的 Task 与记忆系统产生语义冲突。
	if finalState != nil {
		cs := finalState.EnsureRuntimeState().EnsureCommonState()
		if cs == nil || !cs.UsedQuickNote {
			if memErr := eventsvc.PublishMemoryExtractFromGraph(requestCtx, s.eventPublisher, userID, chatID, userMessage); memErr != nil {
				log.Printf("[WARN] graph 完成后发布记忆抽取事件失败 trace=%s chat=%s err=%v", traceID, chatID, memErr)
			}
		}
	}

	// 排程预览缓存由 Deliver 节点负责写入（通过注入的 WriteSchedulePreview func），
	// 保证只有任务真正完成时才写，中断路径不写中间态。

	// 13. 发送 OpenAI 兼容的流式结束标记，告知客户端 stream 已完成。
	_ = chunkEmitter.EmitDone()

	// 14. 异步生成会话标题。
	s.ensureConversationTitleAsync(userID, chatID)
}

// loadOrCreateRuntimeState 从 StateStore 加载或创建新的 RuntimeState。
//
// 返回值：
//   - RuntimeState：可持久化流程状态；
//   - ConversationContext：快照中保存的完整对话上下文（含工具调用/结果），
//     仅在恢复已有快照时非 nil，新建会话时为 nil。
//
// 设计说明：
//  1. 快照中的 ConversationContext 包含 graph 执行期间的完整中间消息（工具调用、工具结果等），
//     这些消息不会出现在 Redis LLM 历史缓存中；
//  2. 恢复场景（confirm/ask_user）必须使用快照中的 ConversationContext，否则工具结果丢失，
//     导致后续 LLM 调用收到非法的裸 Tool 消息，API 拒绝请求、连接断开。
func (s *AgentService) loadOrCreateRuntimeState(ctx context.Context, chatID string, userID int) (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext, *schedule.ScheduleState, *schedule.ScheduleState) {
	newRT := func() (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext, *schedule.ScheduleState, *schedule.ScheduleState) {
		rt := newagentmodel.NewAgentRuntimeState(nil)
		cs := rt.EnsureCommonState()
		cs.UserID = userID
		cs.ConversationID = chatID // saveAgentState 依赖此字段决定是否持久化
		return rt, nil, nil, nil
	}

	if s.agentStateStore == nil {
		return newRT()
	}

	snapshot, ok, err := s.agentStateStore.Load(ctx, chatID)
	log.Printf("[DEBUG] loadOrCreateRuntimeState chatID=%s ok=%v err=%v hasRuntime=%v hasPending=%v hasCtx=%v hasSchedule=%v hasOriginal=%v",
		chatID, ok, err,
		snapshot != nil && snapshot.RuntimeState != nil,
		snapshot != nil && snapshot.RuntimeState != nil && snapshot.RuntimeState.HasPendingInteraction(),
		snapshot != nil && snapshot.ConversationContext != nil,
		snapshot != nil && snapshot.ScheduleState != nil,
		snapshot != nil && snapshot.OriginalScheduleState != nil,
	)
	if err != nil {
		log.Printf("加载 agent 状态失败 chat=%s: %v", chatID, err)
		return newRT()
	}
	if ok && snapshot != nil && snapshot.RuntimeState != nil {
		// 恢复运行态，确保身份信息与当前请求一致。
		cs := snapshot.RuntimeState.EnsureCommonState()
		cs.UserID = userID
		cs.ConversationID = chatID

		// 1. 冷加载兜底：若上一轮已经收口且当前没有待恢复交互，说明本次是新一轮请求；
		// 2. 这里先重置执行期临时字段，避免旧 round/terminal 状态污染 chat 路由和后续 execute；
		// 3. 即使 chat 节点也有同条件重置，这里仍保留兜底，覆盖断线恢复或入口绕行场景。
		if !snapshot.RuntimeState.HasPendingInteraction() && cs.Phase == newagentmodel.PhaseDone {
			terminalBefore := cs.TerminalStatus()
			roundBefore := cs.RoundUsed
			// 1. 仅"正常完成(completed)"写 loop 收口 marker：
			// 1.1 下一轮执行时，prompt 会把上一轮 loop 从 msg2 归档到 msg1；
			// 1.2 异常中断（aborted/exhausted）不写 marker，保留 msg2 便于后续续跑。
			if terminalBefore == newagentmodel.FlowTerminalStatusCompleted {
				appendExecuteLoopClosedMarker(snapshot.ConversationContext)
			}
			cs.ResetForNextRun()
			log.Printf(
				"[DEBUG] loadOrCreateRuntimeState reset runtime for next run chat=%s round_before=%d terminal_before=%s",
				chatID,
				roundBefore,
				terminalBefore,
			)
		}

		// 常规场景仍由 Chat 节点基于路由覆盖 Phase，这里只在"上一轮已 done"时做一次前置清理兜底。
		// 其余跨轮可复用状态（如任务类范围、会话历史、日程内存态）继续保留，支持连续对话调整日程。

		originalScheduleState := snapshot.OriginalScheduleState
		if snapshot.ScheduleState != nil && originalScheduleState == nil {
			// 1. 兼容老快照：历史会话可能只存了 ScheduleState，没有 original 副本。
			// 2. 这里补一份克隆，保证后续节点拿到的仍是"恢复态 + 原始态"成对数据。
			// 3. 即便当前阶段不落库，这里也保留一致性，避免下一轮再出现语义漂移。
			originalScheduleState = snapshot.ScheduleState.Clone()
		}
		return snapshot.RuntimeState, snapshot.ConversationContext, snapshot.ScheduleState, originalScheduleState
	}
	return newRT()
}

// appendExecuteLoopClosedMarker 在 ConversationContext 写入"上一轮 loop 正常收口"标记。
//
// 职责边界：
// 1. 只追加轻量 marker 供 prompt 分层，不做历史摘要或裁剪；
// 2. 若末尾已是同类 marker，则幂等跳过；
// 3. context 为空时直接返回，避免冷启动异常。
func appendExecuteLoopClosedMarker(conversationContext *newagentmodel.ConversationContext) {
	if conversationContext == nil {
		return
	}
	history := conversationContext.HistorySnapshot()
	if len(history) > 0 {
		last := history[len(history)-1]
		if last != nil && last.Extra != nil {
			if kind, ok := last.Extra[newAgentHistoryKindKey].(string); ok && strings.TrimSpace(kind) == newAgentHistoryKindLoopClosed {
				return
			}
		}
	}

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		Extra: map[string]any{
			newAgentHistoryKindKey: newAgentHistoryKindLoopClosed,
		},
	})
}

// loadConversationContext 加载对话历史，构造 ConversationContext。
func (s *AgentService) loadConversationContext(ctx context.Context, chatID, userMessage string) *newagentmodel.ConversationContext {
	// 从 Redis 加载历史。
	history, err := s.agentCache.GetHistory(ctx, chatID)
	if err != nil {
		log.Printf("加载历史失败 chat=%s: %v", chatID, err)
		history = nil
	}

	// 缓存未命中时回源 DB。
	if history == nil {
		histories, hisErr := s.repo.GetUserChatHistories(ctx, 0, pkg.HistoryFetchLimitByModel("worker"), chatID)
		if hisErr != nil {
			log.Printf("从 DB 加载历史失败 chat=%s: %v", chatID, hisErr)
		} else {
			history = conv.ToEinoMessages(histories)
			// 回填到 Redis。
			if backfillErr := s.agentCache.BackfillHistory(ctx, chatID, history); backfillErr != nil {
				log.Printf("回填历史到 Redis 失败 chat=%s: %v", chatID, backfillErr)
			}
		}
	}

	// 构造 ConversationContext。
	conversationContext := newagentmodel.NewConversationContext(newagentprompt.SystemPrompt)
	if history != nil {
		conversationContext.ReplaceHistory(history)
	}

	// 把用户本轮输入追加到历史（供 graph 使用）。
	if strings.TrimSpace(userMessage) != "" {
		conversationContext.AppendHistory(schema.UserMessage(userMessage))
	}

	return conversationContext
}

// persistNewAgentConversationMessage 负责把 newAgent 链路里"真正对用户可见"的消息统一落到 Redis + MySQL。
//
// 职责边界：
// 1. 只做单条消息的持久化，不做 graph 流程控制；
// 2. TokensConsumed 由调用方显式传入，newAgent 逐条可见消息默认写 0；
// 3. Redis 失败只记日志，DB 失败返回错误，便于调用方决定是否中止当前链路。
func (s *AgentService) persistNewAgentConversationMessage(
	ctx context.Context,
	userID int,
	chatID string,
	msg *schema.Message,
	tokensConsumed int,
) error {
	if s == nil || msg == nil {
		return nil
	}
	role := strings.TrimSpace(string(msg.Role))
	content := strings.TrimSpace(msg.Content)
	if role == "" || content == "" {
		return nil
	}
	if userID <= 0 || strings.TrimSpace(chatID) == "" {
		return fmt.Errorf("newAgent visible message persist: invalid conversation identity")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	persistMsg := &schema.Message{
		Role:    msg.Role,
		Content: content,
		// 可见消息持久化只保存正文；模型 raw reasoning 改由 thinking_summary 生成用户可见摘要，
		// 避免历史接口或时间线刷新时重新暴露内部思考文本。
		ReasoningContent: "",
	}
	if len(msg.Extra) > 0 {
		persistMsg.Extra = make(map[string]any, len(msg.Extra))
		for key, value := range msg.Extra {
			persistMsg.Extra[key] = value
		}
	}

	if err := s.agentCache.PushMessage(ctx, chatID, persistMsg); err != nil {
		log.Printf("写入 newAgent 可见消息到 Redis 失败 chat=%s role=%s: %v", chatID, role, err)
	}

	reasoningDurationSeconds := 0
	if persistMsg.Extra != nil {
		switch v := persistMsg.Extra["reasoning_duration_seconds"].(type) {
		case int:
			reasoningDurationSeconds = v
		case int64:
			reasoningDurationSeconds = int(v)
		case float64:
			reasoningDurationSeconds = int(v)
		}
	}

	persistPayload := model.ChatHistoryPersistPayload{
		UserID:                   userID,
		ConversationID:           chatID,
		Role:                     role,
		Message:                  content,
		ReasoningContent:         strings.TrimSpace(persistMsg.ReasoningContent),
		ReasoningDurationSeconds: reasoningDurationSeconds,
		TokensConsumed:           tokensConsumed,
	}
	if err := s.PersistChatHistory(ctx, persistPayload); err != nil {
		return err
	}

	// 统一写入会话时间线，保证正文与卡片可按单一 seq 顺序重建。
	timelineKind := model.AgentTimelineKindAssistantText
	switch role {
	case "user":
		timelineKind = model.AgentTimelineKindUserText
	case "assistant":
		timelineKind = model.AgentTimelineKindAssistantText
	}
	timelinePayload := map[string]any{}
	if reasoningDurationSeconds > 0 {
		timelinePayload["reasoning_duration_seconds"] = reasoningDurationSeconds
	}
	if _, err := s.appendConversationTimelineEvent(
		ctx,
		userID,
		chatID,
		timelineKind,
		role,
		content,
		timelinePayload,
		tokensConsumed,
	); err != nil {
		return err
	}
	return nil
}

// makeRoughBuildFunc 把 AgentService 上的 HybridScheduleWithPlanMultiFunc 封装成
// newAgent 层的 RoughBuildFunc，将 HybridScheduleWithPlanMultiFunc 的结果转换为 RoughBuildPlacement。
// HybridScheduleWithPlanMultiFunc 未注入时返回 nil，RoughBuild 节点会静默跳过粗排。
//
// 修复说明：
// 旧实现使用第二个返回值 []TaskClassItem，只有 EmbeddedTime != nil 的条目（嵌入水课）才生成
// placement，普通时段放置的任务全部被丢弃。
// 正确做法：使用第一个返回值 []HybridScheduleEntry，过滤 Status="suggested" 且 TaskItemID>0 的条目，
// 这样嵌入和非嵌入的粗排结果都能正确写入 ScheduleState。
// adjustNewAgentRequestTokenUsage 负责把本轮 graph 的请求级 token 一次性回写到账本。
//
// 说明：
// 1. newAgent 逐条可见消息都按 0 token 落库，最终统一在这里补记整轮消耗；
// 2. 如果启用了 outbox，就沿用异步 token 调整事件，保持写账口径一致；
// 3. 该步骤属于请求收尾，不应反过来打断用户已看到的回复。
func (s *AgentService) adjustNewAgentRequestTokenUsage(ctx context.Context, userID int, chatID string, deltaTokens int) {
	if s == nil || userID <= 0 || strings.TrimSpace(chatID) == "" || deltaTokens <= 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if s.eventPublisher != nil {
		if err := eventsvc.PublishChatTokenUsageAdjustRequested(ctx, s.eventPublisher, model.ChatTokenUsageAdjustPayload{
			UserID:         userID,
			ConversationID: chatID,
			TokensDelta:    deltaTokens,
			Reason:         "new_agent_request",
			TriggeredAt:    time.Now(),
		}); err != nil {
			log.Printf("写入 newAgent 请求级 token 调整事件失败 chat=%s tokens=%d err=%v", chatID, deltaTokens, err)
		}
		return
	}

	if err := s.repo.AdjustTokenUsage(ctx, userID, chatID, deltaTokens); err != nil {
		log.Printf("同步写入 newAgent 请求级 token 调整失败 chat=%s tokens=%d err=%v", chatID, deltaTokens, err)
	}
}

func (s *AgentService) makeRoughBuildFunc() newagentmodel.RoughBuildFunc {
	if s.HybridScheduleWithPlanMultiFunc == nil {
		return nil
	}
	return func(ctx context.Context, userID int, taskClassIDs []int) ([]newagentmodel.RoughBuildPlacement, error) {
		entries, _, err := s.HybridScheduleWithPlanMultiFunc(ctx, userID, taskClassIDs)
		if err != nil {
			return nil, err
		}
		placements := make([]newagentmodel.RoughBuildPlacement, 0, len(entries))
		for _, entry := range entries {
			if entry.Status != "suggested" || entry.TaskItemID == 0 {
				continue
			}
			placements = append(placements, newagentmodel.RoughBuildPlacement{
				TaskItemID:  entry.TaskItemID,
				Week:        entry.Week,
				DayOfWeek:   entry.DayOfWeek,
				SectionFrom: entry.SectionFrom,
				SectionTo:   entry.SectionTo,
			})
		}
		return placements, nil
	}
}

// makeWriteSchedulePreviewFunc 封装 cacheDAO 写排程预览缓存的操作，供 Execute/Deliver 节点复用。
func (s *AgentService) makeWriteSchedulePreviewFunc() newagentmodel.WriteSchedulePreviewFunc {
	if s.cacheDAO == nil {
		return nil
	}
	return func(ctx context.Context, state *schedule.ScheduleState, userID int, conversationID string, taskClassIDs []int) error {
		stateDigest := summarizeScheduleStateForPreviewDebug(state)
		preview := newagentconv.ScheduleStateToPreview(state, userID, conversationID, taskClassIDs, "")
		if preview == nil {
			log.Printf("[WARN] schedule preview skipped chat=%s user=%d state=%s", conversationID, userID, stateDigest)
			return nil
		}
		previewDigest := summarizeHybridEntriesForPreviewDebug(preview.HybridEntries)
		log.Printf(
			"[DEBUG] schedule preview write chat=%s user=%d state=%s preview=%s generated_at=%s",
			conversationID,
			userID,
			stateDigest,
			previewDigest,
			preview.GeneratedAt.Format(time.RFC3339),
		)
		return s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, conversationID, preview)
	}
}

// summarizeScheduleStateForPreviewDebug 统计 Deliver 写预览前的内存日程摘要。
func summarizeScheduleStateForPreviewDebug(state *schedule.ScheduleState) string {
	if state == nil {
		return "state=nil"
	}

	total := len(state.Tasks)
	pendingTotal := 0
	suggestedTotal := 0
	existingTotal := 0
	taskItemWithSlot := 0
	eventWithSlot := 0
	for i := range state.Tasks {
		t := &state.Tasks[i]
		hasSlot := len(t.Slots) > 0

		switch {
		case schedule.IsPendingTask(*t):
			pendingTotal++
		case schedule.IsSuggestedTask(*t):
			suggestedTotal++
		case schedule.IsExistingTask(*t):
			existingTotal++
		}
		if hasSlot {
			if t.Source == "task_item" {
				taskItemWithSlot++
			}
			if t.Source == "event" {
				eventWithSlot++
			}
		}
	}
	return fmt.Sprintf(
		"tasks=%d pending=%d suggested=%d existing=%d task_item_with_slot=%d event_with_slot=%d",
		total,
		pendingTotal,
		suggestedTotal,
		existingTotal,
		taskItemWithSlot,
		eventWithSlot,
	)
}

// summarizeHybridEntriesForPreviewDebug 统计预览转换后的 HybridEntries 摘要。
func summarizeHybridEntriesForPreviewDebug(entries []model.HybridScheduleEntry) string {
	existing := 0
	suggested := 0
	taskType := 0
	courseType := 0
	for _, e := range entries {
		if e.Status == "suggested" {
			suggested++
		} else {
			existing++
		}
		if e.Type == "task" {
			taskType++
		}
		if e.Type == "course" {
			courseType++
		}
	}
	return fmt.Sprintf(
		"entries=%d existing=%d suggested=%d task_type=%d course_type=%d",
		len(entries),
		existing,
		suggested,
		taskType,
		courseType,
	)
}

// --- 依赖注入字段 ---

// toolRegistry 由 cmd/start.go 注入
func (s *AgentService) SetToolRegistry(registry *newagenttools.ToolRegistry) {
	s.toolRegistry = registry
}

// scheduleProvider 由 cmd/start.go 注入
func (s *AgentService) SetScheduleProvider(provider newagentmodel.ScheduleStateProvider) {
	s.scheduleProvider = provider
}

// agentStateStore 由 cmd/start.go 注入
func (s *AgentService) SetAgentStateStore(store newagentmodel.AgentStateStore) {
	s.agentStateStore = store
}

// compactionStore 由 cmd/start.go 注入
func (s *AgentService) SetCompactionStore(store newagentmodel.CompactionStore) {
	s.compactionStore = store
}

// quickTaskDeps 由 cmd/start.go 注入
func (s *AgentService) SetQuickTaskDeps(deps newagentmodel.QuickTaskDeps) {
	s.quickTaskDeps = deps
}
