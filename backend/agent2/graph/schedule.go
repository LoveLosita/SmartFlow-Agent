package agentgraph

import (
	"context"
	"errors"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent2/node"
	"github.com/cloudwego/eino/compose"
)

const (
	SchedulePlanGraphName   = "schedule_plan"
	ScheduleRefineGraphName = "schedule_refine"
)

func RunSchedulePlanGraph(ctx context.Context, input agentnode.SchedulePlanGraphRunInput) (*agentmodel.SchedulePlanState, error) {
	if input.Model == nil {
		return nil, errors.New("schedule plan graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule plan graph: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return nil, err
	}

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

	if err = graph.AddEdge(compose.START, agentnode.SchedulePlanGraphNodePlan); err != nil {
		return nil, err
	}
	if err = graph.AddBranch(agentnode.SchedulePlanGraphNodePlan, compose.NewGraphBranch(
		nodes.NextAfterPlan,
		map[string]bool{
			agentnode.SchedulePlanGraphNodeRoughBuild: true,
			agentnode.SchedulePlanGraphNodeExit:       true,
		},
	)); err != nil {
		return nil, err
	}
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

func RunScheduleRefineGraph(ctx context.Context, input agentnode.ScheduleRefineGraphRunInput) (*agentnode.ScheduleRefineState, error) {
	if input.Model == nil {
		return nil, errors.New("schedule refine graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule refine graph: state is nil")
	}
	return agentnode.RunScheduleRefineGraph(ctx, input)
}
