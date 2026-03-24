package agentrouter

import (
	"context"
)

// Resolver 定义一级路由器能力。
type Resolver interface {
	Resolve(ctx context.Context, req *AgentRequest) (*RoutingDecision, error)
}

// SkillHandler 是某个 skill 的统一执行入口。
type SkillHandler func(ctx context.Context, req *AgentRequest) (*AgentResponse, error)

// AgentRequest 是 agent2 路由层可见的最小请求结构。
//
// 设计目的：
// 1. 让 router 层只依赖自己真正关心的字段；
// 2. 避免把整份 agentmodel 结构在迁移早期层层透传；
// 3. 后续若总入口还要追加别的字段，只需要在入口层做一次映射。
type AgentRequest struct {
	UserID         int
	ConversationID string
	UserMessage    string
	ModelName      string
	Extra          map[string]any
}

// AgentResponse 是路由分发器对 skill handler 的统一响应外壳。
type AgentResponse struct {
	Action Action
	Reply  string
	Meta   map[string]any
}
