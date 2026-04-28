package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
)

const reasoningSummaryMaxTokens = 700

type reasoningSummaryLLMResponse struct {
	ShortSummary  string `json:"short_summary"`
	DetailSummary string `json:"detail_summary"`
}

// makeReasoningSummaryFunc 把便宜模型封装成 stream 层可注入的摘要函数。
//
// 职责边界：
// 1. service 层负责选择模型与 prompt，stream 层只负责调度和闸门；
// 2. 这里不持久化摘要，持久化统一走 ChunkEmitter 的 extra hook；
// 3. 摘要失败时返回 error，由 ReasoningDigestor 吞掉并等待下一次水位线/Flush 兜底。
func (s *AgentService) makeReasoningSummaryFunc(client *infrallm.Client) newagentstream.ReasoningSummaryFunc {
	if client == nil {
		return nil
	}

	return func(ctx context.Context, input newagentstream.ReasoningSummaryInput) (newagentstream.StreamThinkingSummaryExtra, error) {
		previousSummary := ""
		if input.PreviousSummary != nil {
			previousSummary = input.PreviousSummary.DetailSummary
			if strings.TrimSpace(previousSummary) == "" {
				previousSummary = input.PreviousSummary.ShortSummary
			}
		}

		messages := newagentprompt.BuildReasoningSummaryMessages(newagentprompt.ReasoningSummaryPromptInput{
			FullReasoning:   input.FullReasoning,
			DeltaReasoning:  input.DeltaReasoning,
			PreviousSummary: previousSummary,
			CandidateSeq:    input.CandidateSeq,
			Final:           input.Final,
			DurationSeconds: input.DurationSeconds,
		})

		resp, rawResult, err := infrallm.GenerateJSON[reasoningSummaryLLMResponse](
			ctx,
			client,
			messages,
			infrallm.GenerateOptions{
				Temperature: 0.1,
				MaxTokens:   reasoningSummaryMaxTokens,
				Thinking:    infrallm.ThinkingModeDisabled,
				Metadata: map[string]any{
					"stage":         "reasoning_summary",
					"candidate_seq": input.CandidateSeq,
					"final":         input.Final,
				},
			},
		)
		if err != nil {
			log.Printf("[WARN] reasoning 摘要模型调用失败 seq=%d final=%v err=%v raw=%s",
				input.CandidateSeq,
				input.Final,
				err,
				truncateReasoningSummaryRaw(rawResult),
			)
			return newagentstream.StreamThinkingSummaryExtra{}, err
		}

		summary := newagentstream.StreamThinkingSummaryExtra{
			ShortSummary: strings.TrimSpace(resp.ShortSummary),
			DetailSummary: limitReasoningDetailSummary(
				resp.DetailSummary,
				newagentprompt.ReasoningSummaryDetailRuneLimit(input.FullReasoning, input.DeltaReasoning),
			),
		}
		if summary.ShortSummary == "" && summary.DetailSummary == "" {
			return newagentstream.StreamThinkingSummaryExtra{}, errors.New("reasoning 摘要模型返回空摘要")
		}
		return summary, nil
	}
}

func limitReasoningDetailSummary(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return text
	}

	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes])
}

func truncateReasoningSummaryRaw(raw *infrallm.TextResult) string {
	if raw == nil {
		return ""
	}
	text := strings.TrimSpace(raw.Text)
	runes := []rune(text)
	if len(runes) <= 200 {
		return text
	}
	return string(runes[:200]) + "..."
}
