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

func newQuickNoteRunner(input QuickNoteGraphRunInput, createTaskTool tool.InvokableTool, emitStage func(stage, detail string)) *quickNoteRunner {
	return &quickNoteRunner{
		input:          input,
		createTaskTool: createTaskTool,
		emitStage:      emitStage,
	}
}

func (r *quickNoteRunner) intentNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	return runQuickNoteIntentNode(ctx, st, r.input, r.emitStage)
}

func (r *quickNoteRunner) priorityNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	return runQuickNotePriorityNode(ctx, st, r.input, r.emitStage)
}

func (r *quickNoteRunner) persistNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	return runQuickNotePersistNodeInternal(ctx, st, r.createTaskTool, r.input, r.emitStage)
}

func (r *quickNoteRunner) nextAfterIntent(ctx context.Context, st *QuickNoteState) (string, error) {
	_ = ctx
	return selectQuickNoteNextAfterIntent(st), nil
}

func (r *quickNoteRunner) nextAfterPersist(ctx context.Context, st *QuickNoteState) (string, error) {
	_ = ctx
	return selectQuickNoteNextAfterPersist(st), nil
}

func (r *quickNoteRunner) exitNode(ctx context.Context, st *QuickNoteState) (*QuickNoteState, error) {
	_ = ctx
	return st, nil
}
