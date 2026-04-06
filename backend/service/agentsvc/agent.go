package agentsvc

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	agentchat "github.com/LoveLosita/smartflow/backend/agent/chat"
	agentrouter "github.com/LoveLosita/smartflow/backend/agent/router"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/model"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/respond"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type AgentService struct {
	AIHub          *inits.AIHub
	repo           *dao.AgentDAO
	taskRepo       *dao.TaskDAO
	cacheDAO       *dao.CacheDAO
	agentCache     *dao.AgentCache
	eventPublisher outboxinfra.EventPublisher

	// ── 排程计划依赖（函数注入，避免 service 包循环依赖）──

	// SmartPlanningMultiRawFunc 是可选注入能力：
	// 1. 负责多任务类粗排；
	// 2. 当前主链路主要依赖 HybridScheduleWithPlanMultiFunc，可不强制使用。
	SmartPlanningMultiRawFunc func(ctx context.Context, userID int, taskClassIDs []int) ([]model.UserWeekSchedule, []model.TaskClassItem, error)
	// HybridScheduleWithPlanMultiFunc 是排程链路核心依赖：
	// 1. 负责把“多任务类粗排结果 + 既有日程”合并成 HybridEntries；
	// 2. daily/weekly ReAct 全部基于这个结果继续优化。
	HybridScheduleWithPlanMultiFunc func(ctx context.Context, userID int, taskClassIDs []int) ([]model.HybridScheduleEntry, []model.TaskClassItem, error)
	// ResolvePlanningWindowFunc 负责把 task_class_ids 解析成”全局排程窗口”的相对周/天边界。
	//
	// 作用：
	// 1. 给周级 Move 增加硬边界，避免首尾不足一周时移出有效日期范围；
	// 2. 该函数只做”窗口解析”，不负责粗排与混排计算。
	ResolvePlanningWindowFunc func(ctx context.Context, userID int, taskClassIDs []int) (startWeek, startDay, endWeek, endDay int, err error)

	// ── newAgent 依赖（由 cmd/start.go 通过 Set* 方法注入）──
	toolRegistry      *newagenttools.ToolRegistry
	scheduleProvider  newagentmodel.ScheduleStateProvider
	schedulePersistor newagentmodel.SchedulePersistor
	agentStateStore   newagentmodel.AgentStateStore
}

// NewAgentService 构造 AgentService。
// 这里通过依赖注入把“模型、仓储、缓存、异步持久化通道”统一交给服务层管理，
// 便于后续在单测中替换实现，或在启动流程中按环境切换配置。
func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, taskRepo *dao.TaskDAO, cacheDAO *dao.CacheDAO, agentRedis *dao.AgentCache, eventPublisher outboxinfra.EventPublisher) *AgentService {
	// 全局注册一次 token 采集 callback：
	// 1. 只注册一次，避免重复处理；
	// 2. 只有带 RequestTokenMeter 的请求上下文才会真正累加。
	ensureTokenMeterCallbackRegistered()

	return &AgentService{
		AIHub:          aiHub,
		repo:           repo,
		taskRepo:       taskRepo,
		cacheDAO:       cacheDAO,
		agentCache:     agentRedis,
		eventPublisher: eventPublisher,
	}
}

// normalizeConversationID 规范会话 ID。
// 规则：
// 1) 去除首尾空白；
// 2) 若为空则生成 UUID，保证后续缓存/数据库操作始终有合法 chat_id。
func normalizeConversationID(chatID string) string {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return uuid.NewString()
	}
	return trimmed
}

// pickChatModel 根据请求选择模型。
// 当前约定：
// - strategist：策略模型；
// - 其余值默认 worker（包含空字符串场景）。
func (s *AgentService) pickChatModel(requestModel string) (*ark.ChatModel, string) {
	modelName := strings.TrimSpace(requestModel)
	if strings.EqualFold(modelName, "strategist") {
		return s.AIHub.Strategist, "strategist"
	}
	return s.AIHub.Worker, "worker"
}

// PersistChatHistory 是 Agent 聊天链路唯一的“消息持久化入口”。
//
// 职责边界：
// 1. 负责根据当前部署模式选择“异步 outbox”或“同步直写 DB”；
// 2. 负责把统一 DTO（ChatHistoryPersistPayload）交给下游基础设施；
// 3. 不负责 Redis 上下文写入（Redis 由调用方在链路中先行处理）；
// 4. 不负责消费完成回调（异步模式下由 outbox 消费者负责最终落库）。
func (s *AgentService) PersistChatHistory(ctx context.Context, payload model.ChatHistoryPersistPayload) error {
	// 1. 未注入事件发布器时（例如本地极简环境），直接同步写 DB。
	//    这样可以保证功能不依赖 Kafka 也能跑通。
	if s.eventPublisher == nil {
		return s.repo.SaveChatHistory(
			ctx,
			payload.UserID,
			payload.ConversationID,
			payload.Role,
			payload.Message,
			payload.ReasoningContent,
			payload.ReasoningDurationSeconds,
			payload.RetryGroupID,
			payload.RetryIndex,
			payload.RetryFromUserMessageID,
			payload.RetryFromAssistantMessageID,
			payload.TokensConsumed,
		)
	}
	// 2. 已启用异步总线时，只发布“持久化请求事件”，不在请求路径阻塞 Kafka。
	// 2.1 发布成功仅代表“事件安全入队”，实际落库由消费者异步完成。
	return eventsvc.PublishChatHistoryPersistRequested(ctx, s.eventPublisher, payload)
}

// saveChatHistoryReliable 是历史兼容别名。
// 迁移策略：先保留旧方法名，避免同轮改动跨文件过大；后续可统一替换为 PersistChatHistory。
func (s *AgentService) saveChatHistoryReliable(ctx context.Context, payload model.ChatHistoryPersistPayload) error {
	return s.PersistChatHistory(ctx, payload)
}

func mergeAgentReasoningText(parts ...string) string {
	merged := make([]string, 0, len(parts))
	for _, part := range parts {
		text := strings.TrimSpace(part)
		if text == "" {
			continue
		}
		merged = append(merged, text)
	}
	return strings.Join(merged, "\n\n")
}

type chatRetryMeta struct {
	GroupID                string
	Index                  int
	FromUserMessageID      int
	FromAssistantMessageID int
}

func (m *chatRetryMeta) GroupIDPtr() *string {
	if m == nil || strings.TrimSpace(m.GroupID) == "" {
		return nil
	}
	groupID := strings.TrimSpace(m.GroupID)
	return &groupID
}

func (m *chatRetryMeta) IndexPtr() *int {
	if m == nil || m.Index <= 0 {
		return nil
	}
	index := m.Index
	return &index
}

func (m *chatRetryMeta) FromUserMessageIDPtr() *int {
	if m == nil || m.FromUserMessageID <= 0 {
		return nil
	}
	id := m.FromUserMessageID
	return &id
}

func (m *chatRetryMeta) FromAssistantMessageIDPtr() *int {
	if m == nil || m.FromAssistantMessageID <= 0 {
		return nil
	}
	id := m.FromAssistantMessageID
	return &id
}

func (m *chatRetryMeta) CacheExtra() map[string]any {
	if m == nil || strings.TrimSpace(m.GroupID) == "" || m.Index <= 0 {
		return nil
	}
	extra := map[string]any{
		"retry_group_id": m.GroupID,
		"retry_index":    m.Index,
	}
	if m.FromUserMessageID > 0 {
		extra["retry_from_user_message_id"] = m.FromUserMessageID
	}
	if m.FromAssistantMessageID > 0 {
		extra["retry_from_assistant_message_id"] = m.FromAssistantMessageID
	}
	return extra
}

func (s *AgentService) buildChatRetryMeta(ctx context.Context, userID int, chatID string, extra map[string]any) (*chatRetryMeta, error) {
	if len(extra) == 0 {
		return nil, nil
	}
	requestMode := strings.ToLower(strings.TrimSpace(readAgentExtraString(extra, "request_mode")))
	if requestMode != "retry" {
		return nil, nil
	}

	groupID := strings.TrimSpace(readAgentExtraString(extra, "retry_group_id"))
	if groupID == "" {
		groupID = uuid.NewString()
	}

	sourceUserMessageID := readAgentExtraInt(extra, "retry_from_user_message_id")
	sourceAssistantMessageID := readAgentExtraInt(extra, "retry_from_assistant_message_id")
	// 1. retry 请求必须明确指向“被重试的那一轮 user + assistant”。
	// 2. 若这里拿不到有效父消息 id，继续写库只会生成一组孤立的 index=1 重试消息。
	// 3. 因此直接拒绝本次请求，让前端刷新历史后重试，比静默写脏数据更安全。
	if sourceUserMessageID <= 0 || sourceAssistantMessageID <= 0 {
		return nil, errors.New("重试请求缺少有效的父消息ID，请刷新会话后重试")
	}
	// 4. 再进一步校验父消息确实属于当前用户与当前会话，且角色语义正确。
	// 5. 这样即便前端误把占位 id 或串号 id 发过来，后端也不会继续落错库。
	if err := s.repo.ValidateRetrySourceMessages(ctx, userID, chatID, sourceUserMessageID, sourceAssistantMessageID); err != nil {
		return nil, errors.New("重试引用的父消息无效，请刷新会话后重试")
	}

	if err := s.repo.EnsureRetryGroupSeed(ctx, userID, chatID, groupID, sourceUserMessageID, sourceAssistantMessageID); err != nil {
		return nil, err
	}
	if s.agentCache != nil && (sourceUserMessageID > 0 || sourceAssistantMessageID > 0) {
		if cacheErr := s.agentCache.ApplyRetrySeed(ctx, chatID, groupID, sourceUserMessageID, sourceAssistantMessageID); cacheErr != nil {
			log.Printf("更新重试分组缓存失败 chat=%s group=%s err=%v", chatID, groupID, cacheErr)
		}
	}

	nextIndex, err := s.repo.GetRetryGroupNextIndex(ctx, userID, chatID, groupID)
	if err != nil {
		return nil, err
	}

	return &chatRetryMeta{
		GroupID:                groupID,
		Index:                  nextIndex,
		FromUserMessageID:      sourceUserMessageID,
		FromAssistantMessageID: sourceAssistantMessageID,
	}, nil
}

func readAgentExtraString(extra map[string]any, key string) string {
	if len(extra) == 0 {
		return ""
	}
	raw, ok := extra[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func readAgentExtraInt(extra map[string]any, key string) int {
	if len(extra) == 0 {
		return 0
	}
	raw, ok := extra[key]
	if !ok {
		return 0
	}
	// 1. 前端的历史消息 id 在本地态里可能是 string，也可能是 number。
	// 2. 重试链路只要这里解析失败，父消息 id 就会退化成 0，后续写库自然会落成 NULL。
	// 3. 因此这里统一做“宽松整型解析”，兼容 JSON number、前端字符串数字和常见整数类型。
	value, ok := parseAgentLooseInt(raw)
	if !ok || value <= 0 {
		return 0
	}
	return value
}

// readAgentExtraIntSlice 从 extra 中提取 []int。
// 支持 JSON 数组格式（[]any，每个元素为 float64/int）。
func readAgentExtraIntSlice(extra map[string]any, key string) []int {
	if len(extra) == 0 {
		return nil
	}
	raw, ok := extra[key]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}
	result := make([]int, 0, len(arr))
	for _, item := range arr {
		if v, ok := parseAgentLooseInt(item); ok && v > 0 {
			result = append(result, v)
		}
	}
	return result
}

// parseAgentLooseInt 负责把 extra 中的”弱类型数字”归一成 int。
//
// 职责边界：
// 1. 负责兼容前端 JSON 解码后的常见数值类型，以及字符串形式的数字。
// 2. 不负责业务语义校验；例如是否必须大于 0，由调用方自行决定。
// 3. 解析失败时返回 ok=false，调用方可按各自场景走兜底逻辑。
func parseAgentLooseInt(raw any) (value int, ok bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		if parsed, err := v.Int64(); err == nil {
			return int(parsed), true
		}
		if parsed, err := v.Float64(); err == nil {
			return int(parsed), true
		}
		return 0, false
	case string:
		trimmed := strings.TrimSpace(v)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

// pushErrNonBlocking 向错误通道“尽力投递”错误。
// 目的：
// 1) 避免 goroutine 在 errChan 满时被阻塞导致泄漏；
// 2) 保证主业务协程不因“错误上报拥塞”卡死。
func pushErrNonBlocking(errChan chan error, err error) {
	select {
	case errChan <- err:
	default:
		log.Printf("错误通道已满，丢弃错误: %v", err)
	}
}

// runNormalChatFlow 执行普通流式聊天链路（非随口记）。
// 该函数被两处复用：
// 1) 用户输入本就不是随口记；
// 2) 开启随口记进度推送后，最终判定“非随口记”时回落到普通聊天。
func (s *AgentService) runNormalChatFlow(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	resolvedModelName string,
	userMessage string,
	assistantReasoningPrefix string,
	assistantReasoningStartedAt *time.Time,
	retryMeta *chatRetryMeta,
	ifThinking bool,
	userID int,
	chatID string,
	traceID string,
	requestStart time.Time,
	outChan chan<- string,
	errChan chan error,
) {
	// 1. 先尝试从 Redis 读历史，命中可直接进入模型推理，减少 DB 压力。
	chatHistory, err := s.agentCache.GetHistory(ctx, chatID)
	if err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}

	cacheMiss := false
	if chatHistory == nil {
		// 2. 缓存未命中时回源 DB，并转换为 Eino message 格式。
		cacheMiss = true
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel(resolvedModelName), chatID)
		if hisErr != nil {
			pushErrNonBlocking(errChan, hisErr)
			return
		}
		chatHistory = conv.ToEinoMessages(histories)
	}

	// 3. 计算本次请求可用的历史 token 预算，并执行历史裁剪。
	//    这样可以在上下文增长时稳定控制模型窗口，避免超长上下文引发报错或高延迟。
	historyBudget := pkg.HistoryTokenBudgetByModel(resolvedModelName, agentchat.SystemPrompt, userMessage)
	trimmedHistory, totalHistoryTokens, keptHistoryTokens, droppedCount := pkg.TrimHistoryByTokenBudget(chatHistory, historyBudget)
	chatHistory = trimmedHistory

	// 4. 根据裁剪后历史长度更新 Redis 会话窗口配置，并主动执行窗口收敛。
	targetWindow := pkg.CalcSessionWindowSize(len(chatHistory))
	if err = s.agentCache.SetSessionWindowSize(ctx, chatID, targetWindow); err != nil {
		log.Printf("设置历史窗口失败 chat=%s: %v", chatID, err)
	}
	if err = s.agentCache.EnforceHistoryWindow(ctx, chatID); err != nil {
		log.Printf("执行历史窗口裁剪失败 chat=%s: %v", chatID, err)
	}

	if droppedCount > 0 {
		log.Printf("历史裁剪: chat=%s total_tokens=%d kept_tokens=%d dropped=%d budget=%d target_window=%d",
			chatID, totalHistoryTokens, keptHistoryTokens, droppedCount, historyBudget, targetWindow)
	}

	if cacheMiss {
		// 5. 回源后把历史回填到 Redis，减少下一次请求的冷启动成本。
		if err = s.agentCache.BackfillHistory(ctx, chatID, chatHistory); err != nil {
			pushErrNonBlocking(errChan, err)
			return
		}
	}

	// 6. 执行真正的流式聊天。
	//    fullText 用于后续写 Redis/持久化，outChan 用于把流片段实时推给前端。
	fullText, reasoningText, reasoningDurationSeconds, streamUsage, streamErr := agentchat.StreamChat(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan, traceID, chatID, requestStart, assistantReasoningStartedAt)
	if streamErr != nil {
		pushErrNonBlocking(errChan, streamErr)
		return
	}
	assistantReasoning := mergeAgentReasoningText(assistantReasoningPrefix, reasoningText)

	// 6.1 流式 usage 并入请求级 token 统计器：
	// 6.1.1 route/quicknote/taskquery 等 Generate 调用由 callback 自动累加；
	// 6.1.2 主对话 Stream usage 在这里手动补齐。
	addSchemaUsageIntoRequest(ctx, streamUsage)
	requestTokenSnapshot := snapshotRequestTokenMeter(ctx)
	requestTotalTokens := requestTokenSnapshot.TotalTokens
	if requestTotalTokens <= 0 && streamUsage != nil {
		// 兜底：若 callback/meter 未生效，至少使用流式 usage 保底记账。
		requestTotalTokens = normalizeUsageTotal(streamUsage.TotalTokens, streamUsage.PromptTokens, streamUsage.CompletionTokens)
	}

	// 7. 后置持久化（用户消息）：
	//    7.1 先写 Redis，保证“最新会话上下文”可立即用于下一轮推理；
	//    7.2 再走可靠持久化入口（outbox 或同步 DB）。
	userMsg := &schema.Message{Role: schema.User, Content: userMessage}
	if retryExtra := retryMeta.CacheExtra(); len(retryExtra) > 0 {
		userMsg.Extra = retryExtra
	}
	if err = s.agentCache.PushMessage(ctx, chatID, userMsg); err != nil {
		log.Printf("写入用户消息到 Redis 失败: %v", err)
	}

	if err = s.PersistChatHistory(ctx, model.ChatHistoryPersistPayload{
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
		// 口径B：用户消息固定记 0；本轮总 token 统一记在助手消息。
		TokensConsumed: 0,
	}); err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}
	s.appendConversationHistoryCacheOptimistically(
		context.Background(),
		userID,
		chatID,
		buildOptimisticConversationHistoryItem(
			"user",
			userMessage,
			"",
			0,
			retryMeta,
			requestStart,
		),
	)

	// 普通聊天链路也需要把助手回复写入 Redis，
	// 否则会出现“数据库有助手消息，但 Redis 最新会话只有用户消息”的口径不一致。
	// 8. 后置持久化（助手消息）：
	//    8.1 先写 Redis，保证下一轮上下文可见；
	//    8.2 再异步可靠落库，失败通过 errChan 回传给上层。
	assistantMsg := &schema.Message{Role: schema.Assistant, Content: fullText, ReasoningContent: assistantReasoning}
	if reasoningDurationSeconds > 0 {
		assistantMsg.Extra = map[string]any{"reasoning_duration_seconds": reasoningDurationSeconds}
	}
	if retryExtra := retryMeta.CacheExtra(); len(retryExtra) > 0 {
		if assistantMsg.Extra == nil {
			assistantMsg.Extra = make(map[string]any, len(retryExtra))
		}
		for key, value := range retryExtra {
			assistantMsg.Extra[key] = value
		}
	}
	if err = s.agentCache.PushMessage(context.Background(), chatID, assistantMsg); err != nil {
		log.Printf("写入助手消息到 Redis 失败: %v", err)
	}

	if saveErr := s.PersistChatHistory(context.Background(), model.ChatHistoryPersistPayload{
		UserID:                      userID,
		ConversationID:              chatID,
		Role:                        "assistant",
		Message:                     fullText,
		ReasoningContent:            assistantReasoning,
		ReasoningDurationSeconds:    reasoningDurationSeconds,
		RetryGroupID:                retryMeta.GroupIDPtr(),
		RetryIndex:                  retryMeta.IndexPtr(),
		RetryFromUserMessageID:      retryMeta.FromUserMessageIDPtr(),
		RetryFromAssistantMessageID: retryMeta.FromAssistantMessageIDPtr(),
		// 口径B：助手消息记录“本轮请求总 token”。
		TokensConsumed: requestTotalTokens,
	}); saveErr != nil {
		pushErrNonBlocking(errChan, saveErr)
	} else {
		s.appendConversationHistoryCacheOptimistically(
			context.Background(),
			userID,
			chatID,
			buildOptimisticConversationHistoryItem(
				"assistant",
				fullText,
				assistantReasoning,
				reasoningDurationSeconds,
				retryMeta,
				time.Now(),
			),
		)
	}

	// 9. 在主回复完成后异步尝试生成会话标题（仅首次、仅标题为空时生效）。
	//    该步骤不影响当前请求返回时延，也不影响聊天主链路成功与否。
	s.ensureConversationTitleAsync(userID, chatID)
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string, extra map[string]any) (<-chan string, <-chan error) {
	requestStart := time.Now()
	traceID := uuid.NewString()

	outChan := make(chan string, 256)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		s.runNewAgentGraph(ctx, userMessage, ifThinking, modelName, userID, chatID, extra, traceID, requestStart, outChan, errChan)
	}()

	return outChan, errChan
}

// agentChatOld 是旧路由逻辑的备份，暂时保留供回滚使用。
// TODO: 新 graph 稳定后删除。
func (s *AgentService) agentChatOld(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string, extra map[string]any) (<-chan string, <-chan error) {
	requestStart := time.Now()
	traceID := uuid.NewString()

	outChan := make(chan string, 256)
	errChan := make(chan error, 1)

	// 0. 初始化”请求级 token 统计器”，用于聚合本次请求所有模型开销。
	requestCtx, _ := withRequestTokenMeter(ctx)

	// 1) 规范会话 ID，选择模型。
	chatID = normalizeConversationID(chatID)
	selectedModel, resolvedModelName := s.pickChatModel(modelName)

	// 2) 确保会话存在（优先缓存，必要时回源 DB 并创建）。
	// 2.1 先查 Redis 会话标记，命中则可跳过 DB 存在性校验。
	result, err := s.agentCache.GetConversationStatus(requestCtx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	if !result {
		// 2.2 缓存未命中时回源 DB：确认会话是否存在。
		innerResult, ifErr := s.repo.IfChatExists(requestCtx, userID, chatID)
		if ifErr != nil {
			errChan <- ifErr
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
		if !innerResult {
			// 2.3 DB 里也不存在则创建新会话。
			if _, err = s.repo.CreateNewChat(userID, chatID); err != nil {
				errChan <- err
				close(outChan)
				close(errChan)
				return outChan, errChan
			}
		}
		// 2.4 补写 Redis 会话标记，优化下次访问。
		if err = s.agentCache.SetConversationStatus(requestCtx, chatID); err != nil {
			log.Printf("设置会话状态缓存失败 chat=%s: %v", chatID, err)
		}
	}

	retryMeta, err := s.buildChatRetryMeta(requestCtx, userID, chatID, extra)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}

	// 3) 统一异步分流：
	// 3.1 先走“通用控制码路由”决定 action（chat / quick_note_create / task_query）；
	// 3.2 quick_note_create 进入随口记 graph；
	// 3.3 task_query 进入任务查询 tool-calling；
	// 3.4 chat 直接普通流式聊天。
	go func() {
		defer close(outChan)

		// 3.1 先走轻量路由，拿到统一 action。
		routing := s.decideActionRouting(requestCtx, selectedModel, userMessage)
		if routing.RouteFailed {
			// 3.1.1 路由码失败不再回落聊天。
			// 3.1.2 直接返回内部错误，避免误进入业务分支导致“吐错内容”（例如吐排程 JSON）。
			pushErrNonBlocking(errChan, respond.RouteControlInternalError)
			return
		}

		// 3.2 chat：直接走普通聊天主链路。
		if routing.Action == agentrouter.ActionChat {
			s.runNormalChatFlow(requestCtx, selectedModel, resolvedModelName, userMessage, "", nil, retryMeta, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
			return
		}

		// 3.3 非 chat 分支统一先发“接收成功”阶段，减少用户等待时的“无反馈感”。
		progress := newQuickNoteProgressEmitter(outChan, resolvedModelName, true)
		progress.Emit("request.accepted", routing.Detail)

		// 3.4 quick_note_create：执行随口记 graph。
		if routing.Action == agentrouter.ActionQuickNoteCreate {
			quickHandled, quickState, quickErr := s.tryHandleQuickNoteWithGraph(
				requestCtx,
				selectedModel,
				userMessage,
				userID,
				chatID,
				traceID,
				routing.TrustRoute,
				progress.Emit,
			)
			if quickErr != nil {
				// graph 出错不直接中断用户请求，而是回退普通聊天，保证可用性优先。
				log.Printf("随口记 graph 执行失败，回退普通聊天 trace_id=%s chat_id=%s err=%v", traceID, chatID, quickErr)
			}

			if quickHandled {
				// 3.4.1 随口记处理成功：组织最终回复并按 OpenAI 兼容格式输出。
				progress.Emit("quick_note.reply.polishing", "正在结合你的话题润色回复。")
				quickReply := buildQuickNoteFinalReply(requestCtx, selectedModel, userMessage, quickState)
				if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, quickReply); emitErr != nil {
					pushErrNonBlocking(errChan, emitErr)
					return
				}

				// 3.4.2 对随口记回复执行统一后置持久化（Redis + outbox/DB）。
				requestTotalTokens := snapshotRequestTokenMeter(requestCtx).TotalTokens
				s.persistChatAfterReply(requestCtx, userID, chatID, userMessage, quickReply, progress.HistoryText(), progress.DurationSeconds(time.Now()), retryMeta, 0, requestTotalTokens, errChan)
				// 3.4.3 随口记链路同样异步生成会话标题（仅首次写入）。
				s.ensureConversationTitleAsync(userID, chatID)
				return
			}

			// 3.4.4 路由误判或 graph 判定非随口记时，回落普通聊天，保证“能聊”。
			progress.Emit("quick_note.fallback", "当前输入不是随口记请求，切换到普通对话。")
			s.runNormalChatFlow(requestCtx, selectedModel, resolvedModelName, userMessage, progress.HistoryText(), progress.StartedAt(), retryMeta, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
			return
		}

		// 3.5 task_query：执行任务查询 tool-calling。
		if routing.Action == agentrouter.ActionTaskQuery {
			reply, queryErr := s.runTaskQueryFlow(requestCtx, selectedModel, userMessage, userID, progress.Emit)
			if queryErr != nil {
				// 3.5.1 任务查询失败时回退普通聊天，避免请求直接中断。
				log.Printf("任务查询 tool-calling 执行失败，回退普通聊天 trace_id=%s chat_id=%s err=%v", traceID, chatID, queryErr)
				progress.Emit("task_query.fallback", "任务查询暂不可用，先切回普通对话。")
				s.runNormalChatFlow(requestCtx, selectedModel, resolvedModelName, userMessage, progress.HistoryText(), progress.StartedAt(), retryMeta, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
				return
			}

			// 3.5.2 查询成功后按 OpenAI 兼容格式输出，并执行统一后置持久化。
			if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, reply); emitErr != nil {
				pushErrNonBlocking(errChan, emitErr)
				return
			}
			requestTotalTokens := snapshotRequestTokenMeter(requestCtx).TotalTokens
			s.persistChatAfterReply(requestCtx, userID, chatID, userMessage, reply, progress.HistoryText(), progress.DurationSeconds(time.Now()), retryMeta, 0, requestTotalTokens, errChan)
			s.ensureConversationTitleAsync(userID, chatID)
			return
		}

		// 3.6 schedule_plan：执行智能排程 graph。
		if routing.Action == agentrouter.ActionSchedulePlanCreate {
			reply, planErr := s.runSchedulePlanFlow(requestCtx, selectedModel, userMessage, userID, chatID, traceID, extra, progress.Emit, outChan, resolvedModelName)
			if planErr != nil {
				log.Printf("智能排程 graph 执行失败，回退普通聊天 trace_id=%s chat_id=%s err=%v", traceID, chatID, planErr)
				progress.Emit("schedule_plan.fallback", "智能排程暂不可用，先切回普通对话。")
				s.runNormalChatFlow(requestCtx, selectedModel, resolvedModelName, userMessage, progress.HistoryText(), progress.StartedAt(), retryMeta, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
				return
			}

			if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, reply); emitErr != nil {
				pushErrNonBlocking(errChan, emitErr)
				return
			}
			requestTotalTokens := snapshotRequestTokenMeter(requestCtx).TotalTokens
			s.persistChatAfterReply(requestCtx, userID, chatID, userMessage, reply, progress.HistoryText(), progress.DurationSeconds(time.Now()), retryMeta, 0, requestTotalTokens, errChan)
			s.ensureConversationTitleAsync(userID, chatID)
			return
		}

		// 3.7 schedule_plan_refine：执行“连续微调排程”graph。
		if routing.Action == agentrouter.ActionSchedulePlanRefine {
			reply, refineErr := s.runScheduleRefineFlow(requestCtx, selectedModel, userMessage, userID, chatID, traceID, progress.Emit, outChan, resolvedModelName)
			if refineErr != nil {
				// 连续微调失败不再回落普通聊天，直接上报错误。
				pushErrNonBlocking(errChan, refineErr)
				return
			}

			if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, reply); emitErr != nil {
				pushErrNonBlocking(errChan, emitErr)
				return
			}
			requestTotalTokens := snapshotRequestTokenMeter(requestCtx).TotalTokens
			s.persistChatAfterReply(requestCtx, userID, chatID, userMessage, reply, progress.HistoryText(), progress.DurationSeconds(time.Now()), retryMeta, 0, requestTotalTokens, errChan)
			s.ensureConversationTitleAsync(userID, chatID)
			return
		}

		// 3.8 未知 action 兜底：走普通聊天，保证可用性。
		s.runNormalChatFlow(requestCtx, selectedModel, resolvedModelName, userMessage, progress.HistoryText(), progress.StartedAt(), retryMeta, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
	}()

	return outChan, errChan
}
