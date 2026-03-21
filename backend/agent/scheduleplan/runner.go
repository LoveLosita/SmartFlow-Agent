package scheduleplan

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
)

// schedulePlanRunner 是“单次图执行”的请求级依赖容器。
//
// 设计目标：
// 1. 把节点运行所需依赖（model/deps/emit/extra/history）就近收口；
// 2. 让 graph.go 只保留“节点连线与分支决策”，提升可读性；
// 3. 避免在 graph.go 里重复出现大量闭包和参数透传。
type schedulePlanRunner struct {
	chatModel   *ark.ChatModel
	deps        SchedulePlanToolDeps
	emitStage   func(stage, detail string)
	userMessage string
	extra       map[string]any
	chatHistory []*schema.Message

	// weekly refine 需要的上下文
	outChan   chan<- string
	modelName string

	// daily refine 并发度
	dailyRefineConcurrency int
}

func newSchedulePlanRunner(
	chatModel *ark.ChatModel,
	deps SchedulePlanToolDeps,
	emitStage func(stage, detail string),
	userMessage string,
	extra map[string]any,
	chatHistory []*schema.Message,
	outChan chan<- string,
	modelName string,
	dailyRefineConcurrency int,
) *schedulePlanRunner {
	return &schedulePlanRunner{
		chatModel:              chatModel,
		deps:                   deps,
		emitStage:              emitStage,
		userMessage:            userMessage,
		extra:                  extra,
		chatHistory:            chatHistory,
		outChan:                outChan,
		modelName:              modelName,
		dailyRefineConcurrency: dailyRefineConcurrency,
	}
}

// 节点方法适配层

func (r *schedulePlanRunner) planNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runPlanNode(ctx, st, r.chatModel, r.userMessage, r.extra, r.chatHistory, r.emitStage)
}

func (r *schedulePlanRunner) roughBuildNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runRoughBuildNode(ctx, st, r.deps, r.emitStage)
}

func (r *schedulePlanRunner) dailySplitNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runDailySplitNode(ctx, st, r.emitStage)
}

func (r *schedulePlanRunner) dailyRefineNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runDailyRefineNode(ctx, st, r.chatModel, r.dailyRefineConcurrency, r.emitStage)
}

func (r *schedulePlanRunner) mergeNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runMergeNode(ctx, st, r.emitStage)
}

func (r *schedulePlanRunner) weeklyRefineNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runWeeklyRefineNode(ctx, st, r.chatModel, r.outChan, r.modelName, r.emitStage)
}

func (r *schedulePlanRunner) finalCheckNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runFinalCheckNode(ctx, st, r.chatModel, r.emitStage)
}

func (r *schedulePlanRunner) returnPreviewNode(ctx context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return runReturnPreviewNode(ctx, st, r.emitStage)
}

func (r *schedulePlanRunner) exitNode(_ context.Context, st *SchedulePlanState) (*SchedulePlanState, error) {
	return st, nil
}

// 分支决策适配层

func (r *schedulePlanRunner) nextAfterPlan(_ context.Context, st *SchedulePlanState) (string, error) {
	return selectNextAfterPlan(st), nil
}

// nextAfterRoughBuild 根据粗排构建结果决定后续路径。
//
// 规则：
// 1. 没有可优化条目 -> exit；
// 2. task_class_ids >= 2 -> dailySplit（多任务类混排，先做日内并发）；
// 3. task_class_ids == 1 -> weeklyRefine（单任务类直接周级配平）。
func (r *schedulePlanRunner) nextAfterRoughBuild(_ context.Context, st *SchedulePlanState) (string, error) {
	if st == nil || len(st.HybridEntries) == 0 {
		return schedulePlanGraphNodeExit, nil
	}
	if len(st.TaskClassIDs) >= 2 {
		return schedulePlanGraphNodeDailySplit, nil
	}
	return schedulePlanGraphNodeWeeklyRefine, nil
}
