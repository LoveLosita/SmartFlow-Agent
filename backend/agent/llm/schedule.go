package agentllm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	agentprompt "github.com/LoveLosita/smartflow/backend/agent/prompt"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// ScheduleIntentOutput 是 plan 节点要求模型返回的结构化结果。
//
// 兼容说明：
//  1. 新主语义是 task_class_ids（数组）；
//  2. 为兼容旧 prompt/旧缓存输出，保留 task_class_id（单值）兜底解析；
//  3. TaskTags 的 key 兼容两种写法：
//     3.1 推荐：task_item_id（例如 "12"）；
//     3.2 兼容：任务名称（例如 "高数复习"）。
type ScheduleIntentOutput struct {
	Intent          string            `json:"intent"`
	Constraints     []string          `json:"constraints"`
	TaskClassIDs    []int             `json:"task_class_ids"`
	TaskClassID     int               `json:"task_class_id"`
	Strategy        string            `json:"strategy"`
	TaskTags        map[string]string `json:"task_tags"`
	Restart         bool              `json:"restart"`
	AdjustmentScope string            `json:"adjustment_scope"`
	Reason          string            `json:"reason"`
	Confidence      float64           `json:"confidence"`
}

// ReactToolCall 是 LLM 输出的单个工具调用。
type ReactToolCall struct {
	Tool   string         `json:"tool"`
	Params map[string]any `json:"params"`
}

// ReactLLMOutput 是 ReAct 节点要求模型返回的统一 JSON。
type ReactLLMOutput struct {
	Done      bool            `json:"done"`
	Summary   string          `json:"summary"`
	ToolCalls []ReactToolCall `json:"tool_calls"`
}

// IdentifySchedulePlanIntent 调用模型识别“排程意图 + 约束 + 任务类集合”。
func IdentifySchedulePlanIntent(
	ctx context.Context,
	chatModel *ark.ChatModel,
	nowText string,
	userMessage string,
	adjustmentHint string,
) (*ScheduleIntentOutput, error) {
	prompt := fmt.Sprintf(
		"当前时间（北京时间）：%s\n用户输入：%s%s\n\n请提取排程意图与约束。",
		strings.TrimSpace(nowText),
		strings.TrimSpace(userMessage),
		strings.TrimSpace(adjustmentHint),
	)

	parsed, _, err := CallArkJSON[ScheduleIntentOutput](ctx, chatModel, agentprompt.SchedulePlanIntentPrompt, prompt, ArkCallOptions{
		Temperature: 0,
		MaxTokens:   256,
		Thinking:    ThinkingModeDisabled,
	})
	return parsed, err
}

// ParseScheduleReactOutput 解析 ReAct 节点的 JSON 输出。
func ParseScheduleReactOutput(raw string) (*ReactLLMOutput, error) {
	return ParseJSONObject[ReactLLMOutput](raw)
}

// GenerateScheduleDailyReactRound 调用模型生成“单天日内优化”的一轮决策。
//
// 职责边界：
// 1. 只负责统一关闭 thinking、设置温度，并返回纯文本；
// 2. 不负责工具执行，不负责结果回灌。
func GenerateScheduleDailyReactRound(
	ctx context.Context,
	chatModel *ark.ChatModel,
	messages []*schema.Message,
) (string, error) {
	resp, err := chatModel.Generate(
		ctx,
		messages,
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}),
		einoModel.WithTemperature(0),
	)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("日内优化调用返回为空")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("日内优化调用返回内容为空")
	}
	return content, nil
}

// GenerateScheduleWeeklyReactRound 调用模型生成“单周单步优化”的一轮决策。
//
// 职责边界：
// 1. 周级仍保留 thinking，提高复杂排程准确率；
// 2. 仅返回最终 content，是否透出思考流由上层决定。
func GenerateScheduleWeeklyReactRound(
	ctx context.Context,
	chatModel *ark.ChatModel,
	messages []*schema.Message,
) (string, error) {
	resp, err := chatModel.Generate(
		ctx,
		messages,
		ark.WithThinking(&arkModel.Thinking{Type: arkModel.ThinkingTypeEnabled}),
		einoModel.WithTemperature(0.2),
	)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", fmt.Errorf("周级单步调用返回为空")
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return "", fmt.Errorf("周级单步调用返回内容为空")
	}
	return content, nil
}

// GenerateScheduleHumanSummary 调用模型生成“用户可读”的最终总结。
func GenerateScheduleHumanSummary(
	ctx context.Context,
	chatModel *ark.ChatModel,
	entries []model.HybridScheduleEntry,
	constraints []string,
	actionLogs []string,
) (string, error) {
	if chatModel == nil {
		return "", fmt.Errorf("final summary model is nil")
	}

	entriesJSON, _ := json.Marshal(entries)
	constraintText := "无"
	if len(constraints) > 0 {
		constraintText = strings.Join(constraints, "、")
	}
	actionLogText := "无"
	if len(actionLogs) > 0 {
		start := 0
		if len(actionLogs) > 30 {
			start = len(actionLogs) - 30
		}
		actionLogText = strings.Join(actionLogs[start:], "\n")
	}

	userPrompt := fmt.Sprintf(
		"以下是最终排程方案（JSON）：\n%s\n\n用户约束：%s\n\n以下是本次周级优化动作日志（按时间顺序）：\n%s\n\n请基于“结果+过程”输出2-3句自然中文总结，重点说明本方案的优点和改进点。",
		string(entriesJSON),
		constraintText,
		actionLogText,
	)

	return CallArkText(ctx, chatModel, agentprompt.SchedulePlanFinalCheckPrompt, userPrompt, ArkCallOptions{
		Temperature: 0.4,
		MaxTokens:   256,
		Thinking:    ThinkingModeDisabled,
	})
}
