package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
)

const (
	defaultMemoryExtractMaxTokens = 1200
	defaultMemoryExtractMaxFacts  = 5
)

// LLMWriteOrchestrator 负责把单条对话消息转成可入库的记忆候选。
//
// 职责边界：
// 1. 负责调用 LLM 做抽取、把输出标准化成 memory_facts；
// 2. 不负责落库，不负责任务状态机推进；
// 3. 当 LLM 不可用或输出异常时，回退到保守的本地抽取，保证链路不完全断。
type LLMWriteOrchestrator struct {
	client *infrallm.Client
	cfg    memorymodel.Config
	logger *log.Logger
}

// NewLLMWriteOrchestrator 构造 LLM 版记忆写入编排器。
func NewLLMWriteOrchestrator(client *infrallm.Client, cfg memorymodel.Config) *LLMWriteOrchestrator {
	return &LLMWriteOrchestrator{
		client: client,
		cfg:    cfg,
		logger: log.Default(),
	}
}

// ExtractFacts 从单条消息中抽取可入库事实。
//
// 返回语义：
// 1. 成功时返回标准化后的候选事实；
// 2. 即使 LLM 失败，也尽量返回保守的 fallback 结果，避免 worker 空转报错；
// 3. 只有输入本身为空时才返回空结果。
func (o *LLMWriteOrchestrator) ExtractFacts(ctx context.Context, payload memorymodel.ExtractJobPayload) ([]memorymodel.NormalizedFact, error) {
	sourceText := strings.TrimSpace(payload.SourceText)
	if sourceText == "" {
		return nil, nil
	}

	if o == nil || o.client == nil {
		return fallbackNormalizedFacts(payload), nil
	}

	messages := infrallm.BuildSystemUserMessages(
		buildMemoryExtractSystemPrompt(o.cfg.ExtractPrompt),
		nil,
		buildMemoryExtractUserPrompt(payload),
	)

	resp, rawResult, err := infrallm.GenerateJSON[memoryExtractResponse](
		ctx,
		o.client,
		messages,
		infrallm.GenerateOptions{
			Temperature: clampTemperature(o.cfg.LLMTemperature),
			MaxTokens:   defaultMemoryExtractMaxTokens,
			Thinking:    infrallm.ThinkingModeDisabled,
			Metadata: map[string]any{
				"stage":           "memory_extract",
				"user_id":         payload.UserID,
				"conversation_id": payload.ConversationID,
			},
		},
	)
	if err != nil {
		if o.logger != nil {
			o.logger.Printf("[WARN] memory extract llm failed user_id=%d conversation_id=%s err=%v raw=%s",
				payload.UserID, payload.ConversationID, err, truncateForLog(rawResult))
		}
		return fallbackNormalizedFacts(payload), nil
	}

	facts := convertExtractResponse(resp)
	normalized := memoryutils.NormalizeFacts(facts)
	if len(normalized) == 0 {
		return fallbackNormalizedFacts(payload), nil
	}
	return normalized, nil
}

type memoryExtractResponse struct {
	MessageIntent string              `json:"message_intent"`
	Facts         []memoryExtractFact `json:"facts"`
}

type memoryExtractFact struct {
	MemoryType       string  `json:"memory_type"`
	Title            string  `json:"title"`
	Content          string  `json:"content"`
	Confidence       float64 `json:"confidence"`
	Importance       float64 `json:"importance"`
	SensitivityLevel int     `json:"sensitivity_level"`
	IsExplicit       bool    `json:"is_explicit"`
}

type memoryExtractPromptInput struct {
	UserID          int    `json:"user_id"`
	ConversationID  string `json:"conversation_id"`
	AssistantID     string `json:"assistant_id,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	SourceMessageID int64  `json:"source_message_id,omitempty"`
	SourceRole      string `json:"source_role"`
	SourceText      string `json:"source_text"`
	OccurredAt      string `json:"occurred_at"`
	TraceID         string `json:"trace_id,omitempty"`
}

func buildMemoryExtractSystemPrompt(override string) string {
	override = strings.TrimSpace(override)
	if override != "" {
		return override
	}

	return strings.TrimSpace(`你是一个”记忆守门员”。
你的任务是判断用户消息是否包含值得长期记住的信息，如有则提取。
请只输出 JSON 对象，不要输出解释、不要输出 markdown。

输出格式：
{
  “message_intent”: “chitchat|task_request|knowledge_qa|preference|personal_fact|standing_instruction”,
  “facts”: [
    {
      “memory_type”: “preference|constraint|fact|todo_hint”,
      “title”: “短标题”,
      “content”: “完整事实内容”,
      “confidence”: 0.0,
      “importance”: 0.0,
      “sensitivity_level”: 0,
      “is_explicit”: false
    }
  ]
}

意图分类规则：
- chitchat：闲聊、寒暄、情绪表达（”你好””谢谢””我今天好累””嗯嗯”）
- task_request：一次性任务请求（”帮我查天气””定个闹钟””帮我写个邮件”）
- knowledge_qa：知识问答、信息查询（”什么是量子力学””北京明天多少度”）
- preference：用户偏好、习惯、口味（”我喜欢吃辣””别用简称””我习惯用微信”）
- personal_fact：个人事实（”我有两个孩子””我在上海工作””我老婆对花生过敏”）
- standing_instruction：持久指令（”以后都用英文回复我””记住我的生日是3月5号”）

规则：
1. 先判断 message_intent。chitchat / task_request / knowledge_qa 三类，facts 输出空数组。
2. 只有 preference / personal_fact / standing_instruction 才提取 facts，最多 3 条。
3. 一条消息可能同时包含任务和偏好（如”帮我查天气，记住我喜欢晴天”），此时 intent 取偏好类型，facts 只保留偏好部分。
4. confidence 表示这条事实是否真的值得长期记，取 0 到 1。低于 0.5 的不要输出。
5. importance 表示对后续陪伴的价值，取 0 到 1。
6. sensitivity_level 取 0 到 2，数字越大越敏感。
7. 用户明确说”记住”或”以后提醒我”时，is_explicit 设为 true。
8. 宁可漏记也不要滥记。大多数消息不应该产生任何 facts。`)
}

func buildMemoryExtractUserPrompt(payload memorymodel.ExtractJobPayload) string {
	request := memoryExtractPromptInput{
		UserID:          payload.UserID,
		ConversationID:  payload.ConversationID,
		AssistantID:     payload.AssistantID,
		RunID:           payload.RunID,
		SourceMessageID: payload.SourceMessageID,
		SourceRole:      payload.SourceRole,
		SourceText:      payload.SourceText,
		OccurredAt:      payload.OccurredAt.Format("2006-01-02 15:04:05"),
		TraceID:         payload.TraceID,
	}

	raw, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		return fmt.Sprintf("请分析这条用户消息，判断是否需要写入长期记忆：%s", payload.SourceText)
	}

	return fmt.Sprintf("请分析下面这条用户消息，判断 message_intent，如包含值得长期记住的信息则提取 facts。\n输入：\n%s",
		string(raw))
}

func convertExtractResponse(resp *memoryExtractResponse) []memorymodel.FactCandidate {
	if resp == nil {
		return nil
	}

	// 意图过滤：跳过不需要记忆的消息类型。
	// 兼容自定义 prompt（不返回 message_intent 时跳过此检查，保持向后兼容）。
	if intent := strings.TrimSpace(resp.MessageIntent); intent != "" {
		if isSkipIntent(intent) {
			return nil
		}
	}

	if len(resp.Facts) == 0 {
		return nil
	}

	result := make([]memorymodel.FactCandidate, 0, len(resp.Facts))
	for _, fact := range resp.Facts {
		memoryType := memorymodel.NormalizeMemoryType(fact.MemoryType)
		if memoryType == "" {
			continue
		}

		content := strings.TrimSpace(fact.Content)
		if content == "" {
			continue
		}

		confidence := clamp01(fact.Confidence)
		if confidence == 0 {
			confidence = 0.6
		}

		importance := clamp01(fact.Importance)
		if importance == 0 {
			importance = defaultImportanceByType(memoryType)
		}

		result = append(result, memorymodel.FactCandidate{
			MemoryType:       memoryType,
			Title:            strings.TrimSpace(fact.Title),
			Content:          content,
			Confidence:       confidence,
			Importance:       importance,
			SensitivityLevel: clampInt(fact.SensitivityLevel, 0, 2),
			IsExplicit:       fact.IsExplicit,
		})
	}
	return result
}

func fallbackNormalizedFacts(payload memorymodel.ExtractJobPayload) []memorymodel.NormalizedFact {
	sourceText := strings.TrimSpace(payload.SourceText)
	if sourceText == "" {
		return nil
	}

	return memoryutils.NormalizeFacts([]memorymodel.FactCandidate{
		{
			MemoryType:       memorymodel.MemoryTypeFact,
			Title:            buildFallbackTitle(sourceText),
			Content:          sourceText,
			Confidence:       0.45,
			Importance:       defaultImportanceByType(memorymodel.MemoryTypeFact),
			SensitivityLevel: 0,
			IsExplicit:       false,
		},
	})
}

func buildFallbackTitle(sourceText string) string {
	runes := []rune(strings.TrimSpace(sourceText))
	if len(runes) == 0 {
		return "用户提到"
	}
	if len(runes) > 24 {
		runes = runes[:24]
	}
	return "用户提到：" + string(runes)
}

func clampTemperature(v float64) float64 {
	if v <= 0 {
		return 0.1
	}
	if v > 1 {
		return 1
	}
	return v
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampInt(v, minValue, maxValue int) int {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func defaultImportanceByType(memoryType string) float64 {
	switch memoryType {
	case memorymodel.MemoryTypePreference:
		return 0.85
	case memorymodel.MemoryTypeConstraint:
		return 0.95
	case memorymodel.MemoryTypeTodoHint:
		return 0.8
	default:
		return 0.6
	}
}

// isSkipIntent 判断意图是否属于"不需要记忆"的类别。
// chitchat / task_request / knowledge_qa 三类直接跳过，不产出任何候选事实。
func isSkipIntent(intent string) bool {
	switch strings.ToLower(strings.TrimSpace(intent)) {
	case "chitchat", "task_request", "knowledge_qa":
		return true
	default:
		return false
	}
}

func truncateForLog(raw *infrallm.TextResult) string {
	if raw == nil {
		return ""
	}
	text := strings.TrimSpace(raw.Text)
	if len(text) <= 200 {
		return text
	}
	return text[:200] + "..."
}
