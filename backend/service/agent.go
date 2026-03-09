package service

import (
	"context"
	"log"
	"strings"

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

// saveChatHistoryReliable 是聊天记录持久化的统一入口：
// 1) 启用 outbox + Kafka 时，走异步可靠链路；
// 2) 未启用时，退化为同步写数据库。
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
		log.Printf("error channel is full, drop error: %v", err)
	}
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string) (<-chan string, <-chan error) {
	// 1) 准备输出通道。
	outChan := make(chan string, 5)
	errChan := make(chan error, 1)

	// 2) 规范会话 ID 并选择模型。
	chatID = normalizeConversationID(chatID)
	selectedModel, resolvedModelName := s.pickChatModel(modelName)

	// 3) 确保会话存在：先查缓存，再回源数据库，必要时创建新会话。
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
			log.Printf("failed to set conversation status cache for %s: %v", chatID, err)
		}
	}

	// 4) 组装历史上下文：先读缓存，缓存未命中再读数据库。
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

	// 5) 基于 token 预算裁剪历史，避免请求超长。
	historyBudget := pkg.HistoryTokenBudgetByModel(resolvedModelName, agent.SystemPrompt, userMessage)
	trimmedHistory, totalHistoryTokens, keptHistoryTokens, droppedCount := pkg.TrimHistoryByTokenBudget(chatHistory, historyBudget)
	chatHistory = trimmedHistory

	// 6) 根据裁剪结果调整 Redis 会话窗口，控制缓存体积。
	targetWindow := pkg.CalcSessionWindowSize(len(chatHistory))
	if err = s.agentCache.SetSessionWindowSize(ctx, chatID, targetWindow); err != nil {
		log.Printf("failed to set history window for %s: %v", chatID, err)
	}
	if err = s.agentCache.EnforceHistoryWindow(ctx, chatID); err != nil {
		log.Printf("failed to enforce history window for %s: %v", chatID, err)
	}

	if droppedCount > 0 {
		log.Printf("agent history trimmed: chat=%s total_tokens=%d kept_tokens=%d dropped=%d budget=%d target_window=%d",
			chatID, totalHistoryTokens, keptHistoryTokens, droppedCount, historyBudget, targetWindow)
	}

	// 缓存未命中时，把“裁剪后的历史”回填 Redis。
	if cacheMiss {
		if err = s.agentCache.BackfillHistory(ctx, chatID, chatHistory); err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
	}

	// 7) 先同步写 Redis，再把数据库持久化交给 outbox 可靠链路。
	if err = s.agentCache.PushMessage(ctx, chatID, &schema.Message{Role: schema.User, Content: userMessage}); err != nil {
		log.Printf("failed to push user message into redis history: %v", err)
	}
	if err = s.saveChatHistoryReliable(ctx, model.ChatHistoryPersistPayload{
		UserID:         userID,
		ConversationID: chatID,
		Role:           "user",
		Message:        userMessage,
	}); err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}

	// 8) 启动流式对话。
	go func() {
		defer close(outChan)

		fullText, streamErr := agent.StreamChat(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan)
		if streamErr != nil {
			pushErrNonBlocking(errChan, streamErr)
			return
		}

		// 9) 助手回答完成后，重复同样流程：先写 Redis，再异步持久化。
		if cacheErr := s.agentCache.PushMessage(context.Background(), chatID, &schema.Message{Role: schema.Assistant, Content: fullText}); cacheErr != nil {
			log.Printf("failed to push assistant message into redis history: %v", cacheErr)
		}
		if saveErr := s.saveChatHistoryReliable(context.Background(), model.ChatHistoryPersistPayload{
			UserID:         userID,
			ConversationID: chatID,
			Role:           "assistant",
			Message:        fullText,
		}); saveErr != nil {
			pushErrNonBlocking(errChan, saveErr)
		}
	}()

	return outChan, errChan
}
