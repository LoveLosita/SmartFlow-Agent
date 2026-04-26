package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

// buildStageMessages 组装某个阶段通用的 messages。
//
// 消息排列策略（利用 LLM 近因效应）：
//  1. system prompt（角色 + 阶段规则）— 始终最顶部，定义基本身份；
//  2. tool schemas（能力边界）— 稳定参考信息，放在 history 前即可；
//  3. history（对话历史、工具调用、修正反馈）— 按时间顺序排列；
//  4. pinned blocks（当前计划、当前步骤、粗排结果等最新约束）— 紧贴 user prompt，
//     利用近因效应让 LLM 优先关注本轮最相关的约束，而非被历史消息分散注意力；
//  5. user prompt（阶段性指令）— 始终在末尾，是本轮回答的核心触发。
func buildStageMessages(stageSystemPrompt string, ctx *newagentmodel.ConversationContext, runtimeUserPrompt string) []*schema.Message {
	messages := make([]*schema.Message, 0, 4)

	// 1. 合并 system prompt：基础角色约束 + 阶段规则，始终在最顶部。
	mergedSystemPrompt := mergeSystemPrompts(ctx, stageSystemPrompt)
	if mergedSystemPrompt != "" {
		messages = append(messages, schema.SystemMessage(mergedSystemPrompt))
	}

	// 2. 工具摘要：稳定参考信息，放在 history 前即可。
	if toolText := renderToolSchemas(ctx); toolText != "" {
		messages = append(messages, schema.SystemMessage(toolText))
	}

	// 3. 对话历史：按时间顺序，包含工具调用结果和修正反馈。
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

	// 4. 置顶上下文块：当前计划、当前步骤、粗排结果等最新约束。
	//    放在 history 之后、user prompt 之前，利用 LLM 近因效应提升对最新约束的注意力。
	if pinnedText := renderPinnedBlocks(ctx); pinnedText != "" {
		messages = append(messages, schema.SystemMessage(pinnedText))
	}

	// 5. 阶段性用户提示词：始终在末尾，是本轮回答的核心触发。
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
	if state.HasTerminalOutcome() && state.TerminalOutcome != nil {
		sb.WriteString(fmt.Sprintf("终止结果：%s\n", state.TerminalOutcome.Status))
		if strings.TrimSpace(state.TerminalOutcome.Stage) != "" {
			sb.WriteString(fmt.Sprintf("终止阶段：%s\n", state.TerminalOutcome.Stage))
		}
		if strings.TrimSpace(state.TerminalOutcome.Code) != "" {
			sb.WriteString(fmt.Sprintf("终止代码：%s\n", state.TerminalOutcome.Code))
		}
		if strings.TrimSpace(state.TerminalOutcome.UserMessage) != "" {
			sb.WriteString(fmt.Sprintf("终止说明：%s\n", state.TerminalOutcome.UserMessage))
		}
	}

	if !state.HasPlan() {
		sb.WriteString("当前完整 plan：暂无。\n")
	} else {
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
	}

	// 渲染任务类约束元数据（如有），帮助 LLM 了解排程范围和策略，避免追问已有信息。
	if len(state.TaskClasses) > 0 {
		sb.WriteString("\n本次排课涉及的任务类约束：\n")
		for _, tc := range state.TaskClasses {
			line := fmt.Sprintf("- [ID=%d] %s：策略=%s，总时段预算=%d", tc.ID, tc.Name, tc.Strategy, tc.TotalSlots)
			if tc.StartDate != "" || tc.EndDate != "" {
				line += fmt.Sprintf("，日期范围=%s ~ %s", tc.StartDate, tc.EndDate)
			}
			if tc.SubjectType != "" || tc.DifficultyLevel != "" || tc.CognitiveIntensity != "" {
				line += fmt.Sprintf("，语义画像=%s/%s/%s",
					defaultSemanticValue(tc.SubjectType),
					defaultSemanticValue(tc.DifficultyLevel),
					defaultSemanticValue(tc.CognitiveIntensity),
				)
			}
			if tc.AllowFillerCourse {
				line += "，允许嵌入水课"
			}
			if len(tc.ExcludedSlots) > 0 {
				line += fmt.Sprintf("，排除时段=%v", tc.ExcludedSlots)
			}
			if len(tc.ExcludedDaysOfWeek) > 0 {
				line += fmt.Sprintf("，排除星期=%v", tc.ExcludedDaysOfWeek)
			}
			sb.WriteString(line + "\n")
		}
	}

	return sb.String()
}

func defaultSemanticValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "未标注"
	}
	return trimmed
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
