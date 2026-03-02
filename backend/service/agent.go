package service

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/agent"
	"github.com/LoveLosita/smartflow/backend/inits"
)

type AgentService struct {
	AIHub *inits.AIHub
}

func NewAgentService(aiHub *inits.AIHub) *AgentService {
	return &AgentService{
		AIHub: aiHub,
	}
}

func (s *AgentService) AgentChat(ctx context.Context, userMessage string) (<-chan string, <-chan error) {
	//1. 创建一个输出通道
	outChan := make(chan string, 5)
	errChan := make(chan error)
	//2. 启动一个 goroutine 来处理聊天逻辑
	go func() {
		defer close(outChan) // 确保在函数结束时关闭通道
		//3. 调用 StreamChat 函数进行流式聊天
		err := agent.StreamChat(ctx, s.AIHub.Worker, userMessage, outChan)
		if err != nil {
			errChan <- err
			return
		}
	}()
	return outChan, errChan
}
