package taskquery

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

const (
	// 图节点：意图规划（一次模型调用，产出结构化查询计划）
	taskQueryGraphNodePlan = "task_query_plan"
	// 图节点：象限归一化（不调模型，只做参数规整）
	taskQueryGraphNodeQuadrant = "task_query_quadrant"
	// 图节点：时间锚定（不调模型，锁定绝对时间边界）
	taskQueryGraphNodeTime = "task_query_time_anchor"
	// 图节点：工具查询（调用 query_tasks 工具）
	taskQueryGraphNodeQuery = "task_query_tool_query"
	// 图节点：结果反思与回复（模型判断是否满足并产出回复/重试补丁）
	taskQueryGraphNodeReflect = "task_query_reflect"
)

// QueryGraphRunInput 是任务查询图运行输入。
//
// 职责边界：
// 1. Model/Deps 提供图运行依赖；
// 2. UserMessage/RequestNowText 提供本次请求上下文；
// 3. MaxReflectRetry 控制“反思重试”上限；
// 4. EmitStage 是可选阶段推送钩子，不影响主链路成功与否。
type QueryGraphRunInput struct {
	Model           *ark.ChatModel
	UserMessage     string
	RequestNowText  string
	Deps            TaskQueryToolDeps
	MaxReflectRetry int
	EmitStage       func(stage, detail string)
}

// RunTaskQueryGraph 执行“任务查询图编排”。
//
// 关键策略：
// 1. 规划节点只调用一次模型，统一产出查询计划；
// 2. 查询节点优先按计划查，若为空先自动放宽一次（无额外模型调用）；
// 3. 反思节点最多重试 2 次，每次决定“是否满足、是否继续、如何补丁”。
func RunTaskQueryGraph(ctx context.Context, input QueryGraphRunInput) (string, error) {
	// 1. 启动前硬校验。
	if input.Model == nil {
		return "", errors.New("task query graph: model is nil")
	}
	if err := input.Deps.validate(); err != nil {
		return "", err
	}

	// 2. 构建工具包，并拿到 query_tasks 可执行工具。
	toolBundle, err := BuildTaskQueryToolBundle(ctx, input.Deps)
	if err != nil {
		return "", err
	}
	toolMap, err := buildInvokableToolMap(toolBundle)
	if err != nil {
		return "", err
	}
	queryTool, exists := toolMap[ToolNameTaskQueryTasks]
	if !exists {
		return "", fmt.Errorf("task query graph: tool %s not found", ToolNameTaskQueryTasks)
	}

	// 3. 初始化状态：请求时间为空时做本地兜底。
	requestNow := strings.TrimSpace(input.RequestNowText)
	if requestNow == "" {
		requestNow = time.Now().In(time.Local).Format("2006-01-02 15:04")
	}
	state := NewTaskQueryState(strings.TrimSpace(input.UserMessage), requestNow, input.MaxReflectRetry)

	// 4. 封装 runner，把“依赖注入”和“节点逻辑”解耦。
	runner := newTaskQueryGraphRunner(input, queryTool)

	// 5. 只在本次请求内构图并执行，避免跨请求共享状态。
	graph := compose.NewGraph[*TaskQueryState, *TaskQueryState]()

	if err = graph.AddLambdaNode(taskQueryGraphNodePlan, compose.InvokableLambda(runner.planNode)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(taskQueryGraphNodeQuadrant, compose.InvokableLambda(runner.quadrantNode)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(taskQueryGraphNodeTime, compose.InvokableLambda(runner.timeAnchorNode)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(taskQueryGraphNodeQuery, compose.InvokableLambda(runner.queryNode)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(taskQueryGraphNodeReflect, compose.InvokableLambda(runner.reflectNode)); err != nil {
		return "", err
	}

	// 连线：START -> plan -> quadrant -> time -> query -> reflect
	if err = graph.AddEdge(compose.START, taskQueryGraphNodePlan); err != nil {
		return "", err
	}
	if err = graph.AddEdge(taskQueryGraphNodePlan, taskQueryGraphNodeQuadrant); err != nil {
		return "", err
	}
	if err = graph.AddEdge(taskQueryGraphNodeQuadrant, taskQueryGraphNodeTime); err != nil {
		return "", err
	}
	if err = graph.AddEdge(taskQueryGraphNodeTime, taskQueryGraphNodeQuery); err != nil {
		return "", err
	}
	if err = graph.AddEdge(taskQueryGraphNodeQuery, taskQueryGraphNodeReflect); err != nil {
		return "", err
	}

	// 分支：reflect 后要么结束，要么回到 query 重试。
	if err = graph.AddBranch(taskQueryGraphNodeReflect, compose.NewGraphBranch(
		runner.nextAfterReflect,
		map[string]bool{
			taskQueryGraphNodeQuery: true,
			compose.END:             true,
		},
	)); err != nil {
		return "", err
	}

	maxRunSteps := 24 + state.MaxReflectRetry*4
	if maxRunSteps < 24 {
		maxRunSteps = 24
	}
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName("TaskQueryGraph"),
		compose.WithMaxRunSteps(maxRunSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return "", err
	}

	finalState, err := runnable.Invoke(ctx, state)
	if err != nil {
		return "", err
	}
	if finalState == nil {
		return "", errors.New("task query graph: final state is nil")
	}

	reply := strings.TrimSpace(finalState.FinalReply)
	if reply == "" {
		reply = buildTaskQueryFallbackReply(finalState.LastQueryItems)
	}
	return reply, nil
}

type taskQueryGraphRunner struct {
	input     QueryGraphRunInput
	queryTool tool.InvokableTool
}

func newTaskQueryGraphRunner(input QueryGraphRunInput, queryTool tool.InvokableTool) *taskQueryGraphRunner {
	return &taskQueryGraphRunner{
		input:     input,
		queryTool: queryTool,
	}
}

func (r *taskQueryGraphRunner) emit(stage, detail string) {
	if r.input.EmitStage == nil {
		return
	}
	r.input.EmitStage(stage, detail)
}

func (r *taskQueryGraphRunner) nextAfterReflect(ctx context.Context, st *TaskQueryState) (string, error) {
	_ = ctx
	if st != nil && st.NeedRetry {
		return taskQueryGraphNodeQuery, nil
	}
	return compose.END, nil
}
