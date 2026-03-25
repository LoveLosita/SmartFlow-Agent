package agentgraph

import (
	"context"
	"errors"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"
	"github.com/cloudwego/eino/compose"
)

const (
	// SchedulePlanGraphName 是首次排程 graph 的稳定标识。
	SchedulePlanGraphName = "schedule_plan"
	// ScheduleRefineGraphName 先保留给 refine 链路使用。
	ScheduleRefineGraphName = "schedule_refine"
)

// RunSchedulePlanGraph 执行“智能排程”图编排。
//
// 当前链路：
// START
// -> plan
// -> roughBuild
// -> (len(task_class_ids)>=2 ? dailySplit -> dailyRefine -> merge : weeklyRefine)
// -> finalCheck
// -> returnPreview
// -> END
//
// 说明：
// 1. exit 分支可从 plan/roughBuild 直接提前终止；
// 2. 本文件只负责“连线与分支”，节点内业务都在 node 层实现；
// 3. 这轮已经去掉旧 runner 适配层，graph 直接挂 node 方法，减少一跳阅读成本。
func RunSchedulePlanGraph(ctx context.Context, input agentnode.SchedulePlanGraphRunInput) (*agentmodel.SchedulePlanState, error) {
	// 1. 启动前硬校验。
	if input.Model == nil {
		return nil, errors.New("schedule plan graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule plan graph: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return nil, err
	}

	// 2. 注入运行时配置（可选覆盖）。
	if input.DailyRefineConcurrency > 0 {
		input.State.DailyRefineConcurrency = input.DailyRefineConcurrency
	}
	if input.WeeklyAdjustBudget > 0 {
		input.State.WeeklyAdjustBudget = input.WeeklyAdjustBudget
	}

	nodes, err := agentnode.NewSchedulePlanNodes(input)
	if err != nil {
		return nil, err
	}

	graph := compose.NewGraph[*agentmodel.SchedulePlanState, *agentmodel.SchedulePlanState]()

	// 3. 注册节点。
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodePlan, compose.InvokableLambda(nodes.Plan)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeRoughBuild, compose.InvokableLambda(nodes.RoughBuild)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeExit, compose.InvokableLambda(nodes.Exit)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeDailySplit, compose.InvokableLambda(nodes.DailySplit)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeQuickRefine, compose.InvokableLambda(nodes.QuickRefine)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeDailyRefine, compose.InvokableLambda(nodes.DailyRefine)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeMerge, compose.InvokableLambda(nodes.Merge)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeWeeklyRefine, compose.InvokableLambda(nodes.WeeklyRefine)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeFinalCheck, compose.InvokableLambda(nodes.FinalCheck)); err != nil {
		return nil, err
	}
	if err = graph.AddLambdaNode(agentnode.SchedulePlanGraphNodeReturnPreview, compose.InvokableLambda(nodes.ReturnPreview)); err != nil {
		return nil, err
	}

	// 4. 固定入口：START -> plan。
	if err = graph.AddEdge(compose.START, agentnode.SchedulePlanGraphNodePlan); err != nil {
		return nil, err
	}

	// 5. plan 分支：roughBuild | exit。
	if err = graph.AddBranch(agentnode.SchedulePlanGraphNodePlan, compose.NewGraphBranch(
		nodes.NextAfterPlan,
		map[string]bool{
			agentnode.SchedulePlanGraphNodeRoughBuild: true,
			agentnode.SchedulePlanGraphNodeExit:       true,
		},
	)); err != nil {
		return nil, err
	}

	// 6. roughBuild 分支：dailySplit | quickRefine | weeklyRefine | exit。
	if err = graph.AddBranch(agentnode.SchedulePlanGraphNodeRoughBuild, compose.NewGraphBranch(
		nodes.NextAfterRoughBuild,
		map[string]bool{
			agentnode.SchedulePlanGraphNodeDailySplit:   true,
			agentnode.SchedulePlanGraphNodeQuickRefine:  true,
			agentnode.SchedulePlanGraphNodeWeeklyRefine: true,
			agentnode.SchedulePlanGraphNodeExit:         true,
		},
	)); err != nil {
		return nil, err
	}

	// 7. 固定边：quickRefine -> weeklyRefine；dailySplit -> dailyRefine -> merge -> weeklyRefine -> finalCheck -> returnPreview -> END。
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeQuickRefine, agentnode.SchedulePlanGraphNodeWeeklyRefine); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeDailySplit, agentnode.SchedulePlanGraphNodeDailyRefine); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeDailyRefine, agentnode.SchedulePlanGraphNodeMerge); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeMerge, agentnode.SchedulePlanGraphNodeWeeklyRefine); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeWeeklyRefine, agentnode.SchedulePlanGraphNodeFinalCheck); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeFinalCheck, agentnode.SchedulePlanGraphNodeReturnPreview); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeReturnPreview, compose.END); err != nil {
		return nil, err
	}
	if err = graph.AddEdge(agentnode.SchedulePlanGraphNodeExit, compose.END); err != nil {
		return nil, err
	}

	// 8. 编译并执行。
	// 路径最多约 8~9 个节点，保守预留 20 步避免误判。
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(SchedulePlanGraphName),
		compose.WithMaxRunSteps(20),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}
	return runnable.Invoke(ctx, input.State)
}

// ScheduleRefineGraph 先保留骨架，避免本轮“只迁 schedule_plan”时误动 refine 主链路。
type ScheduleRefineGraph struct {
	Nodes *agentnode.ScheduleRefineNodes
}

// NewScheduleRefineGraph 创建连续微调图骨架。
func NewScheduleRefineGraph(nodes *agentnode.ScheduleRefineNodes) *ScheduleRefineGraph {
	return &ScheduleRefineGraph{Nodes: nodes}
}
