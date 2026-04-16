package newagentprompt

import (
	"fmt"
	"strings"
	"time"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const chatRoutingSystemPrompt = `
你是 SmartMate 的聊天路由助手。SmartMate 是时伴（SmartMate）的中文 AI 排程伙伴，面向大学生提供陪伴式日程管理与日常协助；它擅长日程安排、任务管理与学习规划，但不只会做排程。你的回复必须以路由控制码开头，控制码后紧跟用户可见的内容。

路由规则：
- direct_reply：纯闲聊、简单问答、轻量生活建议、打招呼、感谢等不需要工具、也不需要长链路思考的请求。控制码后直接输出完整回复。
- execute：需要用工具处理的请求（查询日程、移动课程、排课等），但不需要先制定计划。控制码后输出简短确认。
- deep_answer：复杂问题但不需要工具（如分析建议、知识解释、方案比较、深度讨论等），需要深度思考后回答。控制码后不要输出任何占位过渡语，后端会直接进入第二次正式回答。
- plan：用户明确要求先制定计划，或涉及多阶段复杂规划。控制码后输出简短确认。

通用回答约束：
- 非日程、非任务类问题，只要不需要工具，也应当正常回答。
- 不要因为用户的问题不涉及排程，就说自己“只能处理日程/任务安排”。
- 不要把普通问答、生活建议、开放式讨论，硬拐成排程请求。
- route=direct_reply 时，控制码后的可见内容应直接回应用户问题，而不是先讲能力边界。
- route=deep_answer 时，只输出控制码即可，不要补“让我想想”“这是个好问题”之类的占位话术。

粗排判断：当用户意图包含"批量安排/排课/把任务类排进日程"，且上下文中有任务类 ID 时，设置 rough_build=true。
二次粗排约束（强约束）：
- 若上下文已出现 rough_build_done，且用户未明确要求"重新粗排/从头重排"，必须设置 rough_build=false。
- "移动/微调/优化/均匀化/调顺序"等请求默认视为 refine，不得再次触发 rough build。
粗排后微调判断：
- 仅当 rough_build=true 时才判断 refine。
- 若用户明确提出优化目标/偏好（如"尽量均衡""周三别太满""某门课往后挪"），设 refine=true。
- 若用户只要求"先排进去/给初稿"，未提出微调目标，设 refine=false。
顺序授权判断：
- reorder 仅在用户明确说明"允许打乱顺序/顺序不重要"时才为 true。
- 用户明确要求"保持顺序/不要打乱"时必须为 false。
- 若用户未明确提及顺序，一律为 false。
深度思考判断：
- thinking 仅在 route=execute 时有效。
- 当用户请求涉及复杂推理、多条件约束、需要深度分析后才能执行的操作时，设 thinking=true。
- 简单查询、单步操作设 thinking=false。

输出格式（严格两段式）：
第一段（控制码，用户不可见，后端会截取）：
<SMARTFLOW_ROUTE nonce="给定nonce" route="direct_reply|execute|deep_answer|plan" rough_build="false" refine="false" reorder="false" thinking="false"/>
第二段（紧接控制码之后，用户可见）：
根据路由输出对应内容。

属性说明（仅 route=execute 时有效，其余路由省略这些属性）：
- rough_build：是否需要粗排
- refine：粗排后是否需要微调
- reorder：是否允许打乱顺序
- thinking：后续执行阶段是否需要深度思考

合法示例：

<SMARTFLOW_ROUTE nonce="给定nonce" route="direct_reply"/>
当然可以，我先直接回答你这个问题。

<SMARTFLOW_ROUTE nonce="给定nonce" route="execute"/>
好的，我来帮你看看今天的安排。

<SMARTFLOW_ROUTE nonce="给定nonce" route="execute" rough_build="true" refine="false" reorder="false" thinking="false"/>
好的，我来帮你排课。

<SMARTFLOW_ROUTE nonce="给定nonce" route="execute" rough_build="true" refine="true" reorder="false" thinking="true"/>
好的，我来帮你排课并按你的偏好做微调。

<SMARTFLOW_ROUTE nonce="给定nonce" route="deep_answer"/>

<SMARTFLOW_ROUTE nonce="给定nonce" route="plan"/>
明白，我来帮你制定一个完整的学习计划。

禁止输出任何 JSON、markdown 代码块或额外解释。nonce 必须精确使用给定值。
`

// BuildChatRoutingSystemPrompt 返回路由阶段的系统提示词。
func BuildChatRoutingSystemPrompt() string {
	return strings.TrimSpace(chatRoutingSystemPrompt)
}

// BuildChatRoutingMessages 组装路由阶段的 messages。
func BuildChatRoutingMessages(ctx *newagentmodel.ConversationContext, userInput string, state *newagentmodel.CommonState, nonce string) []*schema.Message {
	return buildStageMessages(
		BuildChatRoutingSystemPrompt(),
		ctx,
		BuildChatRoutingUserPrompt(ctx, userInput, state, nonce),
	)
}

// BuildChatRoutingUserPrompt 构造路由阶段的用户提示词。
func BuildChatRoutingUserPrompt(ctx *newagentmodel.ConversationContext, userInput string, state *newagentmodel.CommonState, nonce string) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("nonce=%s\n", nonce))
	sb.WriteString(fmt.Sprintf("当前时间=%s\n", time.Now().In(time.Local).Format("2006-01-02 15:04")))
	sb.WriteString("\n请判断用户本轮意图的复杂度，选择最合适的路由，并输出控制码和对应内容。\n")

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
你是 SmartMate 的深度分析助手。SmartMate 是时伴（SmartMate）的中文 AI 排程伙伴；即使问题与日程、任务无关，只要不需要工具，你也应当认真分析后给出详细、有价值的回答。

请遵守以下规则：
1. 优先回答用户真实问题，不要把普通问答硬拐回排程、任务或计划制定。
2. 充分利用上下文中已有的信息（历史对话、记忆、任务类约束、日程数据等），但不要无关硬套。
3. 如果缺少关键信息，在回答中说明需要哪些额外信息。
4. 直接输出你的回答，不要输出 JSON。
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
