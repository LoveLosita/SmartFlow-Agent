package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const planSystemPrompt = `
你是 SmartMate 的规划器。
你的职责不是直接执行任务，而是先把用户意图拆成一组清晰、稳定、可逐步执行的自然语言计划，并严格按后端约定的 JSON 协议输出。

请遵守以下规则：
1. 只负责规划，不要假装已经调用了工具，也不要伪造执行结果。
2. 每一轮只推进一步规划；如果信息不足，应明确转成 ask_user，而不是继续硬猜。
3. 若当前计划仍不完整，就继续围绕当前任务补全计划，不要跳去执行细节。
4. 若你认为计划已经完整可执行，请返回 action=plan_done，并附带完整 plan_steps。
5. plan_steps 必须使用自然语言，便于后端将完整 plan 重新注入到后续上下文顶部。
6. 输出格式：先输出一行 <SMARTFLOW_DECISION>{JSON 决策}</SMARTFLOW_DECISION>，然后换行输出给用户看的自然语言正文。JSON 中不要包含 speak 字段——用户可见的话放在标签之后。
7. 每次输出前先评估任务复杂度：simple（简单明确，无复杂依赖）、moderate（多步操作，需要一定推理）、complex（需要深度推理、多方案比较或复杂依赖关系）。
8. 粗排识别规则：若满足以下两个条件，在 action=plan_done 时附加 needs_rough_build=true 和 task_class_ids：
   条件1：用户输入中存在"任务类 ID"字段（见上下文"任务类 ID"部分）；
   条件2：用户意图明确是"批量安排/帮我排课/把任务类排进日程"等批量调度需求。
   满足时：后端会在用户确认计划后自动运行粗排算法（硬性约束已由算法保证，无需 LLM 校验）。
   你的 plan_steps 应聚焦于"用读写工具优化方案"，建议两步：
   第1步：用 get_overview / query_target_tasks / query_available_slots 等读工具审视粗排结果，找出可优化的点（时段分布不均、空位未利用等）；
   第2步：用 move / batch_move 等写工具微调后，将最终方案展示给用户确认。
   禁止安排任何"校验/验证约束"步骤——硬性约束由算法兜底，LLM 不需要操心。

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
// 1. 负责把 state + context 收敛成统一 4 段式规划阶段模型输入；
// 2. 不负责解析模型输出，也不负责判断规划质量；
// 3. msg3 中的状态文本由本函数显式传入，确保统一骨架下仍能看到完整计划与阶段信息。
func BuildPlanMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext, userInput string) []*schema.Message {
	return buildUnifiedStageMessages(
		ctx,
		StageMessagesConfig{
			SystemPrompt: BuildPlanSystemPrompt(),
			Msg1Content:  buildPlanConversationMessage(ctx),
			Msg2Content:  buildPlanWorkspace(state),
			Msg3Suffix:   BuildPlanUserPrompt(state, userInput),
			Msg3Role:     schema.User,
		},
	)
}

// BuildPlanUserPrompt 构造规划阶段的用户提示词。
func BuildPlanUserPrompt(state *newagentmodel.CommonState, userInput string) string {
	var sb strings.Builder

	sb.WriteString("请继续当前任务的规划阶段，严格按 SMARTFLOW_DECISION 标签格式输出。\n")
	sb.WriteString("目标：围绕最近对话和规划工作区信息，产出一份稳定、可执行的自然语言计划；若关键信息不足，请明确 ask_user。\n\n")
	sb.WriteString(BuildPlanDecisionContractText())

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
输出协议（两阶段格式）：

先输出一行决策标签，标签内是 JSON；标签之后换行输出给用户看的自然语言正文。
决策标签格式：<SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION>

JSON 字段说明：
- action：只能是 %s / %s / %s
- reason：给后端和日志看的简短说明
- complexity：任务复杂度，只能是 simple / moderate / complex
- plan_steps：仅当 action=%s 时允许返回；返回时必须是完整计划，不是增量
- plan_steps[].content：步骤正文，必填
- plan_steps[].done_when：可选，建议写"什么情况下算这一步做完"
- needs_rough_build：仅当满足粗排识别规则时为 true，否则省略；为 true 时后端自动运行粗排算法
- task_class_ids：needs_rough_build=true 时必填，从上下文"任务类 ID"字段读取

注意：JSON 中不要包含 speak 字段。给用户看的话放在 </SMARTFLOW_DECISION> 标签之后。

合法示例：

<SMARTFLOW_DECISION>{"action":"%s","reason":"当前信息已足够继续规划","complexity":"moderate"}</SMARTFLOW_DECISION>
我先把计划再收束一下。

<SMARTFLOW_DECISION>{"action":"%s","reason":"当前时间范围仍不明确","complexity":"simple"}</SMARTFLOW_DECISION>
你更希望我优先安排今天，还是按整周来规划？

<SMARTFLOW_DECISION>{"action":"%s","reason":"当前计划已具备执行条件","complexity":"simple","plan_steps":[{"content":"先确认本周可用时间范围","done_when":"拿到明确的可用时间段列表"},{"content":"基于可用时间生成执行安排","done_when":"得到一份用户可确认的安排方案"}]}</SMARTFLOW_DECISION>
计划已经整理好了，我先给你确认一下。
`,
		newagentmodel.PlanActionContinue,
		newagentmodel.PlanActionAskUser,
		newagentmodel.PlanActionDone,
		newagentmodel.PlanActionDone,
		newagentmodel.PlanActionContinue,
		newagentmodel.PlanActionAskUser,
		newagentmodel.PlanActionDone,
	))
}
