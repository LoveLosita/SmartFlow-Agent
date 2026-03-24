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
	// QuickNoteGraphName 是“随口记”图编排的稳定标识。
	// 保留这个名字的目的：
	// 1. 让 compile 后的 graph 名称在日志、调试、可视化工具里有固定口径；
	// 2. 后续如果接入更多技能图，可以统一按技能名识别。
	QuickNoteGraphName = "quick_note"
)

// RunQuickNoteGraph 执行“随口记”图编排。
//
// 职责边界：
// 1. 这里只负责 graph 连线与运行时装配，不负责节点内部业务细节；
// 2. graph 层只挂 node 层对外暴露的方法，不再维护额外 runner 适配层；
// 3. 工具注册、时间基准补齐、compile 参数收口都在这里统一完成。
func RunQuickNoteGraph(ctx context.Context, input agentnode.QuickNoteGraphRunInput) (*agentmodel.QuickNoteState, error) {
	// 1. 启动前先做硬校验。
	// 1.1 model 为空时无法调模型，直接失败；
	// 1.2 state 为空时图无法承载共享上下文，也必须直接拦截；
	// 1.3 tool deps 不完整时，后续 persist 节点必然失败，因此这里提前收口。
	if input.Model == nil {
		return nil, errors.New("quick note graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("quick note graph: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return nil, err
	}

	// 2. 统一补齐本次请求的时间基准。
	// 2.1 RequestNow 只在整条 quicknote 链路入口确定一次，避免同一次请求里相对时间口径漂移；
	// 2.2 RequestNowText 是 prompt 注入用文本，缺失时也在这里统一补齐。
	if input.State.RequestNow.IsZero() {
		input.State.RequestNow = agentshared.NowToMinute()
	}
	if strings.TrimSpace(input.State.RequestNowText) == "" {
		input.State.RequestNowText = agentshared.FormatMinute(input.State.RequestNow)
	}

	// 3. 构建工具包并提取“创建任务”工具。
	// 3.1 graph 层只关心“拿到一个可执行工具”，不关心工具内部如何注册；
	// 3.2 失败时直接返回，避免把半残依赖继续交给 node 层。
	toolBundle, err := agentnode.BuildQuickNoteToolBundle(ctx, input.Deps)
	if err != nil {
		return nil, err
	}
	createTaskTool, err := agentnode.GetInvokableToolByName(toolBundle, agentnode.ToolNameQuickNoteCreateTask)
	if err != nil {
		return nil, err
	}

	// 4. 在 node 层创建节点容器。
	// 4.1 这一步就是“请求级依赖注入”的唯一收口点；
	// 4.2 graph 后续只认 `nodes.Intent / nodes.Priority / nodes.Persist` 这些方法，不再额外造 runner。
	nodes, err := agentnode.NewQuickNoteNodes(input, createTaskTool)
	if err != nil {
		return nil, err
	}

	// 5. 创建状态图容器，输入输出统一都是 *QuickNoteState。
	graph := compose.NewGraph[*agentmodel.QuickNoteState, *agentmodel.QuickNoteState]()

	// 6. 注册节点。
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

	// 7. 所有请求统一从 intent 节点开始。
	if err = graph.AddEdge(compose.START, agentnode.QuickNoteGraphNodeIntent); err != nil {
		return nil, err
	}

	// 8. intent 后分支：
	// 8.1 命中随口记且时间合法 -> priority；
	// 8.2 非随口记，或时间校验失败 -> exit。
	if err = graph.AddBranch(agentnode.QuickNoteGraphNodeIntent, compose.NewGraphBranch(
		nodes.NextAfterIntent,
		map[string]bool{
			agentnode.QuickNoteGraphNodeRank: true,
			agentnode.QuickNoteGraphNodeExit: true,
		},
	)); err != nil {
		return nil, err
	}

	// 9. 显式 exit 节点仍然保留。
	// 这样后续若要统一加日志、埋点、收尾逻辑，不需要再改 branch 结构。
	if err = graph.AddEdge(agentnode.QuickNoteGraphNodeExit, compose.END); err != nil {
		return nil, err
	}

	// 10. priority 后固定进入 persist。
	if err = graph.AddEdge(agentnode.QuickNoteGraphNodeRank, agentnode.QuickNoteGraphNodePersist); err != nil {
		return nil, err
	}

	// 11. persist 后分支：
	// 11.1 已成功写入 -> END；
	// 11.2 仍可重试 -> 回到 persist；
	// 11.3 重试耗尽 -> END，由 state 中的失败文案兜底。
	if err = graph.AddBranch(agentnode.QuickNoteGraphNodePersist, compose.NewGraphBranch(
		nodes.NextAfterPersist,
		map[string]bool{
			agentnode.QuickNoteGraphNodePersist: true,
			compose.END:                         true,
		},
	)); err != nil {
		return nil, err
	}

	// 12. 为 persist 重试预留运行步数余量，避免异常状态把图跑成死循环。
	maxSteps := input.State.MaxToolRetry + 10
	if maxSteps < 12 {
		maxSteps = 12
	}

	// 13. 编译并执行图。
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
