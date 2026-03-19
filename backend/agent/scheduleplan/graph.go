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
	// 图节点：调用粗排算法生成候选方案
	schedulePlanGraphNodePreview = "schedule_plan_preview"
	// 图节点：将候选方案转换为可落库结构
	schedulePlanGraphNodeMaterialize = "schedule_plan_materialize"
	// 图节点：执行落库
	schedulePlanGraphNodeApply = "schedule_plan_apply"
	// 图节点：分析失败原因并生成修补方案
	schedulePlanGraphNodeReflect = "schedule_plan_reflect"
	// 图节点：生成最终回复文案
	schedulePlanGraphNodeFinalize = "schedule_plan_finalize"
	// 图节点：退出（用于提前终止分支）
	schedulePlanGraphNodeExit = "schedule_plan_exit"
	// 图节点：构建混合日程（ReAct 精排前置）
	schedulePlanGraphNodeHybridBuild = "schedule_plan_hybrid_build"
	// 图节点：ReAct 精排循环
	schedulePlanGraphNodeReactRefine = "schedule_plan_react_refine"
	// 图节点：返回精排预览结果（不落库）
	schedulePlanGraphNodeReturnPreview = "schedule_plan_return_preview"
)

// SchedulePlanGraphRunInput 是运行"智能排程 graph"所需的输入依赖。
//
// 说明：
// 1) EmitStage 可选，用于把节点进度推送给外层（例如 SSE 状态块）；
// 2) Extra 传递前端附加参数（如 task_class_id）；
// 3) ChatHistory 用于连续对话微调场景。
type SchedulePlanGraphRunInput struct {
	Model       *ark.ChatModel
	State       *SchedulePlanState
	Deps        SchedulePlanToolDeps
	UserMessage string
	Extra       map[string]any
	ChatHistory []*schema.Message
	EmitStage   func(stage, detail string)
	// ── ReAct 精排所需 ──
	OutChan   chan<- string // SSE 流式输出通道，用于推送 reasoning_content
	ModelName string        // 模型名称，用于构造 OpenAI 兼容 chunk
}

// RunSchedulePlanGraph 执行"智能排程"图编排。
//
// 图结构：
//
//	START -> plan -> [branch] -> preview -> [branch] -> materialize -> [branch] -> apply -> [branch]
//	                    |                      |                          |                     |
//	                   exit                   exit                       exit               finalize (成功)
//	                                                                                          |
//	                                                                                       reflect -> [branch] -> apply (重试)
//	                                                                                                      |
//	                                                                                                   finalize (放弃)
//
// 该文件只负责"连线与分支"，节点内部逻辑全部下沉到 nodes.go。
func RunSchedulePlanGraph(ctx context.Context, input SchedulePlanGraphRunInput) (*SchedulePlanState, error) {
	// 1. 启动前硬校验：模型、状态、依赖缺一不可。
	if input.Model == nil {
		return nil, errors.New("schedule plan graph: model is nil")
	}
	if input.State == nil {
		return nil, errors.New("schedule plan graph: state is nil")
	}
	if err := input.Deps.validate(); err != nil {
		return nil, err
	}

	// 2. 统一封装阶段推送函数，避免各节点反复判空。
	emitStage := func(stage, detail string) {
		if input.EmitStage != nil {
			input.EmitStage(stage, detail)
		}
	}

	// 3. 构造 runner，收口节点依赖。
	runner := newSchedulePlanRunner(
		input.Model,
		input.Deps,
		emitStage,
		input.UserMessage,
		input.Extra,
		input.ChatHistory,
		input.OutChan,
		input.ModelName,
	)

	// 4. 创建状态图容器：输入/输出类型都为 *SchedulePlanState。
	graph := compose.NewGraph[*SchedulePlanState, *SchedulePlanState]()

	// 5. 注册节点。
	if err := graph.AddLambdaNode(schedulePlanGraphNodePlan, compose.InvokableLambda(runner.planNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodePreview, compose.InvokableLambda(runner.previewNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeMaterialize, compose.InvokableLambda(runner.materializeNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeApply, compose.InvokableLambda(runner.applyNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeReflect, compose.InvokableLambda(runner.reflectNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeFinalize, compose.InvokableLambda(runner.finalizeNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeExit, compose.InvokableLambda(runner.exitNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeHybridBuild, compose.InvokableLambda(runner.hybridBuildNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeReactRefine, compose.InvokableLambda(runner.reactRefineNode)); err != nil {
		return nil, err
	}
	if err := graph.AddLambdaNode(schedulePlanGraphNodeReturnPreview, compose.InvokableLambda(runner.returnPreviewNode)); err != nil {
		return nil, err
	}

	// ── 连线 ──

	// 6. START -> plan
	if err := graph.AddEdge(compose.START, schedulePlanGraphNodePlan); err != nil {
		return nil, err
	}

	// 7. plan -> [branch] -> preview | exit
	if err := graph.AddBranch(schedulePlanGraphNodePlan, compose.NewGraphBranch(
		runner.nextAfterPlan,
		map[string]bool{
			schedulePlanGraphNodePreview: true,
			schedulePlanGraphNodeExit:    true,
		},
	)); err != nil {
		return nil, err
	}

	// 8. preview -> [branch] -> hybridBuild | materialize | exit
	if err := graph.AddBranch(schedulePlanGraphNodePreview, compose.NewGraphBranch(
		runner.nextAfterPreview,
		map[string]bool{
			schedulePlanGraphNodeHybridBuild: true,
			schedulePlanGraphNodeMaterialize: true,
			schedulePlanGraphNodeExit:        true,
		},
	)); err != nil {
		return nil, err
	}

	// 8.1 hybridBuild -> [branch] -> reactRefine | exit
	if err := graph.AddBranch(schedulePlanGraphNodeHybridBuild, compose.NewGraphBranch(
		runner.nextAfterHybridBuild,
		map[string]bool{
			schedulePlanGraphNodeReactRefine: true,
			schedulePlanGraphNodeExit:        true,
		},
	)); err != nil {
		return nil, err
	}

	// 8.2 reactRefine -> returnPreview（固定边）
	if err := graph.AddEdge(schedulePlanGraphNodeReactRefine, schedulePlanGraphNodeReturnPreview); err != nil {
		return nil, err
	}

	// 8.3 returnPreview -> END
	if err := graph.AddEdge(schedulePlanGraphNodeReturnPreview, compose.END); err != nil {
		return nil, err
	}

	// 9. materialize -> [branch] -> apply | exit
	if err := graph.AddBranch(schedulePlanGraphNodeMaterialize, compose.NewGraphBranch(
		runner.nextAfterMaterialize,
		map[string]bool{
			schedulePlanGraphNodeApply: true,
			schedulePlanGraphNodeExit:  true,
		},
	)); err != nil {
		return nil, err
	}

	// 10. apply -> [branch] -> finalize | reflect
	if err := graph.AddBranch(schedulePlanGraphNodeApply, compose.NewGraphBranch(
		runner.nextAfterApply,
		map[string]bool{
			schedulePlanGraphNodeFinalize: true,
			schedulePlanGraphNodeReflect:  true,
		},
	)); err != nil {
		return nil, err
	}

	// 11. reflect -> [branch] -> apply (重试) | finalize (放弃)
	if err := graph.AddBranch(schedulePlanGraphNodeReflect, compose.NewGraphBranch(
		runner.nextAfterReflect,
		map[string]bool{
			schedulePlanGraphNodeApply:    true,
			schedulePlanGraphNodeFinalize: true,
		},
	)); err != nil {
		return nil, err
	}

	// 12. finalize -> END
	if err := graph.AddEdge(schedulePlanGraphNodeFinalize, compose.END); err != nil {
		return nil, err
	}

	// 13. exit -> END
	if err := graph.AddEdge(schedulePlanGraphNodeExit, compose.END); err != nil {
		return nil, err
	}

	// 14. 运行步数上限：原有链路 ~10 步 + ReAct 精排（hybridBuild + reactRefine + returnPreview = 3）。
	//     加余量到 25，防止异常分支导致无限循环。
	maxSteps := 25

	// 15. 编译图得到可执行实例。
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("SchedulePlanGraph"),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return nil, err
	}

	// 16. 执行图并返回最终状态。
	return runnable.Invoke(ctx, input.State)
}
