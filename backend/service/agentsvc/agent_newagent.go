package agentsvc

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	newagentgraph "github.com/LoveLosita/smartflow/backend/newAgent/graph"
	newagentllm "github.com/LoveLosita/smartflow/backend/newAgent/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/cloudwego/eino/schema"

	agentchat "github.com/LoveLosita/smartflow/backend/agent/chat"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/pkg"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
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
	ifThinking bool,
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

	// 3. 构建重试元数据。
	retryMeta, err := s.buildChatRetryMeta(requestCtx, userID, chatID, extra)
	if err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}

	// 4. 从 StateStore 加载或创建 RuntimeState。
	//    恢复场景（confirm/ask_user）同时拿到快照中保存的 ConversationContext，
	//    其中包含工具调用/结果等中间消息，保证后续 LLM 调用的消息链完整。
	runtimeState, savedConversationContext := s.loadOrCreateRuntimeState(requestCtx, chatID, userID)

	// 5. 构造 ConversationContext。
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

	// 5.5 若 extra 携带 task_class_ids，写入 CommonState（仅首轮/尚未设置时生效，跨轮持久化）。
	if taskClassIDs := readAgentExtraIntSlice(extra, "task_class_ids"); len(taskClassIDs) > 0 {
		cs := runtimeState.EnsureCommonState()
		if len(cs.TaskClassIDs) == 0 {
			cs.TaskClassIDs = taskClassIDs
			if s.scheduleProvider != nil {
				if metas, metaErr := s.scheduleProvider.LoadTaskClassMetas(requestCtx, userID, taskClassIDs); metaErr != nil {
					log.Printf("加载任务类约束元数据失败 chat=%s err=%v", chatID, metaErr)
				} else {
					cs.TaskClasses = metas
				}
			}
		}
	}

	// 6. 构造 AgentGraphRequest。
	var confirmAction string
	if len(extra) > 0 {
		confirmAction = readAgentExtraString(extra, "confirm_action")
	}
	graphRequest := newagentmodel.AgentGraphRequest{
		UserInput:     userMessage,
		ConfirmAction: confirmAction,
	}
	graphRequest.Normalize()

	// 7. 适配 LLM clients（从 AIHub 的 ark.ChatModel 转换为 newAgent LLM Client）。
	chatClient := newagentllm.WrapArkClient(s.AIHub.Worker)
	planClient := newagentllm.WrapArkClient(s.AIHub.Worker)
	executeClient := newagentllm.WrapArkClient(s.AIHub.Worker)
	deliverClient := newagentllm.WrapArkClient(s.AIHub.Worker)

	// 8. 适配 SSE emitter。
	sseEmitter := newagentstream.NewSSEPayloadEmitter(outChan)
	chunkEmitter := newagentstream.NewChunkEmitter(sseEmitter, traceID, resolvedModelName, requestStart.Unix())

	// 9. 构造 AgentGraphDeps（由 cmd/start.go 注入的依赖）。
	deps := newagentmodel.AgentGraphDeps{
		ChatClient:        chatClient,
		PlanClient:        planClient,
		ExecuteClient:     executeClient,
		DeliverClient:     deliverClient,
		ChunkEmitter:      chunkEmitter,
		StateStore:        s.agentStateStore,
		ToolRegistry:      s.toolRegistry,
		ScheduleProvider:  s.scheduleProvider,
		SchedulePersistor: s.schedulePersistor,
		RoughBuildFunc:    s.makeRoughBuildFunc(),
	}

	// 10. 构造 AgentGraphRunInput 并运行 graph。
	runInput := newagentmodel.AgentGraphRunInput{
		RuntimeState:        runtimeState,
		ConversationContext: conversationContext,
		Request:             graphRequest,
		Deps:                deps,
	}

	finalState, graphErr := newagentgraph.RunAgentGraph(requestCtx, runInput)
	if graphErr != nil {
		log.Printf("[ERROR] newAgent graph 执行失败 trace=%s chat=%s: %v", traceID, chatID, graphErr)
		pushErrNonBlocking(errChan, fmt.Errorf("graph 执行失败: %w", graphErr))

		// Graph 出错时回退普通聊天，保证可用性。
		s.runNormalChatFlow(requestCtx, s.AIHub.Worker, resolvedModelName, userMessage, "", nil, retryMeta, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
		return
	}

	// 11. 持久化聊天历史（用户消息 + 助手回复）。
	s.persistChatAfterGraph(requestCtx, userID, chatID, userMessage, finalState, retryMeta, requestStart, outChan, errChan)
	// 11.5. 将最终状态快照异步写入 MySQL（通过 outbox）。
	// Deliver 节点已将快照保存到 Redis（2h TTL），此处通过 outbox 异步写入 MySQL 做永久存储。
	if finalState != nil {
		snapshot := &newagentmodel.AgentStateSnapshot{
			RuntimeState:        finalState.EnsureRuntimeState(),
			ConversationContext: finalState.EnsureConversationContext(),
		}
		eventsvc.PublishAgentStateSnapshot(requestCtx, s.eventPublisher, snapshot, chatID, userID)
	}

	// 11.6. 将排程结果写入 Redis 预览缓存，复用旧 agent 的 SchedulePlanPreviewCache 格式。
	// 前端通过 GET /agent/schedule-preview 获取，无需改动。
	if finalState != nil && finalState.ScheduleState != nil {
		flowState := finalState.EnsureFlowState()
		preview := conv.ScheduleStateToPreview(
			finalState.ScheduleState,
			userID,
			chatID,
			flowState.TaskClassIDs,
			"", // summary 由转换函数自动生成
		)
		if preview != nil && s.cacheDAO != nil {
			if err := s.cacheDAO.SetSchedulePlanPreviewToCache(requestCtx, userID, chatID, preview); err != nil {
				log.Printf("[WARN] 写入排程预览缓存失败 chat=%s: %v", chatID, err)
			}
		}
	}

	// 12. 发送 OpenAI 兼容的流式结束标记，告知客户端 stream 已完成。
	_ = chunkEmitter.EmitDone()

	// 13. 异步生成会话标题。
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
func (s *AgentService) loadOrCreateRuntimeState(ctx context.Context, chatID string, userID int) (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext) {
	newRT := func() (*newagentmodel.AgentRuntimeState, *newagentmodel.ConversationContext) {
		rt := newagentmodel.NewAgentRuntimeState(nil)
		cs := rt.EnsureCommonState()
		cs.UserID = userID
		cs.ConversationID = chatID // saveAgentState 依赖此字段决定是否持久化
		return rt, nil
	}

	if s.agentStateStore == nil {
		return newRT()
	}

	snapshot, ok, err := s.agentStateStore.Load(ctx, chatID)
	log.Printf("[DEBUG] loadOrCreateRuntimeState chatID=%s ok=%v err=%v hasRuntime=%v hasPending=%v hasCtx=%v",
		chatID, ok, err,
		snapshot != nil && snapshot.RuntimeState != nil,
		snapshot != nil && snapshot.RuntimeState != nil && snapshot.RuntimeState.HasPendingInteraction(),
		snapshot != nil && snapshot.ConversationContext != nil,
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

		// 不需要手动重置 Phase：所有请求统一先过 Chat 节点，Chat 会根据路由决策覆盖 Phase。
		// 保留完整的 RuntimeState（PlanSteps、CurrentStep 等），支持连续对话调整日程。

		return snapshot.RuntimeState, snapshot.ConversationContext
	}
	return newRT()
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
	conversationContext := newagentmodel.NewConversationContext(agentchat.SystemPrompt)
	if history != nil {
		conversationContext.ReplaceHistory(history)
	}

	// 把用户本轮输入追加到历史（供 graph 使用）。
	if strings.TrimSpace(userMessage) != "" {
		conversationContext.AppendHistory(schema.UserMessage(userMessage))
	}

	return conversationContext
}

// persistChatAfterGraph graph 执行完成后持久化聊天历史。
func (s *AgentService) persistChatAfterGraph(
	ctx context.Context,
	userID int,
	chatID string,
	userMessage string,
	finalState *newagentmodel.AgentGraphState,
	retryMeta *chatRetryMeta,
	requestStart time.Time,
	outChan chan<- string,
	errChan chan error,
) {
	if finalState == nil {
		return
	}

	// 1. 持久化用户消息：先写 LLM 上下文 Redis，再落 DB，最后更新 UI 历史缓存。
	userMsg := &schema.Message{Role: schema.User, Content: userMessage}
	if retryExtra := retryMeta.CacheExtra(); len(retryExtra) > 0 {
		userMsg.Extra = retryExtra
	}
	if err := s.agentCache.PushMessage(ctx, chatID, userMsg); err != nil {
		log.Printf("写入用户消息到 LLM 上下文 Redis 失败 chat=%s: %v", chatID, err)
	}

	userPayload := model.ChatHistoryPersistPayload{
		UserID:                      userID,
		ConversationID:              chatID,
		Role:                        "user",
		Message:                     userMessage,
		ReasoningContent:            "",
		ReasoningDurationSeconds:    0,
		RetryGroupID:                retryMeta.GroupIDPtr(),
		RetryIndex:                  retryMeta.IndexPtr(),
		RetryFromUserMessageID:      retryMeta.FromUserMessageIDPtr(),
		RetryFromAssistantMessageID: retryMeta.FromAssistantMessageIDPtr(),
		TokensConsumed:              0,
	}
	if err := s.PersistChatHistory(ctx, userPayload); err != nil {
		pushErrNonBlocking(errChan, err)
	}
	userCreatedAt := time.Now()
	s.appendConversationHistoryCacheOptimistically(
		context.Background(),
		userID,
		chatID,
		buildOptimisticConversationHistoryItem("user", userMessage, "", 0, retryMeta, userCreatedAt),
	)

	// 2. 从 ConversationContext 提取助手回复（最后一条 assistant 消息）。
	conversationContext := finalState.ConversationContext
	if conversationContext == nil || len(conversationContext.History) == 0 {
		return
	}

	var lastAssistantMsg *schema.Message
	for i := len(conversationContext.History) - 1; i >= 0; i-- {
		msg := conversationContext.History[i]
		if msg.Role == schema.Assistant {
			lastAssistantMsg = msg
			break
		}
	}

	if lastAssistantMsg == nil {
		return
	}

	assistantReply := lastAssistantMsg.Content
	reasoningContent := lastAssistantMsg.ReasoningContent
	var reasoningDurationSeconds int
	if lastAssistantMsg.Extra != nil {
		if dur, ok := lastAssistantMsg.Extra["reasoning_duration_seconds"].(float64); ok {
			reasoningDurationSeconds = int(dur)
		}
	}

	// 3. 持久化助手消息：先写 LLM 上下文 Redis，再落 DB，最后更新 UI 历史缓存。
	assistantMsg := &schema.Message{
		Role:             schema.Assistant,
		Content:          assistantReply,
		ReasoningContent: reasoningContent,
	}
	if reasoningDurationSeconds > 0 {
		assistantMsg.Extra = map[string]any{"reasoning_duration_seconds": reasoningDurationSeconds}
	}
	if retryExtra := retryMeta.CacheExtra(); len(retryExtra) > 0 {
		if assistantMsg.Extra == nil {
			assistantMsg.Extra = make(map[string]any)
		}
		for k, v := range retryExtra {
			assistantMsg.Extra[k] = v
		}
	}
	if err := s.agentCache.PushMessage(context.Background(), chatID, assistantMsg); err != nil {
		log.Printf("写入助手消息到 LLM 上下文 Redis 失败 chat=%s: %v", chatID, err)
	}

	requestTotalTokens := snapshotRequestTokenMeter(ctx).TotalTokens
	assistantPayload := model.ChatHistoryPersistPayload{
		UserID:                      userID,
		ConversationID:              chatID,
		Role:                        "assistant",
		Message:                     assistantReply,
		ReasoningContent:            reasoningContent,
		ReasoningDurationSeconds:    reasoningDurationSeconds,
		RetryGroupID:                retryMeta.GroupIDPtr(),
		RetryIndex:                  retryMeta.IndexPtr(),
		RetryFromUserMessageID:      retryMeta.FromUserMessageIDPtr(),
		RetryFromAssistantMessageID: retryMeta.FromAssistantMessageIDPtr(),
		TokensConsumed:              requestTotalTokens,
	}
	if err := s.PersistChatHistory(ctx, assistantPayload); err != nil {
		pushErrNonBlocking(errChan, err)
	} else {
		s.appendConversationHistoryCacheOptimistically(
			context.Background(),
			userID,
			chatID,
			buildOptimisticConversationHistoryItem(
				"assistant",
				assistantReply,
				reasoningContent,
				reasoningDurationSeconds,
				retryMeta,
				time.Now(),
			),
		)
	}
}

// makeRoughBuildFunc 把 AgentService 上的 HybridScheduleWithPlanMultiFunc 封装成
// newAgent 层的 RoughBuildFunc，完成外层 model.TaskClassItem → RoughBuildPlacement 的转换。
// HybridScheduleWithPlanMultiFunc 未注入时返回 nil，RoughBuild 节点会静默跳过粗排。
func (s *AgentService) makeRoughBuildFunc() newagentmodel.RoughBuildFunc {
	if s.HybridScheduleWithPlanMultiFunc == nil {
		return nil
	}
	return func(ctx context.Context, userID int, taskClassIDs []int) ([]newagentmodel.RoughBuildPlacement, error) {
		_, items, err := s.HybridScheduleWithPlanMultiFunc(ctx, userID, taskClassIDs)
		if err != nil {
			return nil, err
		}
		placements := make([]newagentmodel.RoughBuildPlacement, 0, len(items))
		for _, item := range items {
			if item.EmbeddedTime == nil {
				continue
			}
			placements = append(placements, newagentmodel.RoughBuildPlacement{
				TaskItemID:  item.ID,
				Week:        item.EmbeddedTime.Week,
				DayOfWeek:   item.EmbeddedTime.DayOfWeek,
				SectionFrom: item.EmbeddedTime.SectionFrom,
				SectionTo:   item.EmbeddedTime.SectionTo,
			})
		}
		return placements, nil
	}
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

// schedulePersistor 由 cmd/start.go 注入
func (s *AgentService) SetSchedulePersistor(persistor newagentmodel.SchedulePersistor) {
	s.schedulePersistor = persistor
}

// agentStateStore 由 cmd/start.go 注入
func (s *AgentService) SetAgentStateStore(store newagentmodel.AgentStateStore) {
	s.agentStateStore = store
}
