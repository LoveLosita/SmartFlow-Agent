package newagentprompt

import (
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
)

// buildConversationHistoryMessage 将“真实对话流”渲染成节点可直接复用的 msg1。
//
// 职责边界：
// 1. 只负责把 user + assistant speak 组织成稳定文本；
// 2. 不拼接 tool_call / tool observation，这些不属于“真实对话”；
// 3. 不做长度裁剪，长度预算交给统一压缩层处理。
func buildConversationHistoryMessage(ctx *newagentmodel.ConversationContext, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = "真实对话记录"
	}

	lines := []string{title + "："}
	if ctx == nil {
		lines = append(lines, "暂无。")
		return strings.Join(lines, "\n")
	}

	turns := CollectConversationTurns(ctx.HistorySnapshot())
	if len(turns) == 0 {
		lines = append(lines, "暂无。")
		return strings.Join(lines, "\n")
	}

	for _, turn := range turns {
		lines = append(lines, turn.Role+": \""+turn.Content+"\"")
	}
	return strings.Join(lines, "\n")
}
