package newagentprompt

import (
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

// buildChatConversationMessage 生成 chat / deep_answer 共用的真实对话视图。
func buildChatConversationMessage(ctx *newagentmodel.ConversationContext) string {
	return buildConversationHistoryMessage(ctx, "真实对话记录")
}

// buildChatRoutingWorkspace 渲染 chat 路由节点的轻量补充区。
//
// 设计说明：
// 1. chat 只保留与路由判断直接相关的最小流程标记；
// 2. rough_build_done 仍需显式暴露，否则路由层会丢掉“不要重复粗排”的关键信号；
// 3. 不再展示轮次、阶段锚点、ReAct 摘要等 execute 专属信息。
func buildChatRoutingWorkspace(ctx *newagentmodel.ConversationContext) string {
	lines := []string{"路由补充："}
	if hasExecuteRoughBuildDone(ctx) {
		lines = append(lines, "- 已存在 rough_build_done；除非用户明确要求重新粗排，否则不要再次触发 rough_build。")
	} else {
		lines = append(lines, "- 暂无额外流程标记。")
	}
	return strings.Join(lines, "\n")
}

// buildDeepAnswerWorkspace 渲染 deep_answer 节点的轻量工作区。
func buildDeepAnswerWorkspace() string {
	return "回答补充：请直接延续最近对话，聚焦回答用户本轮问题。"
}
