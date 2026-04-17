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
2. 如果所有步骤都已完成，请自然概括每一步的主要成果。
3. 如果流程因轮次耗尽或主动终止而提前结束，请如实说明当前进度与未完成部分。
4. 使用自然、友好的语气，不要机械罗列工具过程。
5. 如果用户后续还需要继续操作，可以给出一句简短建议。
6. 只输出总结文本，不要输出 JSON，也不要输出 markdown 标题。

你会看到：
- 原始计划步骤及完成进度
- 最近真实对话
- 当前流程的收口状态
`

// BuildDeliverSystemPrompt 返回交付阶段系统提示词。
func BuildDeliverSystemPrompt() string {
	return strings.TrimSpace(deliverSystemPrompt)
}

// BuildDeliverMessages 组装交付阶段 messages。
func BuildDeliverMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) []*schema.Message {
	roughBuildPrefix := buildDeliverRoughBuildPrefix(ctx, state)
	return buildUnifiedStageMessages(
		ctx,
		StageMessagesConfig{
			SystemPrompt: BuildDeliverSystemPrompt(),
			Msg1Content:  buildDeliverConversationMessage(ctx),
			Msg2Content:  buildDeliverWorkspace(state),
			Msg3Prefix:   roughBuildPrefix,
			Msg3Suffix:   BuildDeliverUserPrompt(state, ctx),
			Msg3Role:     schema.User,
		},
	)
}

// BuildDeliverUserPrompt 构造交付阶段的用户提示词。
func BuildDeliverUserPrompt(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext) string {
	var sb strings.Builder

	sb.WriteString("请基于最近对话和交付工作区，生成一段自然、诚实的完成总结。\n")

	if state == nil || !state.HasPlan() {
		if hasExecuteRoughBuildDone(ctx) {
			sb.WriteString("当前没有正式计划，但本轮已经完成粗排，请结合粗排补充和任务类详情总结粗排结果，不要把它说成正式完结。\n")
		} else {
			sb.WriteString("当前没有正式计划，请只概括本次互动，不要编造成果。\n")
		}
		return strings.TrimSpace(sb.String())
	}

	completed := countCompletedPlanSteps(state)
	total := len(state.PlanSteps)

	if state.IsExhaustedTerminal() {
		sb.WriteString(fmt.Sprintf("注意：任务因轮次耗尽提前结束，当前已完成 %d/%d 步。\n", completed, total))
		sb.WriteString("请如实说明已完成与未完成的部分，并给出一句继续建议。\n")
		return strings.TrimSpace(sb.String())
	}

	if state.IsAborted() {
		sb.WriteString(fmt.Sprintf("注意：流程已被主动终止，当前已完成 %d/%d 步。\n", completed, total))
		sb.WriteString("请如实说明停在何处，以及用户若想继续应如何衔接。\n")
		return strings.TrimSpace(sb.String())
	}

	sb.WriteString("若计划已正常完成，请概括整体成果；若仍有未完成步骤，也必须如实说明。\n")
	return strings.TrimSpace(sb.String())
}
