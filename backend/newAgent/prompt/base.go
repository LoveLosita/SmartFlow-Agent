package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

// buildStageMessages 组装某个阶段通用的 messages。
//
// 步骤说明：
// 1. 先合并 context 自带 system prompt 与阶段 prompt，保证通用约束和阶段约束都生效；
// 2. 再把置顶上下文块和工具摘要补成 system message，尽量顶在 history 前面；
// 3. 最后追加历史消息与本轮 user prompt，保持"新约束在前、历史在后"的稳定顺序。
func buildStageMessages(stageSystemPrompt string, ctx *newagentmodel.ConversationContext, runtimeUserPrompt string) []*schema.Message {
	messages := make([]*schema.Message, 0, 4)

	mergedSystemPrompt := mergeSystemPrompts(ctx, stageSystemPrompt)
	if mergedSystemPrompt != "" {
		messages = append(messages, schema.SystemMessage(mergedSystemPrompt))
	}

	if pinnedText := renderPinnedBlocks(ctx); pinnedText != "" {
		messages = append(messages, schema.SystemMessage(pinnedText))
	}

	if toolText := renderToolSchemas(ctx); toolText != "" {
		messages = append(messages, schema.SystemMessage(toolText))
	}

	if ctx != nil {
		history := ctx.HistorySnapshot()
		if len(history) > 0 {
			// 兼容旧快照：裸 Tool 消息（无 ToolCallID）违反 OpenAI 兼容 API 格式约束，
			// 会触发 API 拒绝请求导致连接断开。
			// 这里将裸 Tool 消息降级为 User 消息，保证向后兼容。
			for i, msg := range history {
				if msg.Role == schema.Tool && msg.ToolCallID == "" {
					history[i] = &schema.Message{
						Role:    schema.User,
						Content: fmt.Sprintf("[工具执行结果]\n%s", msg.Content),
					}
				}
			}
			messages = append(messages, history...)
		}
	}

	runtimeUserPrompt = strings.TrimSpace(runtimeUserPrompt)
	if runtimeUserPrompt != "" {
		messages = append(messages, schema.UserMessage(runtimeUserPrompt))
	}

	return messages
}

// renderStateSummary 把当前流程状态渲染成简洁文本。
func renderStateSummary(state *newagentmodel.CommonState) string {
	if state == nil {
		return "当前状态：state 缺失，请先做兜底处理。"
	}

	var sb strings.Builder
	current, total := state.PlanProgress()

	sb.WriteString(fmt.Sprintf("当前阶段：%s\n", state.Phase))
	sb.WriteString(fmt.Sprintf("当前轮次：%d/%d\n", state.RoundUsed, state.MaxRounds))

	if !state.HasPlan() {
		sb.WriteString("当前完整 plan：暂无。\n")
		return sb.String()
	}

	sb.WriteString("当前完整 plan：\n")
	for i, step := range state.PlanSteps {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(step.Content)))
		if strings.TrimSpace(step.DoneWhen) != "" {
			sb.WriteString(fmt.Sprintf("   完成判定：%s\n", strings.TrimSpace(step.DoneWhen)))
		}
	}

	if step, ok := state.CurrentPlanStep(); ok {
		sb.WriteString(fmt.Sprintf("当前步骤进度：%d/%d\n", current, total))
		sb.WriteString("当前步骤内容：\n")
		sb.WriteString(strings.TrimSpace(step.Content))
		sb.WriteString("\n")
		if strings.TrimSpace(step.DoneWhen) != "" {
			sb.WriteString("当前步骤完成判定：\n")
			sb.WriteString(strings.TrimSpace(step.DoneWhen))
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("当前步骤进度：暂时无有效当前步骤。\n")
	}

	return sb.String()
}

// renderPinnedBlocks 把 ConversationContext 中的置顶块渲染成独立的 system 文本。
func renderPinnedBlocks(ctx *newagentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}

	blocks := ctx.PinnedBlocksSnapshot()
	if len(blocks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("以下是后端置顶注入的上下文，请优先遵守：\n")
	for _, block := range blocks {
		title := strings.TrimSpace(block.Title)
		if title == "" {
			title = strings.TrimSpace(block.Key)
		}
		if title != "" {
			sb.WriteString("【")
			sb.WriteString(title)
			sb.WriteString("】\n")
		}
		sb.WriteString(strings.TrimSpace(block.Content))
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// renderToolSchemas 把工具摘要渲染成独立文本块。
func renderToolSchemas(ctx *newagentmodel.ConversationContext) string {
	if ctx == nil {
		return ""
	}

	schemas := ctx.ToolSchemasSnapshot()
	if len(schemas) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("以下是当前可用工具摘要，仅供你在规划时参考能力边界：\n")
	for _, item := range schemas {
		name := strings.TrimSpace(item.Name)
		desc := strings.TrimSpace(item.Desc)
		schemaText := strings.TrimSpace(item.SchemaText)

		if name != "" {
			sb.WriteString("- 工具名：")
			sb.WriteString(name)
			sb.WriteString("\n")
		}
		if desc != "" {
			sb.WriteString("  说明：")
			sb.WriteString(desc)
			sb.WriteString("\n")
		}
		if schemaText != "" {
			sb.WriteString("  参数摘要：")
			sb.WriteString(schemaText)
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String())
}

func mergeSystemPrompts(ctx *newagentmodel.ConversationContext, stageSystemPrompt string) string {
	base := ""
	if ctx != nil {
		base = strings.TrimSpace(ctx.SystemPrompt)
	}
	stageSystemPrompt = strings.TrimSpace(stageSystemPrompt)

	switch {
	case base == "" && stageSystemPrompt == "":
		return ""
	case base == "":
		return stageSystemPrompt
	case stageSystemPrompt == "":
		return base
	default:
		return base + "\n\n" + stageSystemPrompt
	}
}
