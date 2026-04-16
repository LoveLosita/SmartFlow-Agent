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
	newAgentMemoryBlockKey      = "memory_context"
	newAgentMemoryRetrieveLimit = 5
	newAgentMemoryBlockTitle    = "相关记忆"
	newAgentMemoryIntroLine     = "以下是与当前对话相关的用户记忆，仅在自然且确实有帮助时参考，不要生硬复述。"
)

// MemoryReader 描述 newAgent 主链路读取记忆所需的最小能力。
//
// 职责边界：
// 1. 只负责“按当前输入取回候选记忆”；
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
// 步骤说明：
// 1. 先做前置门控：没有 reader、没有有效用户、或输入属于“确认/应答型短句”时，直接清掉旧 block，避免快照残留污染本轮 prompt。
// 2. 再调用 memory 检索：查询失败只记日志，不中断主链路，保证 newAgent 的可用性优先。
// 3. 检索成功后把结果渲染成稳定的中文文本，并用固定 key 覆盖写入，确保每轮都能刷新而不是越积越多。
func (s *AgentService) injectMemoryContext(
	ctx context.Context,
	conversationContext *newagentmodel.ConversationContext,
	userID int,
	chatID string,
	userMessage string,
) {
	if conversationContext == nil {
		return
	}

	if s.memoryReader == nil || userID <= 0 || !shouldInjectMemoryForInput(userMessage) {
		conversationContext.RemovePinnedBlock(newAgentMemoryBlockKey)
		return
	}

	items, err := s.memoryReader.Retrieve(ctx, memorymodel.RetrieveRequest{
		Query:          strings.TrimSpace(userMessage),
		UserID:         userID,
		ConversationID: strings.TrimSpace(chatID),
		Limit:          newAgentMemoryRetrieveLimit,
		Now:            time.Now(),
	})
	if err != nil {
		conversationContext.RemovePinnedBlock(newAgentMemoryBlockKey)
		s.recordMemoryInject(ctx, userID, 0, false, err)
		log.Printf("读取记忆上下文失败 user=%d chat=%s err=%v", userID, chatID, err)
		return
	}

	content := renderMemoryPinnedContentByMode(items, s.memoryCfg.EffectiveInjectRenderMode())
	if content == "" {
		conversationContext.RemovePinnedBlock(newAgentMemoryBlockKey)
		s.recordMemoryInject(ctx, userID, len(items), false, nil)
		return
	}

	conversationContext.UpsertPinnedBlock(newagentmodel.ContextBlock{
		Key:     newAgentMemoryBlockKey,
		Title:   newAgentMemoryBlockTitle,
		Content: content,
	})
	s.recordMemoryInject(ctx, userID, len(items), true, nil)
}

// shouldInjectMemoryForInput 判断当前输入是否值得触发一次记忆召回。
//
// 步骤说明：
// 1. 空输入直接跳过；
// 2. 对“好/确认/ok”这类弱语义应答做显式拦截，避免 legacy fallback 在无查询价值时注入一批高分但不相关的旧记忆；
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
		},
	})
	if inputCount > 0 {
		metrics.AddCounter(memoryobserve.MetricInjectItemTotal, int64(inputCount), map[string]string{
			"inject_mode": s.memoryCfg.EffectiveInjectRenderMode(),
		})
	}
}
