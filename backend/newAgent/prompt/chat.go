package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const chatRoutingSystemPrompt = `
你是 SmartFlow 的智能路由器。你的职责是判断用户意图的复杂度，并决定后续处理路径。

你会看到：
- 历史对话
- 用户本轮输入
- 当前可用工具摘要（如有）
- 本次排课涉及的任务类约束（如有）

请遵守以下规则：
1. 只输出严格 JSON，不要输出 markdown，不要输出额外解释。
2. 根据用户意图判断复杂度并选择路由。
3. speak 字段始终填写：给用户看的话。

路由规则：
- direct_reply：纯闲聊、简单问答、打招呼、感谢等。speak 直接写你的完整回复。
- execute：需要用工具处理的请求（查询日程、移动课程、排课等），但不需要先制定计划。speak 写简短确认。
- deep_answer：复杂问题但不需要工具（如分析建议、深度解释等），需要深度思考后直接回答。speak 写过渡语（如"让我想想"）。
- plan：用户明确要求先制定计划，或涉及多阶段复杂规划。speak 写确认语。

粗排判断：当用户意图包含"批量安排/排课/把任务类排进日程"，且上下文中有任务类 ID 时，设置 needs_rough_build=true。

输出协议（严格 JSON）：
{"route":"direct_reply / execute / deep_answer / plan","speak":"给用户看的话","needs_rough_build":false,"reason":"简短判断依据"}

合法示例：

{"route":"direct_reply","speak":"你好！我是 SmartFlow 助手，有什么可以帮你的？","reason":"用户打招呼"}

{"route":"execute","speak":"好的，我来帮你看看今天的安排。","reason":"需要调用工具查询日程","needs_rough_build":false}

{"route":"execute","speak":"好的，我来帮你排课。","reason":"批量排课需求，有任务类 ID","needs_rough_build":true}

{"route":"deep_answer","speak":"这是个好问题，让我仔细想想。","reason":"需要深度分析但不需要工具"}

{"route":"plan","speak":"明白，我来帮你制定一个完整的学习计划。","reason":"用户明确要求制定计划"}
`

// BuildChatRoutingSystemPrompt 返回路由阶段的系统提示词。
func BuildChatRoutingSystemPrompt() string {
	return strings.TrimSpace(chatRoutingSystemPrompt)
}

// BuildChatRoutingMessages 组装路由阶段的 messages。
func BuildChatRoutingMessages(ctx *newagentmodel.ConversationContext, userInput string, state *newagentmodel.CommonState) []*schema.Message {
	return buildStageMessages(
		BuildChatRoutingSystemPrompt(),
		ctx,
		BuildChatRoutingUserPrompt(ctx, userInput, state),
	)
}

// BuildChatRoutingUserPrompt 构造路由阶段的用户提示词。
func BuildChatRoutingUserPrompt(ctx *newagentmodel.ConversationContext, userInput string, state *newagentmodel.CommonState) string {
	var sb strings.Builder

	sb.WriteString("请判断用户本轮意图的复杂度，并选择最合适的路由。\n")

	// 注入任务类上下文（供粗排判断参考）。
	if state != nil && len(state.TaskClassIDs) > 0 {
		parts := make([]string, len(state.TaskClassIDs))
		for i, id := range state.TaskClassIDs {
			parts[i] = fmt.Sprintf("%d", id)
		}
		sb.WriteString(fmt.Sprintf("\n本次请求涉及的任务类 ID：[%s]\n", strings.Join(parts, ", ")))
	}

	if state != nil && len(state.TaskClasses) > 0 {
		sb.WriteString("任务类约束：\n")
		for _, tc := range state.TaskClasses {
			line := fmt.Sprintf("- [ID=%d] %s：策略=%s，总时段预算=%d", tc.ID, tc.Name, tc.Strategy, tc.TotalSlots)
			if tc.StartDate != "" || tc.EndDate != "" {
				line += fmt.Sprintf("，日期范围=%s ~ %s", tc.StartDate, tc.EndDate)
			}
			sb.WriteString(line + "\n")
		}
	}

	trimmedInput := strings.TrimSpace(userInput)
	if trimmedInput != "" {
		sb.WriteString("\n用户本轮输入：\n")
		sb.WriteString(trimmedInput)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// --- 深度回答 prompt ---

const deepAnswerSystemPrompt = `
你是 SmartFlow 的深度分析助手。用户提出了一个需要深入思考的问题，请认真分析后给出详细、有价值的回答。

请遵守以下规则：
1. 充分利用上下文中已有的信息（任务类约束、日程数据、历史对话等）。
2. 如果缺少关键信息，在回答中说明需要哪些额外信息。
3. 直接输出你的回答，不要输出 JSON。
`

// BuildDeepAnswerSystemPrompt 返回深度回答阶段的系统提示词。
func BuildDeepAnswerSystemPrompt() string {
	return strings.TrimSpace(deepAnswerSystemPrompt)
}

// BuildDeepAnswerMessages 组装深度回答阶段的 messages。
func BuildDeepAnswerMessages(ctx *newagentmodel.ConversationContext, userInput string) []*schema.Message {
	return buildStageMessages(
		BuildDeepAnswerSystemPrompt(),
		ctx,
		userInput,
	)
}
