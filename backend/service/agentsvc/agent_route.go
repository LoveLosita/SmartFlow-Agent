package agentsvc

import (
	"context"

	agentrouter "github.com/LoveLosita/smartflow/backend/agent/router"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

// actionRoutingDecision 是 route 层分流结果在 agentsvc 的本地别名。
//
// 设计目的：
// 1. 让 AgentService 对 route 包保持“最小接触面”；
// 2. 后续若 route 包返回结构调整，只需改这个桥接文件。
type actionRoutingDecision = agentrouter.RoutingDecision

// decideActionRouting 决定当前请求走向哪条业务链路。
//
// 职责边界：
// 1. 只负责调用 route 包拿分流结论；
// 2. 不负责执行任何业务节点；
// 3. route 层失败会通过 RoutingDecision.RouteFailed 向上层显式暴露。
func (s *AgentService) decideActionRouting(ctx context.Context, selectedModel *ark.ChatModel, userMessage string) actionRoutingDecision {
	// 这里保留方法封装，是为了避免上层直接依赖 route 包，降低耦合。
	_ = s
	return agentrouter.DecideActionRouting(ctx, selectedModel, userMessage)
}
