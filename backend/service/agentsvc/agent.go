package agentsvc

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent/chat"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type AgentService struct {
	AIHub         *inits.AIHub
	repo          *dao.AgentDAO
	taskRepo      *dao.TaskDAO
	agentCache    *dao.AgentCache
	asyncPipeline *outboxinfra.ChatHistoryAsync
}

// NewAgentService 构造 AgentService。
// 这里通过依赖注入把“模型、仓储、缓存、异步持久化通道”统一交给服务层管理，
// 便于后续在单测中替换实现，或在启动流程中按环境切换配置。
func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, taskRepo *dao.TaskDAO, agentRedis *dao.AgentCache, asyncPipeline *outboxinfra.ChatHistoryAsync) *AgentService {
	return &AgentService{
		AIHub:         aiHub,
		repo:          repo,
		taskRepo:      taskRepo,
		agentCache:    agentRedis,
		asyncPipeline: asyncPipeline,
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

// saveChatHistoryReliable 统一封装“聊天记录持久化入口”：
// 1) 开启异步链路时，走 outbox + Kafka；
// 2) 未开启时，直接同步写库。
func (s *AgentService) saveChatHistoryReliable(ctx context.Context, payload model.ChatHistoryPersistPayload) error {
	// 1. 未注入异步通道时（例如本地极简环境），直接同步写 DB。
	//    这样可以保证功能不依赖 Kafka 也能跑通。
	if s.asyncPipeline == nil {
		return s.repo.SaveChatHistory(ctx, payload.UserID, payload.ConversationID, payload.Role, payload.Message)
	}
	// 2. 已启用异步通道时，只入 outbox，不在请求路径阻塞 Kafka。
	return s.asyncPipeline.EnqueueChatHistoryPersist(ctx, payload)
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
	historyBudget := pkg.HistoryTokenBudgetByModel(resolvedModelName, chat.SystemPrompt, userMessage)
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
	fullText, streamErr := chat.StreamChat(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan, traceID, chatID, requestStart)
	if streamErr != nil {
		pushErrNonBlocking(errChan, streamErr)
		return
	}

	// 7. 后置持久化（用户消息）：
	//    7.1 先写 Redis，保证“最新会话上下文”可立即用于下一轮推理；
	//    7.2 再走可靠持久化入口（outbox 或同步 DB）。
	if err = s.agentCache.PushMessage(ctx, chatID, &schema.Message{Role: schema.User, Content: userMessage}); err != nil {
		log.Printf("写入用户消息到 Redis 失败: %v", err)
	}

	if err = s.saveChatHistoryReliable(ctx, model.ChatHistoryPersistPayload{
		UserID:         userID,
		ConversationID: chatID,
		Role:           "user",
		Message:        userMessage,
	}); err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}

	// 普通聊天链路也需要把助手回复写入 Redis，
	// 否则会出现“数据库有助手消息，但 Redis 最新会话只有用户消息”的口径不一致。
	// 8. 后置持久化（助手消息）：
	//    8.1 先写 Redis，保证下一轮上下文可见；
	//    8.2 再异步可靠落库，失败通过 errChan 回传给上层。
	if err = s.agentCache.PushMessage(context.Background(), chatID, &schema.Message{Role: schema.Assistant, Content: fullText}); err != nil {
		log.Printf("写入助手消息到 Redis 失败: %v", err)
	}

	if saveErr := s.saveChatHistoryReliable(context.Background(), model.ChatHistoryPersistPayload{
		UserID:         userID,
		ConversationID: chatID,
		Role:           "assistant",
		Message:        fullText,
	}); saveErr != nil {
		pushErrNonBlocking(errChan, saveErr)
	}

	// 9. 在主回复完成后异步尝试生成会话标题（仅首次、仅标题为空时生效）。
	//    该步骤不影响当前请求返回时延，也不影响聊天主链路成功与否。
	s.ensureConversationTitleAsync(userID, chatID)
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string) (<-chan string, <-chan error) {
	requestStart := time.Now()
	traceID := uuid.NewString()

	// 1. 每个请求都返回两个通道：
	//    - outChan：推送流式输出片段；
	//    - errChan：推送异步阶段错误（非阻塞上报）。
	outChan := make(chan string, 8)
	errChan := make(chan error, 1)

	// 1) 规范会话 ID，选择模型。
	chatID = normalizeConversationID(chatID)
	selectedModel, resolvedModelName := s.pickChatModel(modelName)

	// 2) 确保会话存在（优先缓存，必要时回源 DB 并创建）。
	// 2.1 先查 Redis 会话标记，命中则可跳过 DB 存在性校验。
	result, err := s.agentCache.GetConversationStatus(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	if !result {
		// 2.2 缓存未命中时回源 DB：确认会话是否存在。
		innerResult, ifErr := s.repo.IfChatExists(ctx, userID, chatID)
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
		if err = s.agentCache.SetConversationStatus(ctx, chatID); err != nil {
			log.Printf("设置会话状态缓存失败 chat=%s: %v", chatID, err)
		}
	}

	// 3) 统一异步分流：
	// - 先走“模型控制码路由”决定 quick_note / chat；
	// - 路由命中 quick_note 时推阶段状态并执行 graph；
	// - 路由命中 chat 时直接普通流式聊天。
	go func() {
		defer close(outChan)

		// 3.1 先走轻量路由，判断是否进入“随口记”图。
		routing := s.decideQuickNoteRouting(ctx, selectedModel, userMessage)
		if !routing.EnterQuickNote {
			// 3.2 非随口记：直接走普通聊天主链路。
			s.runNormalChatFlow(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
			return
		}

		// 3.3 随口记：先发阶段状态，减少用户等待时的“无反馈感”。
		progress := newQuickNoteProgressEmitter(outChan, resolvedModelName, true)
		progress.Emit("request.accepted", routing.Detail)

		// 3.4 执行随口记 graph。
		quickHandled, quickState, quickErr := s.tryHandleQuickNoteWithGraph(
			ctx,
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
			// 3.5 随口记处理成功：组织最终回复并按 OpenAI 兼容格式输出。
			progress.Emit("quick_note.reply.polishing", "正在结合你的话题润色回复。")
			quickReply := buildQuickNoteFinalReply(ctx, selectedModel, userMessage, quickState)
			if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, quickReply); emitErr != nil {
				pushErrNonBlocking(errChan, emitErr)
				return
			}

			// 3.6 对随口记回复执行统一后置持久化（Redis + outbox/DB）。
			s.persistChatAfterReply(ctx, userID, chatID, userMessage, quickReply, errChan)
			// 3.7 随口记链路同样异步生成会话标题（仅首次写入）。
			s.ensureConversationTitleAsync(userID, chatID)
			return
		}

		// 3.8 路由误判或 graph 判定非随口记时，回落普通聊天，保证“能聊”。
		progress.Emit("quick_note.fallback", "当前输入不是随口记请求，切换到普通对话。")
		s.runNormalChatFlow(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
	}()

	return outChan, errChan
}
