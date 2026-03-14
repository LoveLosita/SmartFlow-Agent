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
	if input.Model == nil {
		return nil, errors.New("quick note graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("quick note graph: state is nil")
	}
	if err := input.Deps.validate(); err != nil {
		return nil, err
	}

	emitStage := func(stage, detail string) {
		if input.EmitStage != nil {
			input.EmitStage(stage, detail)
		}
	}

	// 统一初始化“当前时间基准”，避免同一请求内相对时间口径漂移。
	if input.State.RequestNow.IsZero() {
		input.State.RequestNow = quickNoteNowToMinute()
	}
	if strings.TrimSpace(input.State.RequestNowText) == "" {
		input.State.RequestNowText = formatQuickNoteTimeToMinute(input.State.RequestNow)
	}

	toolBundle, err := BuildQuickNoteToolBundle(ctx, input.Deps)
	if err != nil {
		return nil, err
	}
	createTaskTool, err := getInvokableToolByName(toolBundle, ToolNameQuickNoteCreateTask)
	if err != nil {
		return nil, err
	}
	runner := newQuickNoteRunner(input, createTaskTool, emitStage)

	graph := compose.NewGraph[*QuickNoteState, *QuickNoteState]()

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
	if err = graph.AddEdge(compose.START, quickNoteGraphNodeIntent); err != nil {
		return nil, err
	}

	// 分支：intent 后决定去 priority 还是 exit。
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
	if err = graph.AddEdge(quickNoteGraphNodeExit, compose.END); err != nil {
		return nil, err
	}

	// priority -> persist。
	if err = graph.AddEdge(quickNoteGraphNodeRank, quickNoteGraphNodePersist); err != nil {
		return nil, err
	}

	// persist 后决定“重试 persist”还是结束。
	if err = graph.AddBranch(quickNoteGraphNodePersist, compose.NewGraphBranch(
		runner.nextAfterPersist,
		map[string]bool{
			quickNoteGraphNodePersist: true,
			compose.END:               true,
		},
	)); err != nil {
		return nil, err
	}

	maxSteps := input.State.MaxToolRetry + 10
	if maxSteps < 12 {
		maxSteps = 12
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("QuickNoteGraph"),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}

	return runnable.Invoke(ctx, input.State)
}
