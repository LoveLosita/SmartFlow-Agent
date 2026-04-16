package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const deliverSystemPrompt = `
你是 SmartMate 的交付器。
你的职责是基于原始计划和执行历史，生成一份简洁、诚实的任务完成总结。

请遵守以下规则：
1. 只基于已有历史和计划状态生成总结，不要编造未执行的操作。
2. 如果所有步骤都已完成，简要总结每一步的成果。
3. 如果是因轮次耗尽提前结束，如实告知用户当前进度和未完成的部分。
4. 使用自然、友好的语气，不要机械地罗列步骤。
5. 如果用户后续可能需要继续操作，给出简短的建议。
6. 只输出总结文本，不要输出 JSON，不要输出 markdown 标题。

你会看到：
- 原始计划步骤及完成判定
- 当前执行进度
- 执行阶段的对话历史
`

// BuildDeliverSystemPrompt 返回交付阶段系统提示词。
func BuildDeliverSystemPrompt() string {
	return strings.TrimSpace(deliverSystemPrompt)
}

// BuildDeliverMessages 组装交付阶段的 messages。
func BuildDeliverMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) []*schema.Message {
	return buildStageMessages(
		BuildDeliverSystemPrompt(),
		ctx,
		BuildDeliverUserPrompt(state),
	)
}

// BuildDeliverUserPrompt 构造交付阶段的用户提示词。
func BuildDeliverUserPrompt(state *newagentmodel.CommonState) string {
	var sb strings.Builder

	sb.WriteString("请为当前任务生成完成总结。\n")
	sb.WriteString(renderStateSummary(state))
	sb.WriteString("\n")

	if state == nil || !state.HasPlan() {
		sb.WriteString("当前没有正式计划，请基于对话历史简要总结本次交互。\n")
		return strings.TrimSpace(sb.String())
	}

	current, total := state.PlanProgress()
	exhausted := state.Exhausted()

	if exhausted {
		sb.WriteString(fmt.Sprintf("注意：任务因轮次耗尽提前结束，当前进度 %d/%d。\n", current, total))
		sb.WriteString("请如实说明已完成和未完成的部分，并建议用户如何继续。\n")
	} else {
		sb.WriteString("所有计划步骤已执行完毕，请总结整体成果。\n")
	}

	return strings.TrimSpace(sb.String())
}
