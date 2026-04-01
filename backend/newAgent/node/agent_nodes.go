package newagentnode

import (
	"context"
	"errors"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

// AgentNodes 是 newAgent 通用图的节点容器。
//
// 职责边界：
// 1. 负责把 node 层真正实现的方法统一暴露给 graph 注册；
// 2. 负责收口“graph 只编排、node 真执行”的结构约束；
// 3. 当前先迁移 Plan，其他节点后续按同样模式逐步下沉。
type AgentNodes struct{}

// NewAgentNodes 创建通用节点容器。
func NewAgentNodes() *AgentNodes {
	return &AgentNodes{}
}

// Chat 是聊天入口的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的入口逻辑仍由 RunChatNode 负责；
// 3. 这样 graph 层后续只需挂 n.Chat，而不再自己维护占位 chatNode。
func (n *AgentNodes) Chat(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("chat node: state is nil")
	}

	if err := RunChatNode(
		ctx,
		ChatNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			UserInput:           st.Request.UserInput,
			ConfirmAction:       st.Request.ConfirmAction,
			Client:              st.Deps.ResolveChatClient(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
		},
	); err != nil {
		return nil, err
	}
	return st, nil
}

// Plan 是规划阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的单轮规划逻辑仍由 RunPlanNode 负责；
// 3. 这样 graph 层后续只需挂 n.Plan，而不再自己维护占位 planNode。
func (n *AgentNodes) Plan(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("plan node: state is nil")
	}

	if err := RunPlanNode(
		ctx,
		PlanNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			UserInput:           st.Request.UserInput,
			Client:              st.Deps.ResolvePlanClient(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
			ResumeNode:          "plan",
		},
	); err != nil {
		return nil, err
	}
	return st, nil
}

// Execute 是执行阶段的正式节点方法。
//
// 职责边界：
// 1. 这里只做 graph -> node 的参数转接；
// 2. 真正的单轮执行逻辑仍由 RunExecuteNode 负责；
// 3. 这样 graph 层后续只需挂 n.Execute，而不再自己维护占位 executeNode。
//
// 设计原则：
// 1. LLM 主导：LLM 自己判断 done_when 是否满足，自己决定何时推进/完成；
// 2. 后端兜底：只做资源控制、安全兜底、证据记录；
// 3. 不做硬校验：后端不质疑 LLM 的 advance/complete 决策。
func (n *AgentNodes) Execute(ctx context.Context, st *newagentmodel.AgentGraphState) (*newagentmodel.AgentGraphState, error) {
	if st == nil {
		return nil, errors.New("execute node: state is nil")
	}

	if err := RunExecuteNode(
		ctx,
		ExecuteNodeInput{
			RuntimeState:        st.EnsureRuntimeState(),
			ConversationContext: st.EnsureConversationContext(),
			UserInput:           st.Request.UserInput,
			Client:              st.Deps.ResolveExecuteClient(),
			ChunkEmitter:        st.EnsureChunkEmitter(),
			ResumeNode:          "execute",
		},
	); err != nil {
		return nil, err
	}
	return st, nil
}
