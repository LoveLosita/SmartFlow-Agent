package agentrouter

import (
	"context"
	"errors"
	"fmt"
)

// Dispatcher 是 agent2 的统一分发器。
type Dispatcher struct {
	resolver Resolver
	handlers map[Action]SkillHandler
}

// NewDispatcher 创建统一分发器。
func NewDispatcher(resolver Resolver) *Dispatcher {
	return &Dispatcher{
		resolver: resolver,
		handlers: make(map[Action]SkillHandler),
	}
}

// Register 注册某个动作的处理函数。
func (d *Dispatcher) Register(action Action, handler SkillHandler) error {
	if d == nil {
		return errors.New("dispatcher is nil")
	}
	if action == "" {
		return errors.New("route action is empty")
	}
	if handler == nil {
		return fmt.Errorf("handler for action %s is nil", action)
	}
	if _, exists := d.handlers[action]; exists {
		return fmt.Errorf("handler for action %s already registered", action)
	}
	d.handlers[action] = handler
	return nil
}

// Dispatch 执行“分流 -> skill handler”完整入口。
func (d *Dispatcher) Dispatch(ctx context.Context, req *AgentRequest) (*AgentResponse, error) {
	if d == nil || d.resolver == nil {
		return nil, errors.New("route dispatcher is not ready")
	}
	if req == nil {
		return nil, errors.New("agent request is nil")
	}

	// 1. 调用目的：统一先走一级路由，让入口层只关心“请求来了”，
	//    不需要提前知道这是普通聊天、随口记、任务查询还是后续排程。
	decision, err := d.resolver.Resolve(ctx, req)
	if err != nil {
		return nil, err
	}
	if decision == nil {
		return nil, errors.New("route decision is nil")
	}

	// 2. 路由结果出来后，只根据 action 查找对应 handler。
	//    这里故意不做 skill 级 fallback，避免路由层和 skill 内部职责再次缠在一起。
	handler, exists := d.handlers[decision.Action]
	if !exists {
		return nil, fmt.Errorf("no handler registered for action %s", decision.Action)
	}
	return handler(ctx, req)
}
