package newagentprompt

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

const (
	reasoningSummaryMaxFullRunes  = 6000
	reasoningSummaryMaxDeltaRunes = 1800
)

// ReasoningSummaryPromptInput 描述一次“思考摘要”模型调用所需的最小输入。
//
// 职责边界：
// 1. 只承载摘要模型需要看的文本与运行态，不绑定 stream 包的 DTO，避免 prompt 层反向依赖输出协议；
// 2. FullReasoning 会在构造 prompt 时只保留尾部，避免长时间思考把便宜模型上下文撑爆；
// 3. PreviousSummary 只作为连续摘要的参考，不要求模型逐字继承。
type ReasoningSummaryPromptInput struct {
	FullReasoning   string
	DeltaReasoning  string
	PreviousSummary string
	CandidateSeq    int
	Final           bool
	DurationSeconds float64
}

type reasoningSummaryPromptPayload struct {
	CandidateSeq          int     `json:"candidate_seq"`
	Final                 bool    `json:"final"`
	DurationSeconds       float64 `json:"duration_seconds"`
	PreviousSummary       string  `json:"previous_summary,omitempty"`
	RecentReasoning       string  `json:"recent_reasoning,omitempty"`
	DeltaReasoning        string  `json:"delta_reasoning,omitempty"`
	SourceTextRunes       int     `json:"source_text_runes,omitempty"`
	MaxDetailSummaryRunes int     `json:"max_detail_summary_runes,omitempty"`
}

// BuildReasoningSummaryMessages 构造思考摘要模型调用的 messages。
//
// 步骤说明：
// 1. system prompt 明确“只做用户可见摘要”，禁止复述原始思考链和内部推理细节；
// 2. user prompt 使用 JSON 承载输入，便于后续扩展字段且减少模型误读；
// 3. 长文本只保留尾部窗口，保证异步摘要请求稳定、便宜、可控。
func BuildReasoningSummaryMessages(input ReasoningSummaryPromptInput) []*schema.Message {
	recentReasoning := trimRunesFromEnd(input.FullReasoning, reasoningSummaryMaxFullRunes)
	deltaReasoning := trimRunesFromEnd(input.DeltaReasoning, reasoningSummaryMaxDeltaRunes)
	payload := reasoningSummaryPromptPayload{
		CandidateSeq:          input.CandidateSeq,
		Final:                 input.Final,
		DurationSeconds:       input.DurationSeconds,
		PreviousSummary:       strings.TrimSpace(input.PreviousSummary),
		RecentReasoning:       recentReasoning,
		DeltaReasoning:        deltaReasoning,
		SourceTextRunes:       reasoningSummarySourceRunes(recentReasoning, deltaReasoning),
		MaxDetailSummaryRunes: ReasoningSummaryDetailRuneLimit(input.FullReasoning, input.DeltaReasoning),
	}

	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		raw = []byte(fmt.Sprintf(`{"recent_reasoning":%q}`, trimRunesFromEnd(input.FullReasoning, reasoningSummaryMaxFullRunes)))
	}

	return []*schema.Message{
		schema.SystemMessage(buildReasoningSummarySystemPrompt()),
		schema.UserMessage("请把下面的模型思考内容整理成用户可见的进度摘要。\n输入：\n" + string(raw)),
	}
}

func buildReasoningSummarySystemPrompt() string {
	return strings.TrimSpace(`你是 SmartMate 的“思考摘要器”。你的任务是把模型内部 reasoning 整理成用户可见的进度摘要。

输出必须是严格 JSON 对象：
{
  "short_summary": "8到18个汉字的短摘要",
  "detail_summary": "不超过 max_detail_summary_runes 个字的展开摘要"
}

规则：
1. 只描述“正在做什么”和“目前推进到哪一步”，不要复述、引用或暴露原始思考链。
2. 不输出 markdown，不输出代码块，不解释 JSON 以外的内容。
3. short_summary 要短、稳定、适合前端几秒刷新一次。
4. detail_summary 不按固定句数限制，而按输入长度控制：字数必须小于等于 max_detail_summary_runes；不需要凑满上限，信息密度优先。
5. detail_summary 仍然面向用户，不写内部推理细节、隐含假设链、逐步演算。
6. 若输入为空或噪声较多，用保守摘要，例如“正在整理思路”“正在核对可用信息”。
7. final=true 时，detail_summary 用完成态语气，说明思考已收拢到下一步答复或动作。`)
}

// ReasoningSummaryDetailRuneLimit 返回 detail_summary 的最大字数。
//
// 职责边界：
// 1. 与 BuildReasoningSummaryMessages 使用同一套输入窗口，避免 prompt 提示和服务端兜底口径不一致；
// 2. 上限取“提供给摘要模型的主要文本段”的一半，并向上取整，适配极短文本；
// 3. 返回 0 表示没有有效输入文本，调用方不应做硬裁剪。
func ReasoningSummaryDetailRuneLimit(fullReasoning, deltaReasoning string) int {
	recentReasoning := trimRunesFromEnd(fullReasoning, reasoningSummaryMaxFullRunes)
	delta := trimRunesFromEnd(deltaReasoning, reasoningSummaryMaxDeltaRunes)
	sourceRunes := reasoningSummarySourceRunes(recentReasoning, delta)
	if sourceRunes <= 0 {
		return 0
	}
	return (sourceRunes + 1) / 2
}

func reasoningSummarySourceRunes(recentReasoning, deltaReasoning string) int {
	recentReasoning = strings.TrimSpace(recentReasoning)
	if recentReasoning != "" {
		return utf8.RuneCountInString(recentReasoning)
	}
	return utf8.RuneCountInString(strings.TrimSpace(deltaReasoning))
}

func trimRunesFromEnd(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return ""
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[len(runes)-maxRunes:])
}
