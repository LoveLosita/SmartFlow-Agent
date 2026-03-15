package quicknote

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/compose"
)

const (
	// 图节点：意图识别（含聚合规划与时间校验）
	quickNoteGraphNodeIntent = "quick_note_intent"
	// 图节点：优先级评估（或本地兜底）
	quickNoteGraphNodeRank = "quick_note_priority"
	// 图节点：持久化（调用写库工具）
	quickNoteGraphNodePersist = "quick_note_persist"
	// 图节点：退出（用于非随口记/校验失败分支）
	quickNoteGraphNodeExit = "quick_note_exit"
)

// QuickNoteGraphRunInput 是运行“随口记 graph”所需的输入依赖。
// 说明：
// 1) EmitStage 可选，用于把节点进度推送给外层（例如 SSE 状态块）；
// 2) 不传 EmitStage 时，图逻辑保持静默执行；
// 3) SkipIntentVerification=true 时，表示上游路由已信任 quick_note，可跳过二次意图判定。
type QuickNoteGraphRunInput struct {
	Model *ark.ChatModel
	State *QuickNoteState
	Deps  QuickNoteToolDeps

	SkipIntentVerification bool
	EmitStage              func(stage, detail string)
}

// RunQuickNoteGraph 执行“随口记”图编排。
// 该文件只负责“连线与分支”，节点内部逻辑全部下沉到 nodes.go。
func RunQuickNoteGraph(ctx context.Context, input QuickNoteGraphRunInput) (*QuickNoteState, error) {
	// 1. 启动前硬校验：模型、状态、依赖缺一不可。
	if input.Model == nil {
		return nil, errors.New("quick note graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("quick note graph: state is nil")
	}
	if err := input.Deps.validate(); err != nil {
		return nil, err
	}

	// 2. 统一封装阶段推送函数，避免各节点反复判空。
	emitStage := func(stage, detail string) {
		if input.EmitStage != nil {
			input.EmitStage(stage, detail)
		}
	}

	// 统一初始化“当前时间基准”，避免同一请求内相对时间口径漂移。
	// 2.1 若上游未设置 RequestNow，这里补齐。
	if input.State.RequestNow.IsZero() {
		input.State.RequestNow = quickNoteNowToMinute()
	}
	// 2.2 若上游未设置文本基准，这里按统一格式补齐。
	if strings.TrimSpace(input.State.RequestNowText) == "" {
		input.State.RequestNowText = formatQuickNoteTimeToMinute(input.State.RequestNow)
	}

	// 3. 构建工具包并取出写库工具。
	//    这样 graph 运行时只关心“调用工具”，不关心工具如何注册。
	toolBundle, err := BuildQuickNoteToolBundle(ctx, input.Deps)
	if err != nil {
		return nil, err
	}
	createTaskTool, err := getInvokableToolByName(toolBundle, ToolNameQuickNoteCreateTask)
	if err != nil {
		return nil, err
	}

	// 4. runner 负责把依赖收口，graph 只保留连线定义。
	runner := newQuickNoteRunner(input, createTaskTool, emitStage)

	// 5. 创建状态图容器：输入/输出类型都为 *QuickNoteState。
	graph := compose.NewGraph[*QuickNoteState, *QuickNoteState]()

	// 6. 注册节点（意图 -> 优先级 -> 持久化 -> 退出）。
	if err = graph.AddLambdaNode(quickNoteGraphNodeIntent, compose.InvokableLambda(runner.intentNode)); err != nil {
		return nil, err
	}

	if err = graph.AddLambdaNode(quickNoteGraphNodeRank, compose.InvokableLambda(runner.priorityNode)); err != nil {
		return nil, err
	}

	if err = graph.AddLambdaNode(quickNoteGraphNodePersist, compose.InvokableLambda(runner.persistNode)); err != nil {
		return nil, err
	}

	if err = graph.AddLambdaNode(quickNoteGraphNodeExit, compose.InvokableLambda(runner.exitNode)); err != nil {
		return nil, err
	}

	// 连线：START -> intent
	// 7. 所有请求统一先过 intent 节点，确保意图和时间校验在前。
	if err = graph.AddEdge(compose.START, quickNoteGraphNodeIntent); err != nil {
		return nil, err
	}

	// 分支：intent 后决定去 priority 还是 exit。
	// 8. 非随口记或时间非法时直接 exit，避免进入后续写库路径。
	if err = graph.AddBranch(quickNoteGraphNodeIntent, compose.NewGraphBranch(
		runner.nextAfterIntent,
		map[string]bool{
			quickNoteGraphNodeRank: true,
			quickNoteGraphNodeExit: true,
		},
	)); err != nil {
		return nil, err
	}

	// exit 直接结束。
	// 9. exit 是显式终点前节点，方便后续插入“统一收尾逻辑”。
	if err = graph.AddEdge(quickNoteGraphNodeExit, compose.END); err != nil {
		return nil, err
	}

	// priority -> persist。
	// 10. 通过优先级节点后，进入持久化节点。
	if err = graph.AddEdge(quickNoteGraphNodeRank, quickNoteGraphNodePersist); err != nil {
		return nil, err
	}

	// persist 后决定“重试 persist”还是结束。
	// 11. 重试策略由状态字段驱动，不在 graph 层写重试计数逻辑。
	if err = graph.AddBranch(quickNoteGraphNodePersist, compose.NewGraphBranch(
		runner.nextAfterPersist,
		map[string]bool{
			quickNoteGraphNodePersist: true,
			compose.END:               true,
		},
	)); err != nil {
		return nil, err
	}

	// 12. 运行步数上限：至少 12 步，并根据 MaxToolRetry 预留重试步数。
	//     防止异常分支导致无限循环。
	maxSteps := input.State.MaxToolRetry + 10
	if maxSteps < 12 {
		maxSteps = 12
	}

	// 13. 编译图得到可执行实例。
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("QuickNoteGraph"),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}

	// 14. 执行图并返回最终状态。
	return runnable.Invoke(ctx, input.State)
}
