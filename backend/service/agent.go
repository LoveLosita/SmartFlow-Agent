package service

import (
	"context"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/agent"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

type AgentService struct {
	AIHub      *inits.AIHub
	repo       *dao.AgentDAO
	agentCache *dao.AgentCache
}

func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO, agentRedis *dao.AgentCache) *AgentService {
	return &AgentService{
		AIHub:      aiHub,
		repo:       repo,
		agentCache: agentRedis,
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
	model := strings.TrimSpace(requestModel)
	if strings.EqualFold(model, "strategist") {
		return s.AIHub.Strategist, "strategist"
	}
	return s.AIHub.Worker, "worker"
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, modelName string, userID int, chatID string) (<-chan string, <-chan error) {
	// 1) 准备输出通道
	outChan := make(chan string, 5)
	errChan := make(chan error, 1)

	// 2) 规范化会话并选择模型
	chatID = normalizeConversationID(chatID)
	selectedModel, resolvedModelName := s.pickChatModel(modelName)

	// 3) 确保会话存在
	result, err := s.agentCache.GetConversationStatus(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	if !result {
		innerResult, err := s.repo.IfChatExists(ctx, userID, chatID)
		if err != nil {
			errChan <- err
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

	// 4) 构建历史上下文
	chatHistory, err := s.agentCache.GetHistory(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	if chatHistory == nil {
		histories, err := s.repo.GetUserChatHistories(ctx, userID, 20, chatID)
		if err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
		chatHistory = conv.ToEinoMessages(histories)
		if err = s.agentCache.BackfillHistory(ctx, chatID, chatHistory); err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
	}

	// 5) 异步落用户消息
	go func() {
		bg := context.Background()
		_ = s.agentCache.PushMessage(bg, chatID, &schema.Message{
			Role:    schema.User,
			Content: userMessage,
		})
		_ = s.repo.SaveChatHistory(bg, userID, chatID, "user", userMessage)
	}()

	// 6) 流式输出模型回复
	go func() {
		defer close(outChan)

		fullText, err := agent.StreamChat(ctx, selectedModel, resolvedModelName, userMessage, ifThinking, chatHistory, outChan)
		if err != nil {
			errChan <- err
			return
		}

		// 7) 异步落助手消息
		go func() {
			bg := context.Background()
			_ = s.agentCache.PushMessage(bg, chatID, &schema.Message{
				Role:    schema.Assistant,
				Content: fullText,
			})
			if saveErr := s.repo.SaveChatHistory(bg, userID, chatID, "assistant", fullText); saveErr != nil {
				log.Printf("failed to save chat history to database: %v", saveErr)
			}
		}()
	}()

	return outChan, errChan
}
