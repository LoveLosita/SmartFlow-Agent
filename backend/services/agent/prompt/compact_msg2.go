package agentprompt

import (
	"context"
	"fmt"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/cloudwego/eino/schema"
)

const compactMsg2SystemPrompt = `你是一个执行记录压缩助手。你的任务是将以下 ReAct 执行循环记录压缩为简洁摘要。

要求：
1. 保留每个工具调用的关键返回值（尤其是包含排程数据的JSON）
2. 保留执行路径（哪些操作成功了，哪些失败了）
3. 保留当前执行进度（正在做什么，下一步要做什么）
4. 去除重复的工具调用结果
5. 按时间顺序组织，每条一行

直接输出压缩后的摘要，不要输出其他内容。`

// CompactMsg2 将 msg2（ReAct Loop 记录）的早期部分压缩为摘要。
// recentText 是保留的近期记录原文，不参与压缩。
func CompactMsg2(
	ctx context.Context,
	client *llmservice.Client,
	earlyLoopText string,
) (string, error) {
	userContent := fmt.Sprintf(`早期的 ReAct 执行记录：
%s

请压缩以上执行记录，保留关键信息。`, earlyLoopText)

	messages := []*schema.Message{
		schema.SystemMessage(compactMsg2SystemPrompt),
		schema.UserMessage(userContent),
	}

	result, err := client.GenerateText(ctx, messages, llmservice.GenerateOptions{
		MaxTokens: 4000,
	})
	if err != nil {
		return "", fmt.Errorf("compact msg2 LLM call failed: %w", err)
	}
	if result == nil || result.Text == "" {
		return "", fmt.Errorf("compact msg2 LLM returned empty result")
	}
	return result.Text, nil
}
