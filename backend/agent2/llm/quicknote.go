package agentllm

import (
	"context"
	"fmt"
	"strings"

	agentprompt "github.com/LoveLosita/smartflow/backend/agent2/prompt"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

// QuickNoteIntentOutput 是“随口记意图识别”模型契约。
type QuickNoteIntentOutput struct {
	IsQuickNote bool   `json:"is_quick_note"`
	Title       string `json:"title"`
	DeadlineAt  string `json:"deadline_at"`
	Reason      string `json:"reason"`
}

// QuickNotePriorityOutput 是“随口记优先级评估”模型契约。
type QuickNotePriorityOutput struct {
	PriorityGroup      int    `json:"priority_group"`
	Reason             string `json:"reason"`
	UrgencyThresholdAt string `json:"urgency_threshold_at"`
}

// QuickNotePlanOutput 是“随口记单请求聚合规划”模型契约。
type QuickNotePlanOutput struct {
	Title              string `json:"title"`
	DeadlineAt         string `json:"deadline_at"`
	UrgencyThresholdAt string `json:"urgency_threshold_at"`
	PriorityGroup      int    `json:"priority_group"`
	PriorityReason     string `json:"priority_reason"`
	Banter             string `json:"banter"`
}

// IdentifyQuickNoteIntent 调用模型识别“是否随口记”。
func IdentifyQuickNoteIntent(ctx context.Context, chatModel *ark.ChatModel, nowText, userInput string) (*QuickNoteIntentOutput, error) {
	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
用户输入：%s
请仅输出 JSON（不要 markdown，不要解释），字段如下：
{
  "is_quick_note": boolean,
  "title": string,
  "deadline_at": string,
  "reason": string
}
字段约束：
1) deadline_at 只允许输出绝对时间，格式必须为 "yyyy-MM-dd HH:mm"。
2) 如果用户说了“明天/后天/下周一/今晚”等相对时间，必须基于上面的当前时间换算成绝对时间。
3) 如果用户没有提及时间，deadline_at 输出空字符串。`,
		nowText,
		userInput,
	)

	parsed, _, err := CallArkJSON[QuickNoteIntentOutput](ctx, chatModel, agentprompt.QuickNoteIntentPrompt, prompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   256,
		Thinking:    ThinkingModeDisabled,
	})
	return parsed, err
}

// PlanQuickNotePriority 调用模型评估优先级与紧急分界线。
func PlanQuickNotePriority(ctx context.Context, chatModel *ark.ChatModel, nowText, title, userInput, deadlineClue, deadlineText string) (*QuickNotePriorityOutput, error) {
	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
请对以下任务评估优先级：
- 任务标题：%s
- 用户原始输入：%s
- 时间线索原文：%s
- 归一化截止时间：%s

请仅输出 JSON（不要 markdown，不要解释）：
{
  "priority_group": 1|2|3|4,
  "reason": "简短理由",
  "urgency_threshold_at": "yyyy-MM-dd HH:mm 或空字符串"
}

额外约束：
1) urgency_threshold_at 表示“何时从不紧急象限自动平移到紧急象限”；
2) 若该任务不需要自动平移，可输出空字符串；
3) 若任务已在紧急象限（priority_group=1 或 3），优先输出空字符串；
4) 若输出非空时间，必须是绝对时间，且不晚于归一化截止时间（若有）。`,
		nowText,
		title,
		userInput,
		deadlineClue,
		deadlineText,
	)

	parsed, _, err := CallArkJSON[QuickNotePriorityOutput](ctx, chatModel, agentprompt.QuickNotePriorityPrompt, prompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   256,
		Thinking:    ThinkingModeDisabled,
	})
	return parsed, err
}

// PlanQuickNoteInSingleCall 一次性完成标题/时间/优先级/banter 聚合规划。
func PlanQuickNoteInSingleCall(ctx context.Context, chatModel *ark.ChatModel, nowText, userInput string) (*QuickNotePlanOutput, error) {
	prompt := fmt.Sprintf(`当前时间（北京时间，精确到分钟）：%s
用户输入：%s

请仅输出 JSON（不要 markdown，不要解释），字段如下：
{
  "title": string,
  "deadline_at": string,
  "urgency_threshold_at": string,
  "priority_group": 1|2|3|4,
  "priority_reason": string,
  "banter": string
}

约束：
1) deadline_at 只允许 "yyyy-MM-dd HH:mm" 或空字符串；
2) urgency_threshold_at 只允许 "yyyy-MM-dd HH:mm" 或空字符串；
3) 若用户给了相对时间（如明天/今晚/下周一），必须换算为绝对时间；
4) 若任务不需要自动平移，可让 urgency_threshold_at 为空；
5) banter 只允许一句中文，不超过30字，不得改动任务事实。`,
		nowText,
		strings.TrimSpace(userInput),
	)

	parsed, _, err := CallArkJSON[QuickNotePlanOutput](ctx, chatModel, agentprompt.QuickNotePlanPrompt, prompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   220,
		Thinking:    ThinkingModeDisabled,
	})
	return parsed, err
}

// GenerateQuickNoteBanter 生成成功写入后的轻松跟进句。
func GenerateQuickNoteBanter(ctx context.Context, chatModel *ark.ChatModel, userMessage, title, priorityText, deadlineText string) (string, error) {
	if chatModel == nil {
		return "", fmt.Errorf("model is nil")
	}

	prompt := fmt.Sprintf(`用户原话：%s
已确认事实：
- 任务标题：%s
- %s
- %s

请输出一句轻松自然的跟进话术（仅一句）。`,
		strings.TrimSpace(userMessage),
		strings.TrimSpace(title),
		strings.TrimSpace(priorityText),
		strings.TrimSpace(deadlineText),
	)

	text, err := CallArkText(ctx, chatModel, agentprompt.QuickNoteReplyBanterPrompt, prompt, ArkCallOptions{
		Temperature: 0.7,
		MaxTokens:   72,
		Thinking:    ThinkingModeDisabled,
	})
	if err != nil {
		return "", err
	}

	text = strings.TrimSpace(text)
	text = strings.Trim(text, "\"'“”‘’")
	if text == "" {
		return "", fmt.Errorf("empty content")
	}
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return text, nil
}
