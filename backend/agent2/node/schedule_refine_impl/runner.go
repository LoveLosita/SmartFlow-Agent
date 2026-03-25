package schedulerefine

import (
	"context"

	"github.com/cloudwego/eino-ext/components/model/ark"
)

// scheduleRefineRunner 是“单次图运行”的请求级依赖容器。
//
// 职责边界：
// 1. 负责收口模型与阶段回调，避免 graph.go 出现大量闭包；
// 2. 负责把节点函数适配为统一签名；
// 3. 不负责分支决策（当前链路为线性图）。
type scheduleRefineRunner struct {
	chatModel *ark.ChatModel
	emitStage func(stage, detail string)
}

func newScheduleRefineRunner(chatModel *ark.ChatModel, emitStage func(stage, detail string)) *scheduleRefineRunner {
	return &scheduleRefineRunner{
		chatModel: chatModel,
		emitStage: emitStage,
	}
}

func (r *scheduleRefineRunner) contractNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runContractNode(ctx, r.chatModel, st, r.emitStage)
}

func (r *scheduleRefineRunner) planNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runPlanNode(ctx, r.chatModel, st, r.emitStage)
}

func (r *scheduleRefineRunner) sliceNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runSliceNode(ctx, st, r.emitStage)
}

func (r *scheduleRefineRunner) routeNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runCompositeRouteNode(ctx, st, r.emitStage)
}

func (r *scheduleRefineRunner) reactNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runReactLoopNode(ctx, r.chatModel, st, r.emitStage)
}

func (r *scheduleRefineRunner) hardCheckNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runHardCheckNode(ctx, r.chatModel, st, r.emitStage)
}

func (r *scheduleRefineRunner) summaryNode(ctx context.Context, st *ScheduleRefineState) (*ScheduleRefineState, error) {
	return runSummaryNode(ctx, r.chatModel, st, r.emitStage)
}
