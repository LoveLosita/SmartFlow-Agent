package newagentprompt

import (
	"fmt"
	"strings"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const (
	// PlanDoneSignal 表示“规划阶段结束，可以进入 confirm 或下一阶段”。
	//
	// TODO(newagent/node): 后续由 planNode 读取模型输出时识别这个信号，并据此调用 state.FinishPlan(...)。
	PlanDoneSignal = "[PLAN_DONE]"
)

const planSystemPrompt = `
你是 SmartFlow NewAgent 的规划器。

你的职责不是直接执行任务，而是先把用户意图拆成一组清晰、稳定、可逐步执行的自然语言计划。

请遵守以下规则：
1. 只负责规划，不要假装已经调用了工具，也不要伪造执行结果。
2. 每一轮只推进一步规划；如果信息不足，可以明确指出缺口。
3. 若当前计划仍不完整，就继续围绕当前任务补全计划，不要跳去执行细节。
4. 若你认为计划已经完整可执行，请在输出中显式带上 ` + "`" + `[` + `PLAN_DONE` + `]` + "`" + ` 信号。
5. 计划必须使用自然语言，便于后端将完整 plan 重新注入到后续上下文顶部。

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
// 2. 负责把“置顶上下文”和“工具摘要”放到 history 前面，降低模型跑偏概率；
// 3. 不负责解析模型输出，不负责判断是否真的完成规划。
//
// TODO(newagent/node): 后续 planNode 直接复用这个入口，不要在节点里散落拼 message 的逻辑。
func BuildPlanMessages(state *newagentmodel.CommonState, ctx *newagentmodel.ConversationContext, userInput string) []*schema.Message {
	return buildStageMessages(
		BuildPlanSystemPrompt(),
		ctx,
		BuildPlanUserPrompt(state, userInput),
	)
}

// BuildPlanUserPrompt 构造规划阶段的用户提示词。
//
// 设计目标：
// 1. 把当前阶段、轮次、既有 plan、当前步骤等控制信息显式写给模型；
// 2. 保持自然语言风格，方便你后续继续改成自己想要的控制协议；
// 3. 用户原始输入单独放在末尾，避免被系统拼装信息淹没。
func BuildPlanUserPrompt(state *newagentmodel.CommonState, userInput string) string {
	var sb strings.Builder

	sb.WriteString("请继续当前任务的规划阶段。\n")
	sb.WriteString(renderStateSummary(state))
	sb.WriteString("\n")
	sb.WriteString("本轮目标：围绕当前任务继续规划，直到形成一份稳定、可执行的自然语言 plan。\n")
	sb.WriteString("如果计划已经完整，请显式输出 ")
	sb.WriteString(PlanDoneSignal)
	sb.WriteString("。\n")

	trimmedInput := strings.TrimSpace(userInput)
	if trimmedInput != "" {
		sb.WriteString("\n用户本轮输入：\n")
		sb.WriteString(trimmedInput)
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

// buildStageMessages 组装某个阶段通用的 messages。
//
// 步骤说明：
// 1. 先合并 context 自带 system prompt 与阶段 prompt，保证通用约束和阶段约束都能生效；
// 2. 再把置顶上下文块和工具摘要补成 system message，尽量顶在 history 前面；
// 3. 最后追加历史消息与本轮 user prompt，保持“新约束在前、历史在后”的稳定顺序。
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
			messages = append(messages, history...)
		}
	}

	runtimeUserPrompt = strings.TrimSpace(runtimeUserPrompt)
	if runtimeUserPrompt != "" {
		messages = append(messages, schema.UserMessage(runtimeUserPrompt))
	}

	return messages
}

// renderStateSummary 将当前流程状态渲染成简洁文本。
func renderStateSummary(state *newagentmodel.CommonState) string {
	if state == nil {
		return "当前状态：state 缺失，请先进行兜底处理。"
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
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, strings.TrimSpace(step)))
	}

	if step, ok := state.CurrentPlanStep(); ok {
		sb.WriteString(fmt.Sprintf("当前步骤进度：%d/%d\n", current, total))
		sb.WriteString("当前步骤内容：\n")
		sb.WriteString(step)
		sb.WriteString("\n")
	} else {
		sb.WriteString("当前步骤进度：暂无有效当前步骤。\n")
	}

	return sb.String()
}

// renderPinnedBlocks 将 ConversationContext 中的置顶块渲染成一段独立的 system 内容。
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

// renderToolSchemas 将工具摘要渲染成独立文本块。
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
