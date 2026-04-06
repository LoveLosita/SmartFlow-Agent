package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const planSystemPrompt = `
你是 SmartFlow NewAgent 的规划器。
你的职责不是直接执行任务，而是先把用户意图拆成一组清晰、稳定、可逐步执行的自然语言计划，并严格按后端约定的 JSON 协议输出。

请遵守以下规则：
1. 只负责规划，不要假装已经调用了工具，也不要伪造执行结果。
2. 每一轮只推进一步规划；如果信息不足，应明确转成 ask_user，而不是继续硬猜。
3. 若当前计划仍不完整，就继续围绕当前任务补全计划，不要跳去执行细节。
4. 若你认为计划已经完整可执行，请返回 action=plan_done，并附带完整 plan_steps。
5. plan_steps 必须使用自然语言，便于后端将完整 plan 重新注入到后续上下文顶部。
6. 只输出 JSON，不要输出 markdown，不要输出额外解释，不要在 JSON 外再补文字。
7. 每次输出前先评估任务复杂度：simple（简单明确，无复杂依赖）、moderate（多步操作，需要一定推理）、complex（需要深度推理、多方案比较或复杂依赖关系）。
8. 根据复杂度判断 need_thinking：你是否需要深度思考才能生成高质量计划？当不确定时倾向于 false。

你会看到：
- 当前阶段与轮次信息
- 已有完整 plan（如果之前已经规划过）
- 当前步骤（如果已存在）
- 置顶上下文块
- 可用工具摘要
- 历史对话

请基于这些输入继续规划，而不是重复忽略既有 plan。
`

// BuildPlanSystemPrompt 返回规划阶段系统提示词。
func BuildPlanSystemPrompt() string {
	return strings.TrimSpace(planSystemPrompt)
}

// BuildPlanMessages 组装规划阶段的 messages。
//
// 职责边界：
// 1. 负责把 state + context 收敛成规划阶段模型输入；
// 2. 负责把置顶上下文和工具摘要放在 history 前面，降低模型跑偏概率；
// 3. 不负责解析模型输出，也不负责判断规划质量。
func BuildPlanMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext, userInput string) []*schema.Message {
	return buildStageMessages(
		BuildPlanSystemPrompt(),
		ctx,
		BuildPlanUserPrompt(state, userInput),
	)
}

// BuildPlanUserPrompt 构造规划阶段的用户提示词。
func BuildPlanUserPrompt(state *newagentmodel.CommonState, userInput string) string {
	var sb strings.Builder

	sb.WriteString("请继续当前任务的规划阶段。\n")
	sb.WriteString(renderStateSummary(state))
	sb.WriteString("\n")
	sb.WriteString("本轮目标：围绕当前任务继续规划，直到形成一份稳定、可执行的自然语言 plan，或在信息不足时明确追问用户。\n\n")
	sb.WriteString(BuildPlanDecisionContractText())
	sb.WriteString("\n")

	trimmedInput := strings.TrimSpace(userInput)
	if trimmedInput != "" {
		sb.WriteString("\n用户本轮输入：\n")
		sb.WriteString(trimmedInput)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// BuildPlanDecisionContractText 返回规划阶段的输出协议说明。
func BuildPlanDecisionContractText() string {
	return strings.TrimSpace(fmt.Sprintf(`
输出协议（严格 JSON）：
- speak：给用户看的话；若 action=%s，这里通常就是要追问用户的问题
- action：只能是 %s / %s / %s
- reason：给后端和日志看的简短说明
- complexity：任务复杂度，只能是 simple / moderate / complex
- need_thinking：是否需要深度思考才能生成高质量计划，只能是 true / false
- plan_steps：仅当 action=%s 时允许返回；返回时必须是完整计划，不是增量
- plan_steps[].content：步骤正文，必填
- plan_steps[].done_when：可选，建议写”什么情况下算这一步做完”

合法示例：
{
  “speak”: “我先把计划再收束一下。”,
  “action”: “%s”,
  “reason”: “当前信息已足够继续规划”,
  “complexity”: “moderate”,
  “need_thinking”: false
}

{
  “speak”: “你更希望我优先安排今天，还是按整周来规划？”,
  “action”: “%s”,
  “reason”: “当前时间范围仍不明确”,
  “complexity”: “simple”,
  “need_thinking”: false
}

{
  “speak”: “计划已经整理好了，我先给你确认一下。”,
  “action”: “%s”,
  “reason”: “当前计划已具备执行条件”,
  “complexity”: “simple”,
  “need_thinking”: false,
  “plan_steps”: [
    {
      “content”: “先确认本周可用时间范围”,
      “done_when”: “拿到明确的可用时间段列表”
    },
    {
      “content”: “基于可用时间生成执行安排”,
      “done_when”: “得到一份用户可确认的安排方案”
    }
  ]
}
`,
		newagentmodel.PlanActionAskUser,
		newagentmodel.PlanActionContinue,
		newagentmodel.PlanActionAskUser,
		newagentmodel.PlanActionDone,
		newagentmodel.PlanActionDone,
		newagentmodel.PlanActionContinue,
		newagentmodel.PlanActionAskUser,
		newagentmodel.PlanActionDone,
	))
}
