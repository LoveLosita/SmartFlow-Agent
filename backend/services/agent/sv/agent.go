package sv

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agentprompt "github.com/LoveLosita/smartflow/backend/services/agent/prompt"
	agentshared "github.com/LoveLosita/smartflow/backend/services/agent/shared"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	"github.com/LoveLosita/smartflow/backend/services/runtime/conv"
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	eventsvc "github.com/LoveLosita/smartflow/backend/services/runtime/eventsvc"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type AgentService struct {
	llmService               *llmservice.Service
	repo                     *dao.AgentDAO
	taskRepo                 *dao.TaskDAO
	cacheDAO                 *dao.CacheDAO
	agentCache               *dao.AgentCache
	activeScheduleDAO        *dao.ActiveScheduleDAO
	activeScheduleSessionDAO *dao.ActiveScheduleSessionDAO
	eventPublisher           outboxinfra.EventPublisher

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

	// ── 任务紧急性提升依赖（函数注入，避免 service 包循环依赖）──

	// GetTasksWithUrgencyPromotionFunc 读取用户任务并应用读时紧急性提升 + 异步落库触发。
	// 未注入时，QueryTasksForTool 回退到旧逻辑（纯内存提升，不持久化）。
	GetTasksWithUrgencyPromotionFunc func(ctx context.Context, userID int) ([]model.Task, error)

	// ── agent 依赖（由 cmd/start.go 通过 Set* 方法注入）──
	toolRegistry     *agenttools.ToolRegistry
	scheduleProvider agentmodel.ScheduleStateProvider
	agentStateStore  agentmodel.AgentStateStore
	compactionStore  agentmodel.CompactionStore
	quickTaskDeps    agentmodel.QuickTaskDeps
	memoryReader     MemoryReader
	memoryCfg        memorymodel.Config
	memoryObserver   memoryobserve.Observer
	memoryMetrics    memoryobserve.MetricsRecorder
	activeRerunFunc  ActiveScheduleSessionRerunFunc
}

// NewAgentService 构造 AgentService。
// 这里通过依赖注入把“模型、仓储、缓存、异步持久化通道”统一交给服务层管理，
// 便于后续在单测中替换实现，或在启动流程中按环境切换配置。
func NewAgentService(
	llmService *llmservice.Service,
	repo *dao.AgentDAO,
	taskRepo *dao.TaskDAO,
	cacheDAO *dao.CacheDAO,
	agentRedis *dao.AgentCache,
	activeScheduleDAO *dao.ActiveScheduleDAO,
	activeSessionDAO *dao.ActiveScheduleSessionDAO,
	eventPublisher outboxinfra.EventPublisher,
) *AgentService {
	// 全局注册一次 token 采集 callback：
	// 1. 只注册一次，避免重复处理；
	// 2. 只有带 RequestTokenMeter 的请求上下文才会真正累加。
	ensureTokenMeterCallbackRegistered()

	return &AgentService{
		llmService:               llmService,
		repo:                     repo,
		taskRepo:                 taskRepo,
		cacheDAO:                 cacheDAO,
		agentCache:               agentRedis,
		activeScheduleDAO:        activeScheduleDAO,
		activeScheduleSessionDAO: activeSessionDAO,
		eventPublisher:           eventPublisher,
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

// thinkingModeToBool 将前端传入的 thinking 模式转换为旧链路所需的 bool 值。
// 仅 "true" 返回 true，其余（"false"/"auto"/""）均返回 false。
func thinkingModeToBool(mode string) bool {
	return strings.TrimSpace(strings.ToLower(mode)) == "true"
}

// pickChatModel 根据请求选择模型。
// 当前约定：
// - 旧链路已全面切到 agent graph，这里仅作为 runNormalChatFlow 回退时的模型选择入口；
// - 统一返回 Pro 模型，旧 strategist 参数不再生效。
func (s *AgentService) pickChatModel(requestModel string) (*llmservice.Client, string) {
	if s == nil || s.llmService == nil {
		return nil, "pro"
	}
	return s.llmService.ProClient(), "pro"
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
			payload.TokensConsumed,
			"",
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

func readAgentExtraBool(extra map[string]any, key string) bool {
	if len(extra) == 0 {
		return false
	}
	raw, ok := extra[key]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return strings.ToLower(strings.TrimSpace(v)) == "true"
	}
	return false
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
	selectedModel *llmservice.Client,
	resolvedModelName string,
	userMessage string,
	userPersisted bool,
	assistantReasoningPrefix string,
	assistantReasoningStartedAt *time.Time,
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
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, agentshared.HistoryFetchLimitByModel(resolvedModelName), chatID)
		if hisErr != nil {
			pushErrNonBlocking(errChan, hisErr)
			return
		}
		chatHistory = conv.ToEinoMessages(histories)
	}

	// 3. 计算本次请求可用的历史 token 预算，并执行历史裁剪。
	//    这样可以在上下文增长时稳定控制模型窗口，避免超长上下文引发报错或高延迟。
	historyBudget := agentshared.HistoryTokenBudgetByModel(resolvedModelName, agentprompt.SystemPrompt, userMessage)
	trimmedHistory, totalHistoryTokens, keptHistoryTokens, droppedCount := agentshared.TrimHistoryByTokenBudget(chatHistory, historyBudget)
	chatHistory = trimmedHistory

	// 4. 根据裁剪后历史长度更新 Redis 会话窗口配置，并主动执行窗口收敛。
	targetWindow := agentshared.CalcSessionWindowSize(len(chatHistory))
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

	// 6.0. 没有可用模型时，直接中止普通聊天，避免写入半截用户消息后没有后续回复。
	if selectedModel == nil {
		pushErrNonBlocking(errChan, errors.New("llm client is not ready"))
		return
	}

	// 6. 执行真正的流式聊天。
	//    fullText 用于后续写 Redis/持久化，outChan 用于把流片段实时推给前端。
	fullText, _, reasoningDurationSeconds, streamUsage, streamErr := s.streamChatFallback(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan, assistantReasoningStartedAt, userID, chatID)
	if streamErr != nil {
		pushErrNonBlocking(errChan, streamErr)
		return
	}

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
	if !userPersisted {
		userMsg := &schema.Message{Role: schema.User, Content: userMessage}
		if err = s.agentCache.PushMessage(ctx, chatID, userMsg); err != nil {
			log.Printf("写入用户消息到 Redis 失败: %v", err)
		}

		if err = s.PersistChatHistory(ctx, model.ChatHistoryPersistPayload{
			UserID:                   userID,
			ConversationID:           chatID,
			Role:                     "user",
			Message:                  userMessage,
			ReasoningContent:         "",
			ReasoningDurationSeconds: 0,
			// 口径 B：用户消息固定记 0；本轮总 token 统一记在助手消息。
			TokensConsumed: 0,
		}); err != nil {
			pushErrNonBlocking(errChan, err)
			return
		}
		if _, timelineErr := s.appendConversationTimelineEvent(
			ctx,
			userID,
			chatID,
			model.AgentTimelineKindUserText,
			"user",
			userMessage,
			nil,
			0,
		); timelineErr != nil {
			pushErrNonBlocking(errChan, timelineErr)
			return
		}
	}

	// 普通聊天链路也需要把助手回复写入 Redis，
	// 否则会出现“数据库有助手消息，但 Redis 最新会话只有用户消息”的口径不一致。
	// 8. 后置持久化（助手消息）：
	//    8.1 先写 Redis，保证下一轮上下文可见；
	//    8.2 再异步可靠落库，失败通过 errChan 回传给上层。
	assistantMsg := &schema.Message{Role: schema.Assistant, Content: fullText}
	if reasoningDurationSeconds > 0 {
		assistantMsg.Extra = map[string]any{"reasoning_duration_seconds": reasoningDurationSeconds}
	}
	if err = s.agentCache.PushMessage(context.Background(), chatID, assistantMsg); err != nil {
		log.Printf("写入助手消息到 Redis 失败: %v", err)
	}

	if saveErr := s.PersistChatHistory(context.Background(), model.ChatHistoryPersistPayload{
		UserID:                   userID,
		ConversationID:           chatID,
		Role:                     "assistant",
		Message:                  fullText,
		ReasoningContent:         "",
		ReasoningDurationSeconds: reasoningDurationSeconds,
		// 口径B：助手消息记录“本轮请求总 token”。
		TokensConsumed: requestTotalTokens,
	}); saveErr != nil {
		pushErrNonBlocking(errChan, saveErr)
	} else {
		assistantTimelinePayload := map[string]any{}
		if reasoningDurationSeconds > 0 {
			assistantTimelinePayload["reasoning_duration_seconds"] = reasoningDurationSeconds
		}
		if _, timelineErr := s.appendConversationTimelineEvent(
			context.Background(),
			userID,
			chatID,
			model.AgentTimelineKindAssistantText,
			"assistant",
			fullText,
			assistantTimelinePayload,
			requestTotalTokens,
		); timelineErr != nil {
			pushErrNonBlocking(errChan, timelineErr)
		}
	}

	// 9. 在主回复完成后异步尝试生成会话标题（仅首次、仅标题为空时生效）。
	//    该步骤不影响当前请求返回时延，也不影响聊天主链路成功与否。
	s.ensureConversationTitleAsync(userID, chatID)
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, thinkingMode string, modelName string, userID int, chatID string, extra map[string]any) (<-chan string, <-chan error) {
	requestStart := time.Now()
	traceID := uuid.NewString()

	outChan := make(chan string, 256)
	errChan := make(chan error, 1)

	go func() {
		defer close(outChan)
		s.runAgentGraph(ctx, userMessage, thinkingMode, modelName, userID, chatID, extra, traceID, requestStart, outChan, errChan)
	}()

	return outChan, errChan
}
