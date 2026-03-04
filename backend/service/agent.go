package service

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/agent"
	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/cloudwego/eino/schema"
)

type AgentService struct {
	AIHub *inits.AIHub
	repo  *dao.AgentDAO
}

func NewAgentService(aiHub *inits.AIHub, repo *dao.AgentDAO) *AgentService {
	return &AgentService{
		AIHub: aiHub,
		repo:  repo,
	}
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string, userID int, chatID string) (<-chan string, <-chan error) {
	//1. 创建一个输出通道
	outChan := make(chan string, 5)
	errChan := make(chan error, 1)
	//2. 先确保这个会话存在（如果不存在就创建一个新的）
	result, err := s.repo.IfChatExists(ctx, userID, chatID)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	var chatHistory []*schema.Message
	if result {
		//4. 提取出历史消息，构建上下文
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
	} else {
		//如果会话不存在，先创建一个新的会话
		_, err := s.repo.CreateNewChat(userID, chatID)
		if err != nil {
			errChan <- err
			close(outChan)
			close(errChan)
			return outChan, errChan
		}
	}
	//3. 将用户消息落库
	err = s.repo.SaveChatHistory(ctx, userID, chatID, "user", userMessage)
	if err != nil {
		errChan <- err
		close(outChan)
		close(errChan)
		return outChan, errChan
	}
	//5. 启动一个 goroutine 来处理聊天逻辑
	go func() {
		defer close(outChan) // 确保在函数结束时关闭通道
		//3. 调用 StreamChat 函数进行流式聊天
		fullText, err := agent.StreamChat(ctx, s.AIHub.Worker, userMessage, chatHistory, outChan)
		if err != nil {
			errChan <- err
			return
		}
		err = s.repo.SaveChatHistory(ctx, userID, chatID, "assistant", fullText)
		if err != nil {
			errChan <- err
			return
		}
	}()
	return outChan, errChan
}

func (s *AgentService) CreateNewChat(userID int, chatID string) (int64, error) {
	return s.repo.CreateNewChat(userID, chatID)
}
