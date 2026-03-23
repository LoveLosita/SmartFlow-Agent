package schedulerefine

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/compose"
)

const (
	graphNodeContract  = "schedule_refine_contract"
	graphNodePlan      = "schedule_refine_plan"
	graphNodeSlice     = "schedule_refine_slice"
	graphNodeRoute     = "schedule_refine_route"
	graphNodeReact     = "schedule_refine_react"
	graphNodeHardCheck = "schedule_refine_hard_check"
	graphNodeSummary   = "schedule_refine_summary"
)

// ScheduleRefineGraphRunInput 是“连续微调图”运行参数。
//
// 字段语义：
// 1. Model：本轮图运行使用的聊天模型。
// 2. State：预先注入的微调状态（通常来自上一版预览快照）。
// 3. EmitStage：SSE 阶段回调，允许服务层把阶段进度透传给前端。
type ScheduleRefineGraphRunInput struct {
	Model     *ark.ChatModel
	State     *ScheduleRefineState
	EmitStage func(stage, detail string)
}

// RunScheduleRefineGraph 执行“连续微调”独立图链路。
//
// 链路顺序：
// START -> contract -> plan -> slice -> route -> react -> hard_check -> summary -> END
//
// 设计说明：
// 1. 当前链路采用线性图，确保可读性优先；
// 2. “终审失败后单次修复”在 hard_check 节点内部闭环处理，避免图连线分叉过多；
// 3. 若后续需要引入多分支策略（例如大改动转重排），可在 contract 后追加 branch 节点。
func RunScheduleRefineGraph(ctx context.Context, input ScheduleRefineGraphRunInput) (*ScheduleRefineState, error) {
	if input.Model == nil {
		return nil, fmt.Errorf("schedule refine graph: model is nil")
	}
	if input.State == nil {
		return nil, fmt.Errorf("schedule refine graph: state is nil")
	}

	emitStage := func(stage, detail string) {
		if input.EmitStage != nil {
			input.EmitStage(stage, detail)
		}
	}
	runner := newScheduleRefineRunner(input.Model, emitStage)

	graph := compose.NewGraph[*ScheduleRefineState, *ScheduleRefineState]()
	if err := graph.AddLambdaNode(graphNodeContract, compose.InvokableLambda(runner.contractNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(graphNodePlan, compose.InvokableLambda(runner.planNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(graphNodeSlice, compose.InvokableLambda(runner.sliceNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(graphNodeRoute, compose.InvokableLambda(runner.routeNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(graphNodeReact, compose.InvokableLambda(runner.reactNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(graphNodeHardCheck, compose.InvokableLambda(runner.hardCheckNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(graphNodeSummary, compose.InvokableLambda(runner.summaryNode)); err != nil {
		return nil, err
	}

	if err := graph.AddEdge(compose.START, graphNodeContract); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodeContract, graphNodePlan); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodePlan, graphNodeSlice); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodeSlice, graphNodeRoute); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodeRoute, graphNodeReact); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodeReact, graphNodeHardCheck); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodeHardCheck, graphNodeSummary); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(graphNodeSummary, compose.END); err != nil {
		return nil, err
	}

	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("ScheduleRefineGraph"),
		compose.WithMaxRunSteps(20),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}
	return runnable.Invoke(ctx, input.State)
}
