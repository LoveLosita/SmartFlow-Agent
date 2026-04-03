package graph

import (
	"context"
	"errors"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentnode "github.com/LoveLosita/smartflow/backend/newAgent/node"
	"github.com/cloudwego/eino/compose"
)

const (
	GraphName = "agent_loop"

	NodeChat      = "chat"
	NodePlan      = "plan"
	NodeConfirm   = "confirm"
	NodeExecute   = "execute"
	NodeInterrupt = "interrupt"
	NodeDeliver   = "deliver"
)

func RunAgentGraph(ctx context.Context, input newagentmodel.AgentGraphRunInput) (*newagentmodel.AgentGraphState, error) {
	state := newagentmodel.NewAgentGraphState(input)
	if state == nil {
		return nil, errors.New("agent graph: graph state is nil")
	}

	flowState := state.EnsureFlowState()
	if flowState == nil {
		return nil, errors.New("agent graph: flow state is nil")
	}

	nodes := newagentnode.NewAgentNodes()
	g := compose.NewGraph[*newagentmodel.AgentGraphState, *newagentmodel.AgentGraphState]()

	// --- 注册节点 ---
	if err := g.AddLambdaNode(NodeChat, compose.InvokableLambda(nodes.Chat)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodePlan, compose.InvokableLambda(nodes.Plan)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeConfirm, compose.InvokableLambda(nodes.Confirm)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeExecute, compose.InvokableLambda(nodes.Execute)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeInterrupt, compose.InvokableLambda(nodes.Interrupt)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeDeliver, compose.InvokableLambda(nodes.Deliver)); err != nil {
		return nil, err
	}

	// --- 连边 ---
	// 1. 所有请求统一先过 chat 入口，这样普通聊天、首次任务、恢复执行都走同一入口。
	// 2. chat 不再负责旧式多业务图路由，只负责决定后续应该进入哪个统一节点。
	if err := g.AddEdge(compose.START, NodeChat); err != nil {
		return nil, err
	}
	// Chat -> END(普通聊天) / Plan / Confirm / Execute / Deliver / Interrupt
	if err := g.AddBranch(NodeChat, compose.NewGraphBranch(
		branchAfterChat,
		map[string]bool{
			NodePlan:      true,
			NodeConfirm:   true,
			NodeExecute:   true,
			NodeDeliver:   true,
			NodeInterrupt: true,
			compose.END:   true,
		},
	)); err != nil {
		return nil, err
	}
	// Plan -> Plan(继续规划) / Confirm(规划完成) / Interrupt(需要追问用户)
	if err := g.AddBranch(NodePlan, compose.NewGraphBranch(
		branchAfterPlan,
		map[string]bool{
			NodePlan:      true,
			NodeConfirm:   true,
			NodeInterrupt: true,
		},
	)); err != nil {
		return nil, err
	}
	// Confirm -> Plan(用户拒绝或重规划) / Execute(确认后继续执行) / Interrupt(产出确认中断并等待外部回调)
	if err := g.AddBranch(NodeConfirm, compose.NewGraphBranch(
		branchAfterConfirm,
		map[string]bool{
			NodePlan:      true,
			NodeExecute:   true,
			NodeInterrupt: true,
		},
	)); err != nil {
		return nil, err
	}
	// Execute -> Execute(继续 ReAct) / Confirm(写操作待确认) / Deliver(完成) / Interrupt(需要追问用户)
	if err := g.AddBranch(NodeExecute, compose.NewGraphBranch(
		branchAfterExecute,
		map[string]bool{
			NodeExecute:   true,
			NodeConfirm:   true,
			NodeDeliver:   true,
			NodeInterrupt: true,
		},
	)); err != nil {
		return nil, err
	}
	// Interrupt -> END：当前连接必须在这里收口，等待用户输入或确认回调恢复。
	if err := g.AddEdge(NodeInterrupt, compose.END); err != nil {
		return nil, err
	}
	// Deliver -> END
	if err := g.AddEdge(NodeDeliver, compose.END); err != nil {
		return nil, err
	}

	// --- 编译运行 ---
	maxSteps := flowState.MaxRounds + 10
	runnable, err := g.Compile(ctx,
		compose.WithGraphName(GraphName),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}
	return runnable.Invoke(ctx, state)
}

// --- 分支函数 ---

func branchAfterChat(_ context.Context, st *newagentmodel.AgentGraphState) (string, error) {
	if st == nil {
		return compose.END, nil
	}
	if nextNode, interrupted := branchIfInterrupted(st); interrupted {
		return nextNode, nil
	}

	flowState := st.EnsureFlowState()
	if flowState == nil {
		return compose.END, nil
	}
	switch flowState.Phase {
	case newagentmodel.PhasePlanning:
		return NodePlan, nil
	case newagentmodel.PhaseWaitingConfirm:
		return NodeConfirm, nil
	case newagentmodel.PhaseExecuting:
		return NodeExecute, nil
	case newagentmodel.PhaseDone:
		return NodeDeliver, nil
	default:
		// 普通聊天场景，回复已在 chatNode 生成，当前请求可直接结束。
		return compose.END, nil
	}
}

func branchAfterPlan(_ context.Context, st *newagentmodel.AgentGraphState) (string, error) {
	if st == nil {
		return NodePlan, nil
	}
	if nextNode, interrupted := branchIfInterrupted(st); interrupted {
		return nextNode, nil
	}

	flowState := st.EnsureFlowState()
	if flowState == nil {
		return NodePlan, nil
	}
	if flowState.Phase == newagentmodel.PhaseWaitingConfirm {
		return NodeConfirm, nil
	}
	return NodePlan, nil
}

func branchAfterConfirm(_ context.Context, st *newagentmodel.AgentGraphState) (string, error) {
	if st == nil {
		return NodePlan, nil
	}
	if nextNode, interrupted := branchIfInterrupted(st); interrupted {
		return nextNode, nil
	}

	flowState := st.EnsureFlowState()
	if flowState == nil {
		return NodePlan, nil
	}
	switch flowState.Phase {
	case newagentmodel.PhaseExecuting:
		return NodeExecute, nil
	case newagentmodel.PhaseWaitingConfirm:
		// 1. confirm 节点产出确认请求后，当前连接必须进入 interrupt 收口。
		// 2. 真正的用户确认结果应由外部回调写回状态，再重新进入 graph。
		return NodeInterrupt, nil
	default:
		return NodePlan, nil
	}
}

func branchAfterExecute(_ context.Context, st *newagentmodel.AgentGraphState) (string, error) {
	if st == nil {
		return NodeExecute, nil
	}
	if nextNode, interrupted := branchIfInterrupted(st); interrupted {
		return nextNode, nil
	}

	flowState := st.EnsureFlowState()
	if flowState == nil {
		return NodeExecute, nil
	}
	if flowState.Phase == newagentmodel.PhaseWaitingConfirm {
		return NodeConfirm, nil
	}
	if flowState.Phase == newagentmodel.PhaseDone || flowState.Exhausted() {
		return NodeDeliver, nil
	}
	return NodeExecute, nil
}

func branchIfInterrupted(st *newagentmodel.AgentGraphState) (string, bool) {
	if st == nil {
		return "", false
	}
	runtimeState := st.EnsureRuntimeState()
	if runtimeState != nil && runtimeState.HasPendingInteraction() {
		return NodeInterrupt, true
	}
	return "", false
}
