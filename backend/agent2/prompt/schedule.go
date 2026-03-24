package agentprompt

import (
	"fmt"
	"strings"
)

const scheduleSystemPrompt = `
你是 SmartFlow 的智能排程助手。
你的职责是把用户的排程目标转成结构化计划，并与后端粗排/硬校验能力配合完成排程。

当前 agent2 还没正式迁移 schedule 相关旧代码，
因此这里先定义 prompt 收口点，确保后面迁移时不会再把大段提示词散写回 nodes.go。
`

// BuildScheduleSystemPrompt 返回排程系统提示词骨架。
func BuildScheduleSystemPrompt() string {
	return strings.TrimSpace(scheduleSystemPrompt)
}

// BuildScheduleUserPrompt 构造排程用户提示词骨架。
func BuildScheduleUserPrompt(nowText, userInput string) string {
	return fmt.Sprintf("当前时间（北京时间，精确到分钟）：%s\n用户请求：%s", strings.TrimSpace(nowText), strings.TrimSpace(userInput))
}
