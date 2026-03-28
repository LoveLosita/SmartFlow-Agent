package agentprompt

import (
	"fmt"
	"strings"
)

const routeSystemPrompt = `
你是 SmartFlow 的一级路由助手。
你的职责不是回答用户，而是判断这条消息更适合走哪条能力链路。

当前 Agent 仍在逐批迁移阶段，因此这里只先保留 prompt 落点与职责说明。
真正迁移旧 route 提示词时，应把正式版本收敛到这里，而不是散落在 node 或 service 中。
`

// BuildRouteSystemPrompt 返回一级路由系统提示词。
func BuildRouteSystemPrompt() string {
	return strings.TrimSpace(routeSystemPrompt)
}

// BuildRouteUserPrompt 构造一级路由用户提示词。
func BuildRouteUserPrompt(userInput string) string {
	return fmt.Sprintf("用户输入：%s", strings.TrimSpace(userInput))
}
