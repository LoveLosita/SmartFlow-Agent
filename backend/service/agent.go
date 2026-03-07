package service

import (
	"context"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/agent"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/inits"
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

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, ifThinking bool, userID int, chatID string) (<-chan string, <-chan error) {
	//1. 创建一个输出通道
	outChan := make(chan string, 5)
	errChan := make(chan error, 1)
	//补充：会话 ID 兜底，避免上层漏传
	chatID = normalizeConversationID(chatID)

	//2. 先确保这个会话存在（如果不存在就创建一个新的）
	//先看看缓存里面有没有这个会话
	result, err := s.agentCache.GetConversationStatus(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	//如果缓存里面没有，就去查库
	if !result {
		innerResult, err := s.repo.IfChatExists(ctx, userID, chatID)
		if err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
		if !innerResult {
			//如果会话不存在，先创建一个新的会话
			if _, err = s.repo.CreateNewChat(userID, chatID); err != nil {
				errChan <- err
				close(outChan)
				close(errChan)
				return outChan, errChan
			}
		}
		//补充：把“会话存在”状态回写缓存，后续请求可直接命中
		if err = s.agentCache.SetConversationStatus(ctx, chatID); err != nil {
			//缓存回写失败不影响主流程
			log.Printf("failed to set conversation status cache for %s: %v", chatID, err)
		}
	}
	//能走到这里，要么缓存里有这个会话，要么数据库里有这个会话了
	//4. 提取出历史消息，构建上下文
	//先尝试从缓存里拿历史消息
	chatHistory, err := s.agentCache.GetHistory(ctx, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	//如果缓存里没有历史消息，就从数据库里拿
	if chatHistory == nil {
		//先从数据库拿到历史消息
		histories, err := s.repo.GetUserChatHistories(ctx, userID, 20, chatID)
		if err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
		//再转换成 Eino 的消息格式
		chatHistory = conv.ToEinoMessages(histories)
		//把历史消息放到缓存里，方便下次直接拿
		if err = s.agentCache.BackfillHistory(ctx, chatID, chatHistory); err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
	}
	//3. 将用户消息异步落缓存和库
	go func() {
		//这里先不管落库成功与否了，毕竟不想因为落库失败而影响用户的聊天体验
		bg := context.Background()
		_ = s.agentCache.PushMessage(bg, chatID, &schema.Message{
			Role:    schema.User,
			Content: userMessage,
		})
		_ = s.repo.SaveChatHistory(bg, userID, chatID, "user", userMessage)
	}()

	//5. 启动一个 goroutine 来处理聊天逻辑
	go func() {
		defer close(outChan) // 确保在函数结束时关闭通道
		defer close(errChan)
		//3. 调用 StreamChat 函数进行流式聊天
		fullText, err := agent.StreamChat(ctx, s.AIHub.Worker, userMessage, ifThinking, chatHistory, outChan)
		if err != nil {
			errChan <- err
			return
		}
		//4. 将 AI 的回复异步落缓存和库
		go func() {
			bg := context.Background()
			_ = s.agentCache.PushMessage(bg, chatID, &schema.Message{
				Role:    schema.Assistant,
				Content: fullText,
			})
			if saveErr := s.repo.SaveChatHistory(bg, userID, chatID, "assistant", fullText); saveErr != nil {
				log.Printf("failed to save chat history to database: %v", saveErr)
				return
			}
		}()
	}()
	return outChan, errChan
}
