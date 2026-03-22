package scheduleplan

import (
	"context"
	"errors"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const (
	// 图节点：意图识别与约束提取
	schedulePlanGraphNodePlan = "schedule_plan_plan"
	// 图节点：粗排构建（替代旧 preview + hybridBuild）
	schedulePlanGraphNodeRoughBuild = "schedule_plan_rough_build"
	// 图节点：提前退出
	schedulePlanGraphNodeExit = "schedule_plan_exit"
	// 图节点：按天拆分并注入上下文标签
	schedulePlanGraphNodeDailySplit = "schedule_plan_daily_split"
	// 图节点：小改动快速微调（用于 small scope）
	schedulePlanGraphNodeQuickRefine = "schedule_plan_quick_refine"
	// 图节点：并发日内优化
	schedulePlanGraphNodeDailyRefine = "schedule_plan_daily_refine"
	// 图节点：合并日内优化结果
	schedulePlanGraphNodeMerge = "schedule_plan_merge"
	// 图节点：周级配平优化（单步动作模式，输出阶段状态）
	schedulePlanGraphNodeWeeklyRefine = "schedule_plan_weekly_refine"
	// 图节点：终审校验
	schedulePlanGraphNodeFinalCheck = "schedule_plan_final_check"
	// 图节点：返回预览结果（不落库）
	schedulePlanGraphNodeReturnPreview = "schedule_plan_return_preview"
)

// SchedulePlanGraphRunInput 是执行“智能排程 graph”所需输入。
//
// 字段说明：
// 1. Extra：前端附加参数（重点是 task_class_ids）；
// 2. ChatHistory：支持连续对话微调；
// 3. OutChan/ModelName：保留兼容字段（当前 weekly refine 主要输出阶段状态）；
// 4. DailyRefineConcurrency/WeeklyAdjustBudget：可选运行参数覆盖。
type SchedulePlanGraphRunInput struct {
	Model       *ark.ChatModel
	State       *SchedulePlanState
	Deps        SchedulePlanToolDeps
	UserMessage string
	Extra       map[string]any
	ChatHistory []*schema.Message
	EmitStage   func(stage, detail string)

	OutChan   chan<- string
	ModelName string

	DailyRefineConcurrency int
	WeeklyAdjustBudget     int
}

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
// 2. 本文件只负责“连线与分支”，节点内业务都在 nodes/daily/weekly 文件中。
func RunSchedulePlanGraph(ctx context.Context, input SchedulePlanGraphRunInput) (*SchedulePlanState, error) {
	// 1. 启动前硬校验。
	if input.Model == nil {
		return nil, errors.New("schedule plan graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule plan graph: state is nil")
	}
	if err := input.Deps.validate(); err != nil {
		return nil, err
	}

	// 2. 注入运行时配置（可选覆盖）。
	if input.DailyRefineConcurrency > 0 {
		input.State.DailyRefineConcurrency = input.DailyRefineConcurrency
	}
	if input.WeeklyAdjustBudget > 0 {
		input.State.WeeklyAdjustBudget = input.WeeklyAdjustBudget
	}

	emitStage := func(stage, detail string) {
		if input.EmitStage != nil {
			input.EmitStage(stage, detail)
		}
	}

	runner := newSchedulePlanRunner(
		input.Model,
		input.Deps,
		emitStage,
		input.UserMessage,
		input.Extra,
		input.ChatHistory,
		input.OutChan,
		input.ModelName,
		input.State.DailyRefineConcurrency,
	)

	graph := compose.NewGraph[*SchedulePlanState, *SchedulePlanState]()

	// 3. 注册节点。
	if err := graph.AddLambdaNode(schedulePlanGraphNodePlan, compose.InvokableLambda(runner.planNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeRoughBuild, compose.InvokableLambda(runner.roughBuildNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeExit, compose.InvokableLambda(runner.exitNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeDailySplit, compose.InvokableLambda(runner.dailySplitNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeQuickRefine, compose.InvokableLambda(runner.quickRefineNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeDailyRefine, compose.InvokableLambda(runner.dailyRefineNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeMerge, compose.InvokableLambda(runner.mergeNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeWeeklyRefine, compose.InvokableLambda(runner.weeklyRefineNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeFinalCheck, compose.InvokableLambda(runner.finalCheckNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeReturnPreview, compose.InvokableLambda(runner.returnPreviewNode)); err != nil {
		return nil, err
	}

	// 4. 连线：START -> plan
	if err := graph.AddEdge(compose.START, schedulePlanGraphNodePlan); err != nil {
		return nil, err
	}

	// 5. plan 分支：roughBuild | exit
	if err := graph.AddBranch(schedulePlanGraphNodePlan, compose.NewGraphBranch(
		runner.nextAfterPlan,
		map[string]bool{
			schedulePlanGraphNodeRoughBuild: true,
			schedulePlanGraphNodeExit:       true,
		},
	)); err != nil {
		return nil, err
	}

	// 6. roughBuild 分支：dailySplit | weeklyRefine | exit
	if err := graph.AddBranch(schedulePlanGraphNodeRoughBuild, compose.NewGraphBranch(
		runner.nextAfterRoughBuild,
		map[string]bool{
			schedulePlanGraphNodeDailySplit:   true,
			schedulePlanGraphNodeQuickRefine:  true,
			schedulePlanGraphNodeWeeklyRefine: true,
			schedulePlanGraphNodeExit:         true,
		},
	)); err != nil {
		return nil, err
	}

	// 7. 固定边：quickRefine -> weeklyRefine；dailySplit -> dailyRefine -> merge -> weeklyRefine -> finalCheck -> returnPreview -> END
	if err := graph.AddEdge(schedulePlanGraphNodeQuickRefine, schedulePlanGraphNodeWeeklyRefine); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeDailySplit, schedulePlanGraphNodeDailyRefine); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeDailyRefine, schedulePlanGraphNodeMerge); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeMerge, schedulePlanGraphNodeWeeklyRefine); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeWeeklyRefine, schedulePlanGraphNodeFinalCheck); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeFinalCheck, schedulePlanGraphNodeReturnPreview); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeReturnPreview, compose.END); err != nil {
		return nil, err
	}
	if err := graph.AddEdge(schedulePlanGraphNodeExit, compose.END); err != nil {
		return nil, err
	}

	// 8. 编译并执行。
	// 路径最多约 8~9 个节点，保守预留 20 步避免误判。
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("SchedulePlanGraph"),
		compose.WithMaxRunSteps(20),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}
	return runnable.Invoke(ctx, input.State)
}
