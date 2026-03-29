package newagentprompt

import (
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const (
	// ExecuteNextPlanSignal 表示“当前 plan 步骤已经完成，可以进入下一个步骤”。
	//
	// TODO(newagent/node): 后续 executeNode 识别到该信号后，调用 state.AdvanceStep() 或决定进入交付阶段。
	ExecuteNextPlanSignal = "[NEXT_PLAN]"

	// ExecuteDoneSignal 表示“整个任务已经完成，可以结束执行链路”。
	//
	// TODO(newagent/node): 后续 executeNode 识别到该信号后，调用 state.Done() 并进入 deliver。
	ExecuteDoneSignal = "[DONE]"

	// ExecuteAskUserSignal 表示“执行阶段缺关键信息，需要向用户追问”。
	//
	// TODO(newagent/node): 后续若你决定支持 ask_user，这里可作为统一控制信号继续扩展。
	ExecuteAskUserSignal = "[ASK_USER]"
)

const executeSystemPrompt = `
你是 SmartFlow NewAgent 的执行器。

你的职责是在“当前 plan 步骤”的约束下，进行思考、执行、观察，再决定下一步动作。

请遵守以下规则：
1. 只围绕当前步骤行动，不要擅自跳到其他 plan 步骤。
2. 只有当你确认当前步骤已经完成时，才输出 ` + "`" + `[NEXT_PLAN]` + "`" + `。
3. 只有当你确认整个任务已经完成时，才输出 ` + "`" + `[DONE]` + "`" + `。
4. 如果执行当前步骤缺少关键上下文，且无法通过已有历史或工具补齐，可以输出 ` + "`" + `[ASK_USER]` + "`" + `。
5. 不要伪造工具结果；如果尚未真正拿到观察结果，就不要假装已经完成。

你会看到：
- 当前完整 plan
- 当前步骤
- 置顶上下文块
- 工具摘要
- 历史对话与历史观察

请把注意力聚焦在“当前步骤是否完成，以及下一步最合理的执行动作”上。
`

// BuildExecuteSystemPrompt 返回执行阶段系统提示词。
func BuildExecuteSystemPrompt() string {
	return strings.TrimSpace(executeSystemPrompt)
}

// BuildExecuteMessages 组装执行阶段的 messages。
//
// 职责边界：
// 1. 负责收敛执行阶段需要的 system / pinned / history / runtime prompt；
// 2. 负责把“完整 plan + 当前步骤 + 控制信号”显式告知模型；
// 3. 不负责解析模型输出，也不负责真正调用工具。
//
// TODO(newagent/node): 后续 executeNode 应直接复用这个方法，而不是在节点内手拼执行提示词。
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
		sb.WriteString("3. 若当前步骤已完成，请输出 ")
		sb.WriteString(ExecuteNextPlanSignal)
		sb.WriteString("。\n")
		sb.WriteString("4. 若整个任务已完成，请输出 ")
		sb.WriteString(ExecuteDoneSignal)
		sb.WriteString("。\n")
		sb.WriteString("5. 若缺少关键用户信息且现有上下文无法补足，请输出 ")
		sb.WriteString(ExecuteAskUserSignal)
		sb.WriteString("。\n")
		sb.WriteString("\n当前步骤正文：\n")
		sb.WriteString(currentStep)
		sb.WriteString("\n")
	} else {
		sb.WriteString("当前 plan 已存在，但当前步骤索引无效；请不要擅自执行其他步骤。\n")
	}

	return strings.TrimSpace(sb.String())
}
