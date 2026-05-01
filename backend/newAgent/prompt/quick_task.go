package newagentprompt

import (
	"fmt"
	"strings"
	"time"

	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/cloudwego/eino/schema"
)

const quickTaskSystemPrompt = `
你是 SmartMate 的快捷任务助手。用户想记录或查看待办任务。你需要从用户消息中提取操作意图和参数。

你能做的操作：
- create：记录一条新任务/提醒
- query：查看/筛选任务列表

输出格式（两阶段）：
先输出一行决策标签，标签内是 JSON；标签之后换行输出给用户看的自然语言正文。
决策标签格式：<SMARTFLOW_DECISION>{JSON}</SMARTFLOW_DECISION>

JSON 字段说明：
- action：只能是 create / query / ask
- create 时：title 必填，deadline_at 必填，priority_group 必填，范围 1-4；estimated_sections 必填，范围 1-4，不确定默认 1；urgency_threshold_at 满足条件时填写，条件在下面
- query 时：quadrant 可选 1-4，keyword 可选，limit 可选，deadline_after/deadline_before 可选（用于截止时间窗口筛选）
- ask 时：question 必填

规则：
1. 优先级：1=重要且紧急，2=重要不紧急，3=简单不重要，4=复杂不重要；紧急判定：截止时间距今不超过 48 小时为紧急(1)，超过 48 小时为不紧急(2)，无截止时间默认 3
2. 信息不足时（如缺少 title），输出 {"action":"ask","question":"追问内容"}
3. deadline_at 支持「明天下午3点」「下周一」「2026-04-20 18:00」等格式
4. 未提供的可选字段直接省略，不要填 null 或空字符串
5. JSON 中不要包含 speak 字段，给用户看的话放在 </SMARTFLOW_DECISION> 标签之后
6. 紧急分界时间，即任务从"重要不紧急"自动轮换到"重要且紧急"的时间点；格式同 deadline_at ——当 priority_group=2 时必填，你必须根据 deadline 自动推算一个合理的紧急分界时间（通常为 deadline 前 24-48 小时），不要等用户提供；priority_group 为 1、3、4 或无截止时间时不要输出此字段
7. query 里出现相对日期窗口（如"明天有什么事要做"）时，优先输出明确边界：deadline_after="明天 00:00"，deadline_before="后天 00:00"，按 [after, before) 语义筛选

示例：

<SMARTFLOW_DECISION>{"action":"create","title":"明天开会","deadline_at":"明天下午3点","estimated_sections":1}</SMARTFLOW_DECISION>
好的，我来帮你记一下。
<SMARTFLOW_DECISION>{"action":"create","title":"下周交报告","deadline_at":"下周五 18:00","priority_group":2,"estimated_sections":2,"urgency_threshold_at":"下周四 09:00"}</SMARTFLOW_DECISION>
好的，我也帮你记一下。

<SMARTFLOW_DECISION>{"action":"query","limit":5}</SMARTFLOW_DECISION>
我帮你查一下当前的任务。

<SMARTFLOW_DECISION>{"action":"query","deadline_after":"明天 00:00","deadline_before":"后天 00:00","limit":10}</SMARTFLOW_DECISION>
我帮你查一下明天要做的事。

<SMARTFLOW_DECISION>{"action":"ask","question":"你想记录什么呢？告诉我具体内容吧。"}</SMARTFLOW_DECISION>
你想记录什么呢？告诉我具体内容吧。`

// BuildQuickTaskSystemPrompt 返回快捷任务阶段的系统提示词。
func BuildQuickTaskSystemPrompt() string {
	return strings.TrimSpace(quickTaskSystemPrompt)
}

// BuildQuickTaskMessagesSimple 组装快捷任务阶段的最简 messages（无对话历史）。
func BuildQuickTaskMessagesSimple(userInput string) []*schema.Message {
	systemMsg := schema.SystemMessage(BuildQuickTaskSystemPrompt())
	userMsg := schema.UserMessage(buildQuickTaskUserPrompt(userInput))
	return []*schema.Message{systemMsg, userMsg}
}

func buildQuickTaskUserPrompt(userInput string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("当前时间=%s\n", time.Now().In(time.Local).Format("2006-01-02 15:04")))
	sb.WriteString("\n请从用户消息中提取操作意图和参数，严格按 SMARTFLOW_DECISION 标签格式输出。\n\n")

	trimmedInput := strings.TrimSpace(userInput)
	if trimmedInput != "" {
		sb.WriteString("用户输入：\n")
		sb.WriteString(trimmedInput)
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildQuickTaskMessages 组装快捷任务阶段的完整 messages（含对话历史）。
func BuildQuickTaskMessages(
	ctx *newagentmodel.ConversationContext,
	userInput string,
	toolSchemas []newagentmodel.ToolSchemaContext,
) []*schema.Message {
	return buildUnifiedStageMessages(
		ctx,
		StageMessagesConfig{
			SystemPrompt: BuildQuickTaskSystemPrompt(),
			Msg1Content:  buildChatConversationMessage(ctx),
			Msg2Content:  buildQuickTaskWorkspace(toolSchemas),
			Msg3Suffix:   buildQuickTaskUserPrompt(userInput),
			Msg3Role:     schema.User,
		},
	)
}

func buildQuickTaskWorkspace(toolSchemas []newagentmodel.ToolSchemaContext) string {
	var sb strings.Builder
	sb.WriteString("可用工具：\n")
	for _, ts := range toolSchemas {
		sb.WriteString(fmt.Sprintf("- %s: %s\n  参数: %s\n", ts.Name, ts.Desc, ts.SchemaText))
	}
	return sb.String()
}
