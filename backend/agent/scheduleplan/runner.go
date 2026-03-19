package scheduleplan

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

// schedulePlanRunner 是"单次图运行"的请求级依赖容器。
//
// 设计目标：
// 1) 把节点运行所需依赖（model/deps/emit/extra/history）就近收口；
// 2) 让 graph.go 只保留"节点连线"和"方法引用"，提升可读性；
// 3) 避免在 graph.go 里重复出现内联闭包和参数透传。
type schedulePlanRunner struct {
	chatModel   *ark.ChatModel
	deps        SchedulePlanToolDeps
	emitStage   func(stage, detail string)
	userMessage string
	extra       map[string]any
	chatHistory []*schema.Message
	// ── ReAct 精排所需 ──
	outChan   chan<- string // SSE 流式输出通道，用于推送 reasoning_content
	modelName string        // 模型名称，用于构造 OpenAI 兼容 chunk
}

// newSchedulePlanRunner 构造请求级 runner。
// 生命周期仅限一次 graph invoke，不做跨请求复用。
func newSchedulePlanRunner(
	chatModel *ark.ChatModel,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
	userMessage string,
	extra map[string]any,
	chatHistory []*schema.Message,
	outChan chan<- string,
	modelName string,
) *schedulePlanRunner {
	return &schedulePlanRunner{
		chatModel:   chatModel,
		deps:        deps,
		emitStage:   emitStage,
		userMessage: userMessage,
		extra:       extra,
		chatHistory: chatHistory,
		outChan:     outChan,
		modelName:   modelName,
	}
}

// ── 节点方法引用适配层 ──

func (r *schedulePlanRunner) planNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runPlanNode(ctx, st, r.chatModel, r.userMessage, r.extra, r.chatHistory, r.emitStage)
}

func (r *schedulePlanRunner) previewNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runPreviewNode(ctx, st, r.deps, r.emitStage)
}

func (r *schedulePlanRunner) materializeNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runMaterializeNode(ctx, st, r.chatModel, r.deps, r.emitStage)
}

func (r *schedulePlanRunner) applyNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runApplyNode(ctx, st, r.deps, r.emitStage)
}

func (r *schedulePlanRunner) reflectNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runReflectNode(ctx, st, r.chatModel, r.emitStage)
}

func (r *schedulePlanRunner) finalizeNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runFinalizeNode(ctx, st, r.chatModel, r.emitStage)
}

func (r *schedulePlanRunner) exitNode(_ context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	// exit 节点不做任何业务逻辑，仅把当前状态原样透传到 END。
	return st, nil
}

// ── ReAct 精排节点适配层 ──

func (r *schedulePlanRunner) hybridBuildNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runHybridBuildNode(ctx, st, r.deps, r.emitStage)
}

func (r *schedulePlanRunner) reactRefineNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runReactRefineNode(ctx, st, r.chatModel, r.outChan, r.modelName, r.emitStage)
}

func (r *schedulePlanRunner) returnPreviewNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runReturnPreviewNode(ctx, st, r.emitStage)
}

// ── 分支决策适配层 ──

func (r *schedulePlanRunner) nextAfterPlan(_ context.Context, st *SchedulePlanState) (string, error) {
	return selectNextAfterPlan(st), nil
}

func (r *schedulePlanRunner) nextAfterMaterialize(_ context.Context, st *SchedulePlanState) (string, error) {
	// materialize 后：有 ApplyRequest 则去 apply，否则去 exit。
	if st == nil || st.ApplyRequest == nil || len(st.ApplyRequest.Items) == 0 {
		return schedulePlanGraphNodeExit, nil
	}
	return schedulePlanGraphNodeApply, nil
}

func (r *schedulePlanRunner) nextAfterApply(_ context.Context, st *SchedulePlanState) (string, error) {
	return selectNextAfterApply(st), nil
}

func (r *schedulePlanRunner) nextAfterReflect(_ context.Context, st *SchedulePlanState) (string, error) {
	return selectNextAfterReflect(st), nil
}

// nextAfterPreview 根据 preview 结果决定下一步。
//
// 分支规则：
// 1) preview 失败（无候选方案）-> exit
// 2) HybridScheduleWithPlan 已注入 -> hybridBuild（走 ReAct 精排路径）
// 3) 否则 -> materialize（走原有落库路径，向后兼容）
func (r *schedulePlanRunner) nextAfterPreview(_ context.Context, st *SchedulePlanState) (string, error) {
	if st == nil || len(st.CandidatePlans) == 0 {
		return schedulePlanGraphNodeExit, nil
	}
	if r.deps.HybridScheduleWithPlan != nil {
		return schedulePlanGraphNodeHybridBuild, nil
	}
	return schedulePlanGraphNodeMaterialize, nil
}

// nextAfterHybridBuild 根据 hybridBuild 结果决定下一步。
func (r *schedulePlanRunner) nextAfterHybridBuild(_ context.Context, st *SchedulePlanState) (string, error) {
	if st == nil || len(st.HybridEntries) == 0 {
		return schedulePlanGraphNodeExit, nil
	}
	return schedulePlanGraphNodeReactRefine, nil
}

// nextAfterFinalize 用于 finalize 分支——固定结束。
func (r *schedulePlanRunner) nextAfterFinalize(_ context.Context, _ *SchedulePlanState) (string, error) {
	return compose.END, nil
}
