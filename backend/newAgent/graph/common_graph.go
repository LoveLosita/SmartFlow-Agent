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

	NodeChat       = "chat"
	NodePlan       = "plan"
	NodeConfirm    = "confirm"
	NodeRoughBuild = "rough_build"
	NodeExecute    = "execute"
	NodeOrderGuard = "order_guard"
	NodeInterrupt  = "interrupt"
	NodeDeliver    = "deliver"
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
	if err := g.AddLambdaNode(NodeRoughBuild, compose.InvokableLambda(nodes.RoughBuild)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeExecute, compose.InvokableLambda(nodes.Execute)); err != nil {
		return nil, err
	}
	if err := g.AddLambdaNode(NodeOrderGuard, compose.InvokableLambda(nodes.OrderGuard)); err != nil {
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
	// Chat -> END / Plan / Confirm / RoughBuild / Execute / Deliver / Interrupt
	if err := g.AddBranch(NodeChat, compose.NewGraphBranch(
		branchAfterChat,
		map[string]bool{
			NodePlan:       true,
			NodeConfirm:    true,
			NodeRoughBuild: true,
			NodeExecute:    true,
			NodeDeliver:    true,
			NodeInterrupt:  true,
			compose.END:    true,
		},
	)); err != nil {
		return nil, err
	}
	// Plan -> Plan(继续规划) / Confirm(规划完成) / RoughBuild(需粗排) / Execute(直接执行) / Deliver(完成) / Interrupt(需要追问用户)
	if err := g.AddBranch(NodePlan, compose.NewGraphBranch(
		branchAfterPlan,
		map[string]bool{
			NodePlan:       true,
			NodeConfirm:    true,
			NodeRoughBuild: true,
			NodeExecute:    true,
			NodeDeliver:    true,
			NodeInterrupt:  true,
		},
	)); err != nil {
		return nil, err
	}
	// Confirm -> Plan(用户拒绝或重规划) / RoughBuild(需粗排) / Execute(直接执行) / Deliver(完成) / Interrupt(等待用户确认)
	if err := g.AddBranch(NodeConfirm, compose.NewGraphBranch(
		branchAfterConfirm,
		map[string]bool{
			NodePlan:       true,
			NodeRoughBuild: true,
			NodeExecute:    true,
			NodeDeliver:    true,
			NodeInterrupt:  true,
		},
	)); err != nil {
		return nil, err
	}
	// RoughBuild -> Execute / OrderGuard / Deliver：
	// 1. 正常粗排完成后进入 execute 微调；
	// 2. 若粗排阶段 completed 且默认保持顺序，先走 order_guard 再交付；
	// 3. 若粗排阶段已写入正式终止结果（如粗排异常 abort），则直接进入 deliver 收口。
	if err := g.AddBranch(NodeRoughBuild, compose.NewGraphBranch(
		branchAfterRoughBuild,
		map[string]bool{
			NodeExecute:    true,
			NodeOrderGuard: true,
			NodeDeliver:    true,
			NodeInterrupt:  true,
		},
	)); err != nil {
		return nil, err
	}
	// Execute -> Execute(继续 ReAct) / Confirm(写操作待确认) / OrderGuard(顺序守卫) / Deliver(完成) / Interrupt(需要追问用户)
	if err := g.AddBranch(NodeExecute, compose.NewGraphBranch(
		branchAfterExecute,
		map[string]bool{
			NodeExecute:    true,
			NodeConfirm:    true,
			NodeOrderGuard: true,
			NodeDeliver:    true,
			NodeInterrupt:  true,
		},
	)); err != nil {
		return nil, err
	}
	// OrderGuard -> Deliver：顺序守卫只做校验，最终都由 Deliver 统一收口。
	if err := g.AddEdge(NodeOrderGuard, NodeDeliver); err != nil {
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
	case newagentmodel.PhaseChatting:
		// 简单任务直接回复 / 深度回答完成，回复已在 Chat 节点生成。
		return compose.END, nil
	case newagentmodel.PhasePlanning:
		return NodePlan, nil
	case newagentmodel.PhaseWaitingConfirm:
		return NodeConfirm, nil
	case newagentmodel.PhaseExecuting:
		if flowState.NeedsRoughBuild && st.Deps.RoughBuildFunc != nil {
			return NodeRoughBuild, nil
		}
		return NodeExecute, nil
	case newagentmodel.PhaseDone:
		return NodeDeliver, nil
	default:
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
	if flowState.Phase == newagentmodel.PhaseExecuting {
		if flowState.NeedsRoughBuild && st.Deps.RoughBuildFunc != nil {
			return NodeRoughBuild, nil
		}
		return NodeExecute, nil
	}
	if flowState.Phase == newagentmodel.PhaseDone {
		return NodeDeliver, nil
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
		// 若 Plan 节点标记了需要粗排且 RoughBuildFunc 已注入，走粗排节点。
		if flowState.NeedsRoughBuild && st.Deps.RoughBuildFunc != nil {
			return NodeRoughBuild, nil
		}
		return NodeExecute, nil
	case newagentmodel.PhaseWaitingConfirm:
		// confirm 节点产出确认请求后，当前连接必须进入 interrupt 收口。
		// 真正的用户确认结果应由外部回调写回状态，再重新进入 graph。
		return NodeInterrupt, nil
	case newagentmodel.PhaseDone:
		return NodeDeliver, nil
	default:
		return NodePlan, nil
	}
}

func branchAfterRoughBuild(_ context.Context, st *newagentmodel.AgentGraphState) (string, error) {
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
	if flowState.Phase == newagentmodel.PhaseDone {
		if flowState.TerminalStatus() == newagentmodel.FlowTerminalStatusCompleted && !flowState.AllowReorder {
			return NodeOrderGuard, nil
		}
		return NodeDeliver, nil
	}
	return NodeExecute, nil
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
	// 1. 这里只围绕“是否已经写入正式终止结果”做路由，避免把“刚好用完最后一轮预算”
	//    误判成已经 exhausted 收口；
	// 2. 真正的 exhausted 语义应由下一次 Execute 入口在 NextRound() 失败时统一写入，
	//    这样 rough_build / execute / deliver 才都围绕同一份 terminal outcome 工作；
	// 3. 若此处直接按 RoundUsed>=MaxRounds 跳 Deliver，会绕过 Execute 内的 Exhaust 写入，
	//    导致 deliver 收口和后续预览落盘语义不一致。
	if flowState.Phase == newagentmodel.PhaseDone {
		if flowState.TerminalStatus() == newagentmodel.FlowTerminalStatusCompleted && !flowState.AllowReorder && flowState.HasScheduleWriteOps {
			return NodeOrderGuard, nil
		}
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
