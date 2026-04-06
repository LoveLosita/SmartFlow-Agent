package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const executeSystemPrompt = `
你是 SmartFlow NewAgent 的执行器。
你的职责是在“当前 plan 步骤”的约束下，进行思考、执行、观察，再决定下一步动作。

请遵守以下规则：
1. 只围绕当前步骤行动，不要擅自跳到其他 plan 步骤。
2. 只输出严格 JSON，不要输出 markdown，不要输出额外解释，不要在 JSON 外再补文字。
3. 只有当你确认当前步骤已经完成时，才输出 action=next_plan，且必须在 goal_check 中逐条对照 done_when 说明完成依据。
4. 只有当你确认整个任务已经完成时，才输出 action=done，且必须在 goal_check 中总结整体完成证据。
5. 如果执行当前步骤缺少关键上下文，且无法通过已有历史或工具补齐，输出 action=ask_user。
6. 不要伪造工具结果；如果尚未真正拿到观察结果，就不要假装已经完成。
7. goal_check 是你输出 next_plan / done 时的强制字段，禁止为空；必须显式地逐条对照 done_when，说明”哪些条件已满足、依据是什么”。

你会看到：
- 当前完整 plan
- 当前步骤
- 置顶上下文块
- 工具摘要
- 历史对话与历史观察

请把注意力聚焦在”当前步骤是否完成，以及下一步最合理的执行动作”上。
`

// BuildExecuteSystemPrompt 返回执行阶段系统提示词。
func BuildExecuteSystemPrompt() string {
	return strings.TrimSpace(executeSystemPrompt)
}

// BuildExecuteDecisionContractText 返回执行阶段的输出协议说明。
func BuildExecuteDecisionContractText() string {
	return strings.TrimSpace(fmt.Sprintf(`
输出协议（严格 JSON）：
- speak：给用户看的话
- action：只能是 %s / %s / %s / %s / %s
- reason：给后端和日志看的简短说明
- goal_check：输出 %s 或 %s 时必填，对照 done_when 逐条验证
- tool_call：输出 %s 时可附带写工具意图（需 confirm），输出 %s 时可附带读工具调用
- tool_call 格式：{"name": "工具名", "arguments": {...}}

合法示例：
{
  "speak": "我来查一下本周的安排。",
  "action": "%s",
  "reason": "需要先调用 get_overview 获取当前数据",
  "tool_call": {
    "name": "get_overview",
    "arguments": {}
  }
}

{
  "speak": "查询完成。",
  "action": "%s",
  "reason": "已拿到当前周课程列表",
  "goal_check": "已通过 get_overview 确认本周课程列表，满足完成条件"
}

{
  "speak": "",
  "action": "%s",
  "reason": "整个任务已完成"
}
`,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionAskUser,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
		newagentmodel.ExecuteActionConfirm,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionContinue,
		newagentmodel.ExecuteActionNextPlan,
		newagentmodel.ExecuteActionDone,
	))
}

// BuildExecuteMessages 组装执行阶段的 messages。
func BuildExecuteMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) []*schema.Message {
	return buildStageMessages(
		BuildExecuteSystemPrompt(),
		ctx,
		BuildExecuteUserPrompt(state),
	)
}

// BuildExecuteUserPrompt 构造执行阶段的用户提示词。
func BuildExecuteUserPrompt(state *newagentmodel.CommonState) string {
	var sb strings.Builder

	sb.WriteString("请继续当前任务的执行阶段。\n")
	sb.WriteString(renderStateSummary(state))
	sb.WriteString("\n")

	if state == nil || !state.HasPlan() {
		sb.WriteString("当前没有可执行的完整 plan，请不要盲目进入执行；如有需要请回退到规划阶段。\n")
		return strings.TrimSpace(sb.String())
	}

	if currentStep, ok := state.CurrentPlanStep(); ok {
		sb.WriteString("执行要求：\n")
		sb.WriteString("1. 始终围绕下面这个当前步骤行动。\n")
		sb.WriteString("2. 若当前步骤未完成，请继续思考-执行-观察循环。\n")
		sb.WriteString("3. 若当前步骤已完成，请输出 action=next_plan，并填写 goal_check 说明完成依据。\n")
		sb.WriteString("4. 若整个任务已完成，请输出 action=done，并填写 goal_check 总结整体证据。\n")
		sb.WriteString("5. 若缺少关键用户信息且现有上下文无法补足，请输出 action=ask_user。\n")
		sb.WriteString("6. 输出 next_plan 或 done 时，goal_check 不能为空，必须对照 done_when 逐条验证。\n")
		sb.WriteString("\n")
		sb.WriteString(BuildExecuteDecisionContractText())
		sb.WriteString("\n当前步骤正文：\n")
		sb.WriteString(strings.TrimSpace(currentStep.Content))
		sb.WriteString("\n")
		if strings.TrimSpace(currentStep.DoneWhen) != "" {
			sb.WriteString("\n当前步骤完成判定：\n")
			sb.WriteString(strings.TrimSpace(currentStep.DoneWhen))
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("当前 plan 已存在，但当前步骤索引无效；请不要擅自执行其他步骤。\n")
	}

	return strings.TrimSpace(sb.String())
}
