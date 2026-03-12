package service

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
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
	asyncPipeline *AgentAsyncPipeline
}

func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, taskRepo *dao.TaskDAO, agentRedis *dao.AgentCache, asyncPipeline *AgentAsyncPipeline) *AgentService {
	return &AgentService{
		AIHub:         aiHub,
		repo:          repo,
		taskRepo:      taskRepo,
		agentCache:    agentRedis,
		asyncPipeline: asyncPipeline,
	}
}

func normalizeConversationID(chatID string) string {
	trimmed := strings.TrimSpace(chatID)
	if trimmed == "" {
		return uuid.NewString()
	}
	return trimmed
}

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
	if s.asyncPipeline == nil {
		return s.repo.SaveChatHistory(ctx, payload.UserID, payload.ConversationID, payload.Role, payload.Message)
	}
	return s.asyncPipeline.EnqueueChatHistoryPersist(ctx, payload)
}

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
	chatHistory, err := s.agentCache.GetHistory(ctx, chatID)
	if err != nil {
		pushErrNonBlocking(errChan, err)
		return
	}

	cacheMiss := false
	if chatHistory == nil {
		cacheMiss = true
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel(resolvedModelName), chatID)
		if hisErr != nil {
			pushErrNonBlocking(errChan, hisErr)
			return
		}
		chatHistory = conv.ToEinoMessages(histories)
	}

	historyBudget := pkg.HistoryTokenBudgetByModel(resolvedModelName, agent.SystemPrompt, userMessage)
	trimmedHistory, totalHistoryTokens, keptHistoryTokens, droppedCount := pkg.TrimHistoryByTokenBudget(chatHistory, historyBudget)
	chatHistory = trimmedHistory

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
		if err = s.agentCache.BackfillHistory(ctx, chatID, chatHistory); err != nil {
			pushErrNonBlocking(errChan, err)
			return
		}
	}

	fullText, streamErr := agent.StreamChat(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan, traceID, chatID, requestStart)
	if streamErr != nil {
		pushErrNonBlocking(errChan, streamErr)
		return
	}

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

	if saveErr := s.saveChatHistoryReliable(context.Background(), model.ChatHistoryPersistPayload{
		UserID:         userID,
		ConversationID: chatID,
		Role:           "assistant",
		Message:        fullText,
	}); saveErr != nil {
		pushErrNonBlocking(errChan, saveErr)
	}
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string) (<-chan string, <-chan error) {
	requestStart := time.Now()
	traceID := uuid.NewString()

	outChan := make(chan string, 8)
	errChan := make(chan error, 1)

	// 1) 规范会话 ID，选择模型。
	chatID = normalizeConversationID(chatID)
	selectedModel, resolvedModelName := s.pickChatModel(modelName)

	// 2) 确保会话存在（优先缓存，必要时回源 DB 并创建）。
	result, err := s.agentCache.GetConversationStatus(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	if !result {
		innerResult, ifErr := s.repo.IfChatExists(ctx, userID, chatID)
		if ifErr != nil {
			errChan <- ifErr
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
		if !innerResult {
			if _, err = s.repo.CreateNewChat(userID, chatID); err != nil {
				errChan <- err
				close(outChan)
				close(errChan)
				return outChan, errChan
			}
		}
		if err = s.agentCache.SetConversationStatus(ctx, chatID); err != nil {
			log.Printf("设置会话状态缓存失败 chat=%s: %v", chatID, err)
		}
	}

	// 3) 如果命中“任务安排关键词”，开启随口记阶段推送（伪装成 reasoning chunk）。
	if shouldEmitQuickNoteProgress(userMessage) {
		go func() {
			defer close(outChan)

			progress := newQuickNoteProgressEmitter(outChan, resolvedModelName, true)
			progress.Emit("request.accepted", "检测到任务安排请求，开始执行随口记流程。")

			quickHandled, quickState, quickErr := s.tryHandleQuickNoteWithGraph(
				ctx,
				selectedModel,
				userMessage,
				userID,
				chatID,
				traceID,
				progress.Emit,
			)
			if quickErr != nil {
				log.Printf("随口记 graph 执行失败，回退普通聊天 trace_id=%s chat_id=%s err=%v", traceID, chatID, quickErr)
			}

			if quickHandled {
				progress.Emit("quick_note.reply.polishing", "正在结合你的话题润色回复。")
				quickReply := buildQuickNoteFinalReply(ctx, selectedModel, userMessage, quickState)
				if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, quickReply); emitErr != nil {
					pushErrNonBlocking(errChan, emitErr)
					return
				}

				s.persistChatAfterReply(ctx, userID, chatID, userMessage, quickReply, errChan)
				return
			}

			progress.Emit("quick_note.fallback", "当前输入不是随口记请求，切换到普通对话。")
			s.runNormalChatFlow(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
		}()
		return outChan, errChan
	}

	// 4) 无阶段推送模式：保持原逻辑，先尝试随口记，不命中再走普通聊天。
	quickHandled, quickState, quickErr := s.tryHandleQuickNoteWithGraph(
		ctx,
		selectedModel,
		userMessage,
		userID,
		chatID,
		traceID,
		nil,
	)
	if quickErr != nil {
		log.Printf("随口记 graph 执行失败，回退普通聊天 trace_id=%s chat_id=%s err=%v", traceID, chatID, quickErr)
	}
	if quickHandled {
		go func() {
			defer close(outChan)

			quickReply := buildQuickNoteFinalReply(ctx, selectedModel, userMessage, quickState)
			if emitErr := emitSingleAssistantCompletion(outChan, resolvedModelName, quickReply); emitErr != nil {
				pushErrNonBlocking(errChan, emitErr)
				return
			}

			s.persistChatAfterReply(ctx, userID, chatID, userMessage, quickReply, errChan)
		}()
		return outChan, errChan
	}

	// 5) 普通流式聊天。
	go func() {
		defer close(outChan)
		s.runNormalChatFlow(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, userID, chatID, traceID, requestStart, outChan, errChan)
	}()

	return outChan, errChan
}
