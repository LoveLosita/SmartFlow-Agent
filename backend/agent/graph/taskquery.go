package agentgraph

import (
	"context"
	"errors"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent/model"
	agentnode "github.com/LoveLosita/smartflow/backend/agent/node"
	"github.com/cloudwego/eino/compose"
)

const (
	// TaskQueryGraphName 是任务查询图编排的稳定标识。
	//
	// 职责边界：
	// 1. 仅用于 graph 编译、日志和排障时标识当前链路。
	// 2. 不承载路由判断，也不负责描述具体业务含义。
	TaskQueryGraphName = "task_query"
)

// RunTaskQueryGraph 负责串起任务查询图，并返回最终给用户的回复文本。
//
// 职责边界：
// 1. 负责做图运行前的依赖校验、默认值补齐、节点装配与 graph 编译执行。
// 2. 不负责实现单个节点的业务细节，这些逻辑由 node 层承接。
// 3. 返回值中的 string 是最终可直接透传给上层的回复；error 仅表示链路级失败。
func RunTaskQueryGraph(ctx context.Context, input agentnode.TaskQueryGraphRunInput) (string, error) {
	// 1. 先拦住空模型、空状态和依赖缺失，避免 graph 运行到一半才出现不可恢复错误。
	if input.Model == nil {
		return "", errors.New("task query graph: model is nil")
	}
	if input.State == nil {
		return "", errors.New("task query graph: state is nil")
	}
	if err := input.Deps.Validate(); err != nil {
		return "", err
	}

	// 2. 请求时间缺失时补齐当前时间，保证后续时间锚定与提示词上下文稳定。
	if strings.TrimSpace(input.State.RequestNowText) == "" {
		input.State.RequestNowText = time.Now().In(time.Local).Format("2006-01-02 15:04")
	}

	// 3. 先准备工具，再构造节点容器；这样 graph 中每个节点都能拿到已校验好的依赖。
	toolBundle, err := agentnode.BuildTaskQueryToolBundle(ctx, input.Deps)
	if err != nil {
		return "", err
	}
	queryTool, err := agentnode.GetTaskQueryInvokableToolByName(toolBundle, agentnode.ToolNameTaskQueryTasks)
	if err != nil {
		return "", err
	}
	nodes, err := agentnode.NewTaskQueryNodes(input, queryTool)
	if err != nil {
		return "", err
	}

	// 4. 注册节点与边，保持“计划 -> 归一化 -> 时间锚定 -> 查询 -> 反思”的单向主链。
	graph := compose.NewGraph[*agentmodel.TaskQueryState, *agentmodel.TaskQueryState]()
	if err = graph.AddLambdaNode(agentnode.TaskQueryGraphNodePlan, compose.InvokableLambda(nodes.Plan)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(agentnode.TaskQueryGraphNodeQuadrant, compose.InvokableLambda(nodes.NormalizeQuadrant)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(agentnode.TaskQueryGraphNodeTimeAnchor, compose.InvokableLambda(nodes.AnchorTime)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(agentnode.TaskQueryGraphNodeQuery, compose.InvokableLambda(nodes.Query)); err != nil {
		return "", err
	}
	if err = graph.AddLambdaNode(agentnode.TaskQueryGraphNodeReflect, compose.InvokableLambda(nodes.Reflect)); err != nil {
		return "", err
	}
	if err = graph.AddEdge(compose.START, agentnode.TaskQueryGraphNodePlan); err != nil {
		return "", err
	}
	if err = graph.AddEdge(agentnode.TaskQueryGraphNodePlan, agentnode.TaskQueryGraphNodeQuadrant); err != nil {
		return "", err
	}
	if err = graph.AddEdge(agentnode.TaskQueryGraphNodeQuadrant, agentnode.TaskQueryGraphNodeTimeAnchor); err != nil {
		return "", err
	}
	if err = graph.AddEdge(agentnode.TaskQueryGraphNodeTimeAnchor, agentnode.TaskQueryGraphNodeQuery); err != nil {
		return "", err
	}
	if err = graph.AddEdge(agentnode.TaskQueryGraphNodeQuery, agentnode.TaskQueryGraphNodeReflect); err != nil {
		return "", err
	}
	if err = graph.AddBranch(agentnode.TaskQueryGraphNodeReflect, compose.NewGraphBranch(nodes.NextAfterReflect, map[string]bool{
		agentnode.TaskQueryGraphNodeQuery: true,
		compose.END:                       true,
	})); err != nil {
		return "", err
	}

	// 5. 反思节点支持按配置重试，因此最大步数需要覆盖“首次查询 + 多轮回看”的上限。
	maxSteps := 24 + input.State.MaxReflectRetry*4
	if maxSteps < 24 {
		maxSteps = 24
	}
	runnable, err := graph.Compile(ctx,
		compose.WithGraphName(TaskQueryGraphName),
		compose.WithMaxRunSteps(maxSteps),
		compose.WithNodeTriggerMode(compose.AnyPredecessor),
	)
	if err != nil {
		return "", err
	}
	finalState, err := runnable.Invoke(ctx, input.State)
	if err != nil {
		return "", err
	}
	if finalState == nil {
		return "", errors.New("task query graph: final state is nil")
	}

	// 6. 最终回复为空时给一个稳定兜底，避免上层拿到空字符串后再次拼接出异常文案。
	reply := strings.TrimSpace(finalState.FinalReply)
	if reply == "" {
		reply = "我这边暂时没整理出稳定结果，你可以换一个更具体的筛选条件再试一次。"
	}
	return reply, nil
}
