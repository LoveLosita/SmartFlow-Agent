package agentgraph

import (
	"context"
	"errors"
	"strings"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"
	agentshared "github.com/LoveLosita/smartflow/backend/agent2/shared"
	"github.com/cloudwego/eino/compose"
)

const (
	// QuickNoteGraphName 是随口记图编排的稳定标识。
	//
	// 职责边界：
	// 1. 仅用于 graph 编译和链路标识，方便日志与排障统一定位。
	// 2. 不参与意图判断，也不承载任务写库的业务语义。
	QuickNoteGraphName = "quick_note"
)

// RunQuickNoteGraph 负责执行“随口记 -> 判断 -> 提取 -> 落库 -> 收口”的整条图链路。
//
// 职责边界：
// 1. 负责输入兜底、工具装配、节点注册与 graph 运行。
// 2. 不负责每个节点的具体业务决策，节点内部逻辑由 node 层实现。
// 3. 返回的 state 表示整条链路的最终状态，供上层继续拼接响应或写日志。
func RunQuickNoteGraph(ctx context.Context, input agentnode.QuickNoteGraphRunInput) (*agentmodel.QuickNoteState, error) {
	// 1. 先校验最基础依赖，避免图已经启动后才发现模型或状态为空。
	if input.Model == nil {
		return nil, errors.New("quick note graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("quick note graph: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return nil, err
	}

	// 2. 补齐当前请求时间，保证后续提示词、时间解析和落库字段都基于同一时刻。
	if input.State.RequestNow.IsZero() {
		input.State.RequestNow = agentshared.NowToMinute()
	}
	if strings.TrimSpace(input.State.RequestNowText) == "" {
		input.State.RequestNowText = agentshared.FormatMinute(input.State.RequestNow)
	}

	// 3. 图运行前统一准备工具与节点容器，避免节点内部重复做依赖解析。
	toolBundle, err := agentnode.BuildQuickNoteToolBundle(ctx, input.Deps)
	if err != nil {
		return nil, err
	}
	createTaskTool, err := agentnode.GetInvokableToolByName(toolBundle, agentnode.ToolNameQuickNoteCreateTask)
	if err != nil {
		return nil, err
	}

	nodes, err := agentnode.NewQuickNoteNodes(input, createTaskTool)
	if err != nil {
		return nil, err
	}

	// 4. 主链路保持“意图识别 -> 优先级评估 -> 持久化 -> 退出”，中间通过 branch 决定是否提前结束或重试写库。
	graph := compose.NewGraph[*agentmodel.QuickNoteState, *agentmodel.QuickNoteState]()
	if err = graph.AddLambdaNode(agentnode.QuickNoteGraphNodeIntent, compose.InvokableLambda(nodes.Intent)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.QuickNoteGraphNodeRank, compose.InvokableLambda(nodes.Priority)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.QuickNoteGraphNodePersist, compose.InvokableLambda(nodes.Persist)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.QuickNoteGraphNodeExit, compose.InvokableLambda(nodes.Exit)); err != nil {
		return nil, err
	}

	if err = graph.AddEdge(compose.START, agentnode.QuickNoteGraphNodeIntent); err != nil {
		return nil, err
	}
	if err = graph.AddBranch(agentnode.QuickNoteGraphNodeIntent, compose.NewGraphBranch(
		nodes.NextAfterIntent,
		map[string]bool{
			agentnode.QuickNoteGraphNodeRank: true,
			agentnode.QuickNoteGraphNodeExit: true,
		},
	)); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.QuickNoteGraphNodeExit, compose.END); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.QuickNoteGraphNodeRank, agentnode.QuickNoteGraphNodePersist); err != nil {
		return nil, err
	}
	if err = graph.AddBranch(agentnode.QuickNoteGraphNodePersist, compose.NewGraphBranch(
		nodes.NextAfterPersist,
		map[string]bool{
			agentnode.QuickNoteGraphNodePersist: true,
			compose.END:                         true,
		},
	)); err != nil {
		return nil, err
	}

	// 5. persist 节点允许有限次重试，因此最大步数要覆盖首次执行与重试回路。
	maxSteps := input.State.MaxToolRetry + 10
	if maxSteps < 12 {
		maxSteps = 12
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(QuickNoteGraphName),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}
	return runnable.Invoke(ctx, input.State)
}
