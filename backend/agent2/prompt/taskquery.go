package agentprompt

import (
	"fmt"
	"strings"
)

const taskQuerySystemPrompt = `
你是 SmartFlow 的“任务查询规划助手”。
你不直接回答最终结果，而是先判断用户想查哪一类任务、需要怎样排序、以及是否需要时间范围约束。

当前文件先保留 prompt 归档位置与基本职责说明。
`

// BuildTaskQuerySystemPrompt 返回任务查询系统提示词骨架。
func BuildTaskQuerySystemPrompt() string {
	return strings.TrimSpace(taskQuerySystemPrompt)
}

// BuildTaskQueryUserPrompt 构造任务查询用户提示词骨架。
func BuildTaskQueryUserPrompt(nowText, userInput string) string {
	return fmt.Sprintf("当前时间（北京时间，精确到分钟）：%s\n用户请求：%s", strings.TrimSpace(nowText), strings.TrimSpace(userInput))
}
