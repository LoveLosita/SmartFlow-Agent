package agentprompt

import (
	"context"
	"fmt"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/cloudwego/eino/schema"
)

const compactMsg1SystemPrompt = `你是一个对话压缩助手。你的任务是将以下多轮对话历史压缩为一段简洁的结构化摘要。

要求：
1. 保留用户的核心诉求和意图（原文关键词不要丢失）
2. 保留所有已确认的约束条件和规则
3. 保留关键操作决策和结果（比如排程相关的调整结果）
4. 保留用户偏好的重要信息
5. 去除冗余和重复信息
6. 按要点列出，每条一行

直接输出压缩后的摘要，不要输出其他内容。`

// CompactMsg1 将 msg1（历史对话）压缩为摘要。
// existingSummary 不为空时表示已有旧摘要，需要合并压缩。
func CompactMsg1(
	ctx context.Context,
	client *llmservice.Client,
	historyText string,
	existingSummary string,
) (string, error) {
	var userContent string
	if existingSummary != "" {
		userContent = fmt.Sprintf(`已有压缩摘要：
%s

新增的对话记录：
%s

请将以上两部分合并为一份更紧凑的摘要。`, existingSummary, historyText)
	} else {
		userContent = fmt.Sprintf(`对话历史：
%s

请压缩以上对话历史。`, historyText)
	}

	messages := []*schema.Message{
		schema.SystemMessage(compactMsg1SystemPrompt),
		schema.UserMessage(userContent),
	}

	result, err := client.GenerateText(ctx, messages, llmservice.GenerateOptions{
		MaxTokens: 4000,
	})
	if err != nil {
		return "", fmt.Errorf("compact msg1 LLM call failed: %w", err)
	}
	if result == nil || result.Text == "" {
		return "", fmt.Errorf("compact msg1 LLM returned empty result")
	}
	return result.Text, nil
}
