package agentsvc

import (
	"context"
	"log"
	"strings"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/memory/observe"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

const (
	newAgentMemoryRetrieveLimit = 10
	newAgentMemoryIntroLine     = "以下是与当前对话相关的用户记忆，仅在自然且确实有帮助时参考，不要生硬复述。"
)

// MemoryReader 描述 newAgent 主链路读取记忆所需的最小能力。
//
// 职责边界：
// 1. 只负责"按当前输入取回候选记忆"；
// 2. 不负责 prompt 拼装，也不要求调用方感知 memory 模块内部 repo/service 结构；
// 3. 返回值直接复用 memory DTO，避免 service 层再维护一套重复结构。
type MemoryReader interface {
	Retrieve(ctx context.Context, req memorymodel.RetrieveRequest) ([]memorymodel.ItemDTO, error)
}

type memoryObserveProvider interface {
	MemoryObserver() memoryobserve.Observer
	MemoryMetrics() memoryobserve.MetricsRecorder
}

// SetMemoryReader 注入 newAgent 主链路读取记忆所需的薄接口与渲染配置。
func (s *AgentService) SetMemoryReader(reader MemoryReader, cfg memorymodel.Config) {
	s.memoryReader = reader
	s.memoryCfg = cfg
	s.memoryObserver = memoryobserve.NewNopObserver()
	s.memoryMetrics = memoryobserve.NewNopMetrics()
	if provider, ok := reader.(memoryObserveProvider); ok {
		s.memoryObserver = provider.MemoryObserver()
		s.memoryMetrics = provider.MemoryMetrics()
	}
}

// injectMemoryContext 在 graph 执行前，把本轮相关记忆写入 ConversationContext 的 pinned block。
//
// 改造后采用"预取管线"模式：
// 1. 先读 Redis 预取缓存（上一轮写入），命中则立即注入到 ConversationContext；
// 2. 再启动后台 goroutine 做完整记忆检索，渲染后发到 channel + 写 Redis；
// 3. Chat 节点直接用缓存记忆启动（首字节零延迟），Execute/Plan 通过 channel 消费最新结果。
func (s *AgentService) injectMemoryContext(
	ctx context.Context,
	conversationContext *newagentmodel.ConversationContext,
	userID int,
	chatID string,
	userMessage string,
) chan string {
	memoryFuture := make(chan string, 1)

	if conversationContext == nil {
		return memoryFuture
	}

	// 1. 门控检查：无 reader 或无效用户时清掉旧 block 并返回空 channel。
	if s.memoryReader == nil || userID <= 0 {
		conversationContext.RemovePinnedBlock(newagentmodel.MemoryContextBlockKey)
		return memoryFuture
	}

	// 2. 读 Redis 预取缓存（<5ms），命中则注入。
	cachedItems, _ := s.cacheDAO.GetMemoryPrefetchCache(ctx, userID, chatID)
	if len(cachedItems) > 0 {
		content := renderMemoryPinnedContentByMode(cachedItems, s.memoryCfg.EffectiveInjectRenderMode())
		if content != "" {
			conversationContext.UpsertPinnedBlock(newagentmodel.ContextBlock{
				Key:     newagentmodel.MemoryContextBlockKey,
				Title:   newagentmodel.MemoryContextBlockTitle,
				Content: content,
			})
			s.recordMemoryInject(ctx, userID, len(cachedItems), true, nil, "prefetch_cache")
			log.Printf("[INFO] memory prefetch: 从 Redis 缓存注入记忆 user=%d count=%d", userID, len(cachedItems))
		}
	}

	// 3. 短应答不启动后台检索，节省资源。
	if !shouldInjectMemoryForInput(userMessage) {
		log.Printf("[INFO] memory prefetch: 短应答跳过检索 user=%d msg=%q", userID, userMessage)
		return memoryFuture
	}

	// 4. 启动后台 goroutine：完整检索 → 渲染 → 发 channel + 写 Redis。
	log.Printf("[INFO] memory prefetch: 启动后台检索 goroutine user=%d chat=%s", userID, chatID)
	go s.prefetchMemoryForNextTurn(userID, chatID, userMessage, memoryFuture)

	return memoryFuture
}

// prefetchMemoryForNextTurn 后台执行完整记忆检索，将结果渲染后发送到 channel 并写入 Redis。
//
// 职责边界：
// 1. 检索结果渲染为文本后发送到 memoryFuture channel（供 Execute/Plan 节点消费）；
// 2. 原始 ItemDTO 写入 Redis 预取缓存（供下一轮 Chat 节点消费）；
// 3. 检索失败只记日志，不阻断主链路。
func (s *AgentService) prefetchMemoryForNextTurn(userID int, chatID, userMessage string, memoryFuture chan string) {
	bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	items, err := s.memoryReader.Retrieve(bgCtx, memorymodel.RetrieveRequest{
		Query:          strings.TrimSpace(userMessage),
		UserID:         userID,
		ConversationID: strings.TrimSpace(chatID),
		Limit:          newAgentMemoryRetrieveLimit,
		Now:            time.Now(),
	})
	if err != nil {
		log.Printf("[WARN] 记忆预取失败 user=%d chat=%s: %v", userID, chatID, err)
		s.recordMemoryInject(bgCtx, userID, 0, false, err, "prefetch_retrieve")
		return
	}

	log.Printf("[INFO] memory prefetch: 后台检索完成 user=%d count=%d", userID, len(items))

	if len(items) == 0 {
		// 1. 检索为空说明该用户当前没有可用记忆，旧缓存已过期；
		// 2. 主动清除该用户所有会话的预取缓存，避免过期记忆在下一轮继续注入；
		// 3. 清除失败只记日志，不阻断主链路，缓存自然过期也可兜底。
		if cacheErr := s.cacheDAO.DeleteMemoryPrefetchCacheByUser(context.Background(), userID); cacheErr != nil {
			log.Printf("[WARN] memory prefetch cache clear failed (empty result) user=%d: %v", userID, cacheErr)
		}
		return
	}

	// 渲染并发送到 channel（供 Execute/Plan 节点消费）。
	content := renderMemoryPinnedContentByMode(items, s.memoryCfg.EffectiveInjectRenderMode())
	if content != "" {
		memoryFuture <- content
	}

	// 同时写入 Redis 供下一轮 Chat 使用。
	if cacheErr := s.cacheDAO.SetMemoryPrefetchCache(context.Background(), userID, chatID, items); cacheErr != nil {
		log.Printf("[WARN] 记忆预取缓存写入失败 user=%d: %v", userID, cacheErr)
	}
}

// shouldInjectMemoryForInput 判断当前输入是否值得触发一次记忆召回。
//
// 步骤说明：
// 1. 空输入直接跳过；
// 2. 对"好/确认/ok"这类弱语义应答做显式拦截，避免 legacy fallback 在无查询价值时注入一批高分但不相关的旧记忆；
// 3. 其余输入一律放行，优先保证 MVP 可用。
func shouldInjectMemoryForInput(userMessage string) bool {
	trimmed := strings.TrimSpace(userMessage)
	if trimmed == "" {
		return false
	}

	switch strings.ToLower(trimmed) {
	case "好", "好的", "嗯", "嗯嗯", "行", "可以", "收到", "明白", "确认", "取消", "是", "不是", "对", "不对", "ok", "okay", "yes", "no":
		return false
	default:
		return true
	}
}

func (s *AgentService) recordMemoryInject(
	ctx context.Context,
	userID int,
	inputCount int,
	success bool,
	err error,
	source string,
) {
	if s == nil {
		return
	}
	observer := s.memoryObserver
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	metrics := s.memoryMetrics
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}

	level := memoryobserve.LevelInfo
	if err != nil {
		level = memoryobserve.LevelWarn
	}
	observer.Observe(ctx, memoryobserve.Event{
		Level:     level,
		Component: memoryobserve.ComponentInject,
		Operation: memoryobserve.OperationInject,
		Fields: map[string]any{
			"user_id":        userID,
			"inject_mode":    s.memoryCfg.EffectiveInjectRenderMode(),
			"input_count":    inputCount,
			"rendered_count": inputCount,
			"token_budget":   0,
			"fallback":       false,
			"success":        success && err == nil,
			"error":          err,
			"error_code":     memoryobserve.ClassifyError(err),
			"source":         source,
		},
	})
	if inputCount > 0 {
		metrics.AddCounter(memoryobserve.MetricInjectItemTotal, int64(inputCount), map[string]string{
			"inject_mode": s.memoryCfg.EffectiveInjectRenderMode(),
			"source":      source,
		})
	}
}
