package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const planSystemPromptCore = `
你是 SmartMate 的规划器（Planner），只负责规划，不负责执行。

最高优先级规则：
1. 意图边界：只规划用户当前明确要求，禁止擅自扩展后续动作。
2. 事实边界：禁止伪造工具调用和执行结果。

规划规则：
1. 每轮只做一次决策（continue / ask_user / plan_done）。
2. 信息足够时优先 plan_done；信息不足时才 ask_user，且只问最小必要问题。
3. action=plan_done 时必须返回完整 plan_steps（不是增量）。
4. plan_steps 使用自然语言描述目标与完成判定，不写执行结果。
5. 若意图满足批量排程识别条件，可在 plan_done 时附加 needs_rough_build 与 task_class_ids。
6. 可在 plan_done 时附加 context_hook（执行阶段注入建议）；规划阶段禁止调用 context_tools_add/remove。`

// BuildPlanSystemPrompt 返回规划阶段系统提示词。
func BuildPlanSystemPrompt() string {
	parts := []string{
		strings.TrimSpace(planSystemPromptCore),
		BuildPlanDecisionContractText(),
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// BuildPlanMessages 组装规划阶段的 messages。
//
// 1. 规划阶段只保留 Planner 专用规则，跳过通用人格底座，避免角色指令冲突。
// 2. msg1 展示真实对话，msg2 展示规划工作区，msg3 仅给最小执行指令与用户本轮输入。
// 3. 工具目录使用轻量版，仅提供“有什么工具”，不注入执行态大段参数示例。
func BuildPlanMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext, userInput string) []*schema.Message {
	return buildUnifiedStageMessages(
		ctx,
		StageMessagesConfig{
			SystemPrompt:          BuildPlanSystemPrompt(),
			Msg1Content:           buildPlanConversationMessage(ctx),
			Msg2Content:           buildPlanWorkspace(state),
			Msg3Suffix:            BuildPlanUserPrompt(state, userInput),
			Msg3Role:              schema.User,
			SkipBaseSystemPrompt:  true,
			UseLiteToolCatalogMsg: true,
		},
	)
}

// BuildPlanUserPrompt 构造规划阶段的用户提示词。
func BuildPlanUserPrompt(state *newagentmodel.CommonState, userInput string) string {
	var sb strings.Builder

	sb.WriteString("请继续当前任务规划，只输出一组 SMARTFLOW_DECISION 决策。\n")
	sb.WriteString("请基于最近对话与规划工作区推进，不要重复已有计划内容。\n")
	sb.WriteString("输出格式与字段约束严格按 msg0 协议执行。\n")

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
输出协议（唯一口径）：
1. 先输出：<SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION>
2. 再输出：给用户看的自然语言正文

JSON 字段：
- action：只能是 %s / %s / %s
- reason：给后端和日志看的简短说明
- complexity：只能是 simple / moderate / complex
- plan_steps：仅当 action=%s 时允许返回，且必须是完整计划
- plan_steps[].content：步骤正文，必填
- plan_steps[].done_when：可选，建议写完成判定
- needs_rough_build：仅满足粗排识别条件时为 true，否则省略
- task_class_ids：needs_rough_build=true 时必填，从上下文读取
- context_hook：可选，仅用于给 execute 阶段提供注入建议
- context_hook.domain：schedule / taskclass
- context_hook.packs：string 数组，可选；core 固定注入，不要填写 core
- context_hook.reason：可选，说明为何建议该注入

注意：
- JSON 中不要包含 speak 字段
- 不要在 planning 阶段调用任何工具（包括 context_tools_add/remove）`,
		newagentmodel.PlanActionContinue,
		newagentmodel.PlanActionAskUser,
		newagentmodel.PlanActionDone,
		newagentmodel.PlanActionDone,
	))
}
