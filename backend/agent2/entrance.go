package agent2

import (
	"context"
	"errors"

	agentrouter "github.com/LoveLosita/smartflow/backend/agent2/router"
)

// Service 是 agent2 模块的总入口。
//
// 职责边界：
// 1. 负责接住一次完整的 Agent 请求，并把请求交给统一路由器分流；
// 2. 负责维护“路由器 + 各 skill handler”的装配关系；
// 3. 不负责具体 skill 的 graph 连线，也不负责节点内部业务实现。
type Service struct {
	dispatcher *agentrouter.Dispatcher
}

// NewService 创建 agent2 总入口服务。
func NewService(resolver agentrouter.Resolver) *Service {
	return &Service{
		dispatcher: agentrouter.NewDispatcher(resolver),
	}
}

// RegisterHandler 注册某个 skill 的执行入口。
func (s *Service) RegisterHandler(action agentrouter.Action, handler agentrouter.SkillHandler) error {
	if s == nil || s.dispatcher == nil {
		return errors.New("agent2 service is not initialized")
	}
	return s.dispatcher.Register(action, handler)
}

// Handle 是 agent2 的统一处理入口。
func (s *Service) Handle(ctx context.Context, req *agentrouter.AgentRequest) (*agentrouter.AgentResponse, error) {
	if s == nil || s.dispatcher == nil {
		return nil, errors.New("agent2 service is not initialized")
	}
	return s.dispatcher.Dispatch(ctx, req)
}
