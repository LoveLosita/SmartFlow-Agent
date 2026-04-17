package newagentnode

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

// logNodeLLMContext 将某个节点即将送入 LLM 的完整消息上下文按统一格式打印到日志。
//
// 步骤化说明：
// 1. 统一输出 stage / phase / chat / round，方便按一次请求内的多次 LLM 调用串联排查；
// 2. 完整展开 messages，不做截断，保证问题复现时能直接对照 prompt 组装结果；
// 3. 该函数只负责调试日志，不参与任何业务判断，也不修改上下文内容。
func logNodeLLMContext(
	stage string,
	phase string,
	flowState *newagentmodel.CommonState,
	messages []*schema.Message,
) {
	chatID := ""
	roundUsed := 0
	if flowState != nil {
		chatID = flowState.ConversationID
		roundUsed = flowState.RoundUsed
	}

	log.Printf(
		"[DEBUG] %s LLM context begin phase=%s chat=%s round=%d message_count=%d\n%s\n[DEBUG] %s LLM context end phase=%s chat=%s round=%d",
		stage,
		strings.TrimSpace(phase),
		chatID,
		roundUsed,
		len(messages),
		formatLLMMessagesForDebug(messages),
		stage,
		strings.TrimSpace(phase),
		chatID,
		roundUsed,
	)
}

// formatLLMMessagesForDebug 将本轮送入 LLM 的完整消息上下文展开成可读多行日志。
//
// 说明：
// 1. 按消息索引逐条输出，便于和上游上下文构造步骤逐项对齐；
// 2. 完整输出 content / reasoning_content / tool_calls / extra，不做截断；
// 3. 仅用于调试打点，不参与业务决策。
func formatLLMMessagesForDebug(messages []*schema.Message) string {
	if len(messages) == 0 {
		return "(empty messages)"
	}

	var sb strings.Builder
	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("----- message[%d] -----\n", i))
		if msg == nil {
			sb.WriteString("role: <nil>\n\n")
			continue
		}

		sb.WriteString(fmt.Sprintf("role: %s\n", msg.Role))

		if strings.TrimSpace(msg.ToolCallID) != "" {
			sb.WriteString(fmt.Sprintf("tool_call_id: %s\n", msg.ToolCallID))
		}
		if strings.TrimSpace(msg.ToolName) != "" {
			sb.WriteString(fmt.Sprintf("tool_name: %s\n", msg.ToolName))
		}

		if len(msg.ToolCalls) > 0 {
			sb.WriteString("tool_calls:\n")
			for j, call := range msg.ToolCalls {
				sb.WriteString(fmt.Sprintf("  - [%d] id=%s type=%s function=%s\n", j, call.ID, call.Type, call.Function.Name))
				sb.WriteString("    arguments:\n")
				sb.WriteString(indentMultilineForDebug(call.Function.Arguments, "      "))
				sb.WriteString("\n")
			}
		}

		if strings.TrimSpace(msg.ReasoningContent) != "" {
			sb.WriteString("reasoning_content:\n")
			sb.WriteString(indentMultilineForDebug(msg.ReasoningContent, "  "))
			sb.WriteString("\n")
		}

		sb.WriteString("content:\n")
		sb.WriteString(indentMultilineForDebug(msg.Content, "  "))
		sb.WriteString("\n")

		if len(msg.Extra) > 0 {
			sb.WriteString("extra:\n")
			raw, err := json.MarshalIndent(msg.Extra, "", "  ")
			if err != nil {
				sb.WriteString(indentMultilineForDebug("<marshal_error>", "  "))
			} else {
				sb.WriteString(indentMultilineForDebug(string(raw), "  "))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("\n")
	}
	return sb.String()
}

// indentMultilineForDebug 为多行文本统一添加前缀缩进，避免日志折行后难以阅读。
func indentMultilineForDebug(text, prefix string) string {
	if text == "" {
		return prefix + "<empty>"
	}
	lines := strings.Split(text, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
