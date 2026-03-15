package quicknote

import (
	"context"

	"github.com/cloudwego/eino/components/tool"
)

// quickNoteRunner 是“单次图运行”的请求级依赖容器。
// 设计目标：
// 1) 把节点运行所需依赖（input/tool/emit）就近收口；
// 2) 让 graph.go 只保留“节点连线”和“方法引用”，提升可读性；
// 3) 避免在 graph.go 里重复出现内联闭包和参数透传。
type quickNoteRunner struct {
	input          QuickNoteGraphRunInput
	createTaskTool tool.InvokableTool
	emitStage      func(stage, detail string)
}

// newQuickNoteRunner 构造请求级 runner。
// 说明：runner 生命周期仅限一次 graph invoke，不做跨请求复用。
func newQuickNoteRunner(input QuickNoteGraphRunInput, createTaskTool tool.InvokableTool, emitStage func(stage, detail string)) *quickNoteRunner {
	return &quickNoteRunner{
		input:          input,
		createTaskTool: createTaskTool,
		emitStage:      emitStage,
	}
}

func (r *quickNoteRunner) intentNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	// 方法引用适配层：把 runner 内部依赖透传到纯函数节点实现。
	return runQuickNoteIntentNode(ctx, st, r.input, r.emitStage)
}

func (r *quickNoteRunner) priorityNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	// 方法引用适配层：让 graph.go 保持“只连线，不写业务细节”。
	return runQuickNotePriorityNode(ctx, st, r.input, r.emitStage)
}

func (r *quickNoteRunner) persistNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	// 这里注入 createTaskTool，是为了让 persist 节点不直接依赖外部容器对象。
	return runQuickNotePersistNodeInternal(ctx, st, r.createTaskTool, r.input, r.emitStage)
}

func (r *quickNoteRunner) nextAfterIntent(ctx context.Context, st *QuickNoteState) (string, error) {
	// 当前分支决策是纯状态函数，不依赖 context，保留参数仅为适配 GraphBranch 签名。
	_ = ctx
	return selectQuickNoteNextAfterIntent(st), nil
}

func (r *quickNoteRunner) nextAfterPersist(ctx context.Context, st *QuickNoteState) (string, error) {
	// 当前分支决策是纯状态函数，不依赖 context，保留参数仅为适配 GraphBranch 签名。
	_ = ctx
	return selectQuickNoteNextAfterPersist(st), nil
}

func (r *quickNoteRunner) exitNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	// exit 节点不做任何业务逻辑，仅把当前状态原样透传到 END。
	_ = ctx
	return st, nil
}
