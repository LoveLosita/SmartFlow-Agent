package model

import (
	"strings"

	newagentllm "github.com/LoveLosita/smartflow/backend/newAgent/llm"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
)

// AgentGraphRequest 描述一次 agent graph 运行的请求级输入。
//
// 职责边界：
// 1. 这里只放“当前这次请求”天然携带的轻量数据，例如用户本轮输入；
// 2. 不负责承载可持久化流程状态，流程状态仍归 AgentRuntimeState；
// 3. 不负责承载 LLM / emitter / store 等依赖，这些统一放进 AgentGraphDeps。
type AgentGraphRequest struct {
	UserInput     string
	ConfirmAction string // "accept" / "reject" / ""，仅 confirm 恢复场景由前端传入
}

// Normalize 统一清洗请求级输入中的字符串字段。
func (r *AgentGraphRequest) Normalize() {
	if r == nil {
		return
	}
	r.UserInput = strings.TrimSpace(r.UserInput)
	r.ConfirmAction = strings.TrimSpace(r.ConfirmAction)
}

// AgentGraphDeps 描述 graph/node 层运行时真正依赖的可插拔能力。
//
// 设计目的：
// 1. 让 graph 不再只拿到“裸状态”，而是能拿到上下文、模型和输出能力；
// 2. Chat/Plan/Execute/Deliver 允许分别挂不同 client，但也允许先复用同一个 client；
// 3. ChunkEmitter 统一承接阶段提示、正文、工具事件、确认请求等 SSE 输出。
type AgentGraphDeps struct {
	ChatClient    *newagentllm.Client
	PlanClient    *newagentllm.Client
	ExecuteClient *newagentllm.Client
	DeliverClient *newagentllm.Client
	ChunkEmitter  *newagentstream.ChunkEmitter
	StateStore    AgentStateStore
}

// EnsureChunkEmitter 保证 graph 运行时始终有一个可用的 chunk 发射器。
//
// 步骤说明：
// 1. 依赖为空时回退到 Noop emitter，避免骨架期因为没接前端而到处判空；
// 2. 这里只兜底“能安全调用”，不负责填充真实 request_id / model_name；
// 3. 后续 service 层一旦接上真实 emitter，会自然覆盖这里的空实现。
func (d *AgentGraphDeps) EnsureChunkEmitter() *newagentstream.ChunkEmitter {
	if d == nil {
		return newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", 0)
	}
	if d.ChunkEmitter == nil {
		d.ChunkEmitter = newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", 0)
	}
	return d.ChunkEmitter
}

// ResolveChatClient 返回 chat 阶段可用的模型客户端。
func (d *AgentGraphDeps) ResolveChatClient() *newagentllm.Client {
	if d == nil {
		return nil
	}
	return d.ChatClient
}

// ResolvePlanClient 返回 planning 阶段可用的模型客户端。
//
// 兜底策略：
// 1. 优先使用显式注入的 PlanClient；
// 2. 若未单独注入，则回退到 ChatClient；
// 3. 这样在骨架期可先用一套 client 跑通，再按需拆分 strategist / worker。
func (d *AgentGraphDeps) ResolvePlanClient() *newagentllm.Client {
	if d == nil {
		return nil
	}
	if d.PlanClient != nil {
		return d.PlanClient
	}
	return d.ChatClient
}

// ResolveExecuteClient 返回 execute 阶段可用的模型客户端。
func (d *AgentGraphDeps) ResolveExecuteClient() *newagentllm.Client {
	if d == nil {
		return nil
	}
	if d.ExecuteClient != nil {
		return d.ExecuteClient
	}
	if d.PlanClient != nil {
		return d.PlanClient
	}
	return d.ChatClient
}

// ResolveDeliverClient 返回 deliver 阶段可用的模型客户端。
func (d *AgentGraphDeps) ResolveDeliverClient() *newagentllm.Client {
	if d == nil {
		return nil
	}
	if d.DeliverClient != nil {
		return d.DeliverClient
	}
	if d.ExecuteClient != nil {
		return d.ExecuteClient
	}
	if d.PlanClient != nil {
		return d.PlanClient
	}
	return d.ChatClient
}

// AgentGraphRunInput 是执行 newAgent 通用 graph 所需的完整入口参数。
//
// 字段说明：
// 1. RuntimeState：可持久化流程状态与 pending interaction；
// 2. ConversationContext：本轮喂给模型的上下文材料；
// 3. Request：当前这次请求的轻量输入；
// 4. Deps：graph/node 层真正依赖的可插拔能力。
type AgentGraphRunInput struct {
	RuntimeState        *AgentRuntimeState
	ConversationContext *ConversationContext
	Request             AgentGraphRequest
	Deps                AgentGraphDeps
}

// AgentGraphState 是 graph 内部真正流转的运行态容器。
//
// 职责边界：
// 1. 负责把“流程状态 + 对话上下文 + 请求输入 + 运行依赖”收口到同一个对象；
// 2. 负责给 graph 分支和 node 提供最小必要的兜底访问方法；
// 3. 不负责持久化，不负责真正业务执行。
type AgentGraphState struct {
	RuntimeState        *AgentRuntimeState
	ConversationContext *ConversationContext
	Request             AgentGraphRequest
	Deps                AgentGraphDeps
}

// NewAgentGraphState 把入口参数整理成 graph 内部状态。
func NewAgentGraphState(input AgentGraphRunInput) *AgentGraphState {
	st := &AgentGraphState{
		RuntimeState:        input.RuntimeState,
		ConversationContext: input.ConversationContext,
		Request:             input.Request,
		Deps:                input.Deps,
	}
	st.Request.Normalize()
	st.EnsureRuntimeState()
	st.EnsureConversationContext()
	st.Deps.EnsureChunkEmitter()
	return st
}

// EnsureRuntimeState 保证 graph 内部始终持有一份可用的运行态。
func (s *AgentGraphState) EnsureRuntimeState() *AgentRuntimeState {
	if s == nil {
		return nil
	}
	if s.RuntimeState == nil {
		s.RuntimeState = NewAgentRuntimeState(nil)
	}
	s.RuntimeState.EnsureCommonState()
	return s.RuntimeState
}

// EnsureFlowState 返回可持久化的主流程状态。
func (s *AgentGraphState) EnsureFlowState() *CommonState {
	runtimeState := s.EnsureRuntimeState()
	if runtimeState == nil {
		return nil
	}
	return runtimeState.EnsureCommonState()
}

// EnsureConversationContext 保证 graph 内部始终持有一份可用的会话上下文。
func (s *AgentGraphState) EnsureConversationContext() *ConversationContext {
	if s == nil {
		return nil
	}
	if s.ConversationContext == nil {
		s.ConversationContext = NewConversationContext("")
	}
	return s.ConversationContext
}

// EnsureChunkEmitter 返回 graph 可安全调用的 chunk 发射器。
func (s *AgentGraphState) EnsureChunkEmitter() *newagentstream.ChunkEmitter {
	if s == nil {
		return newagentstream.NewChunkEmitter(newagentstream.NoopPayloadEmitter(), "", "", 0)
	}
	return s.Deps.EnsureChunkEmitter()
}
