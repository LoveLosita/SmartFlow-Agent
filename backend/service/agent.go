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
	agentCache    *dao.AgentCache
	asyncPipeline *AgentAsyncPipeline
}

func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, agentRedis *dao.AgentCache, asyncPipeline *AgentAsyncPipeline) *AgentService {
	return &AgentService{
		AIHub:         aiHub,
		repo:          repo,
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

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string) (<-chan string, <-chan error) {
	requestStart := time.Now()
	traceID := uuid.NewString()

	outChan := make(chan string, 5)
	errChan := make(chan error, 1)

	// 1) 规范会话 ID，选择模型。
	chatID = normalizeConversationID(chatID)
	selectedModel, resolvedModelName := s.pickChatModel(modelName)
	/*log.Printf("打点|请求开始|trace_id=%s|chat_id=%s|user_id=%d|model=%s|请求累计_ms=%d",
	traceID, chatID, userID, resolvedModelName, time.Since(requestStart).Milliseconds())*/

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

	// 3) 拉取并裁剪历史上下文。
	chatHistory, err := s.agentCache.GetHistory(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}

	cacheMiss := false
	if chatHistory == nil {
		cacheMiss = true
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel(resolvedModelName), chatID)
		if hisErr != nil {
			errChan <- hisErr
			close(outChan)
			close(errChan)
			return outChan, errChan
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
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
	}

	// 单请求主链路打点：开流前准备完成。
	/*log.Printf("打点|开流前准备完成|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d|history_len=%d|cache_miss=%t",
		traceID,
		chatID,
		time.Since(requestStart).Milliseconds(),
		time.Since(requestStart).Milliseconds(),
		len(chatHistory),
		cacheMiss,
	)*/

	// 4) 启动流式输出，回答完成后执行后置持久化。
	go func() {
		defer close(outChan)

		/*streamStart := time.Now()*/
		fullText, streamErr := agent.StreamChat(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan, traceID, chatID, requestStart)
		if streamErr != nil {
			pushErrNonBlocking(errChan, streamErr)
			return
		}
		/*log.Printf("打点|流式输出完成|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d|reply_chars=%d",
			traceID, chatID, time.Since(streamStart).Milliseconds(), time.Since(requestStart).Milliseconds(), len(fullText))

		postPersistStart := time.Now()

		stepStart := time.Now()*/
		if err = s.agentCache.PushMessage(ctx, chatID, &schema.Message{Role: schema.User, Content: userMessage}); err != nil {
			log.Printf("写入用户消息到 Redis 失败: %v", err)
		}
		/*log.Printf("打点|后置持久化_用户_写Redis|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d",
			traceID, chatID, time.Since(stepStart).Milliseconds(), time.Since(requestStart).Milliseconds())

		stepStart = time.Now()*/
		if err = s.saveChatHistoryReliable(ctx, model.ChatHistoryPersistPayload{
			UserID:         userID,
			ConversationID: chatID,
			Role:           "user",
			Message:        userMessage,
		}); err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
		}
		/*log.Printf("打点|后置持久化_用户_写持久化请求|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d",
			traceID, chatID, time.Since(stepStart).Milliseconds(), time.Since(requestStart).Milliseconds())

		stepStart = time.Now()
		if cacheErr := s.agentCache.PushMessage(context.Background(), chatID, &schema.Message{Role: schema.Assistant, Content: fullText}); cacheErr != nil {
			log.Printf("写入助手消息到 Redis 失败: %v", cacheErr)
		}
		log.Printf("打点|后置持久化_助手_写Redis|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d",
			traceID, chatID, time.Since(stepStart).Milliseconds(), time.Since(requestStart).Milliseconds())

		stepStart = time.Now()*/
		if saveErr := s.saveChatHistoryReliable(context.Background(), model.ChatHistoryPersistPayload{
			UserID:         userID,
			ConversationID: chatID,
			Role:           "assistant",
			Message:        fullText,
		}); saveErr != nil {
			pushErrNonBlocking(errChan, saveErr)
		}
		/*log.Printf("打点|后置持久化_助手_写持久化请求|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d",
			traceID, chatID, time.Since(stepStart).Milliseconds(), time.Since(requestStart).Milliseconds())

		log.Printf("打点|后置持久化完成|trace_id=%s|chat_id=%s|本步耗时_ms=%d|请求累计_ms=%d",
			traceID, chatID, time.Since(postPersistStart).Milliseconds(), time.Since(requestStart).Milliseconds())*/
	}()

	return outChan, errChan
}
