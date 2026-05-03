package agentsvc

import (
	"context"
	"io"
	"strings"
	"time"

	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

// streamChatFallback 是 graph 执行失败时的降级流式聊天。
// 内联了旧 agentchat.StreamChat 的核心逻辑，不再依赖 agent/ 包。
func (s *AgentService) streamChatFallback(
	ctx context.Context,
	llm *llmservice.Client,
	modelName string,
	userInput string,
	ifThinking bool,
	chatHistory []*schema.Message,
	outChan chan<- string,
	reasoningStartAt *time.Time,
	userID int,
	chatID string,
) (string, string, int, *schema.TokenUsage, error) {
	messages := make([]*schema.Message, 0, len(chatHistory)+2)
	messages = append(messages, schema.SystemMessage(newagentprompt.SystemPrompt))
	if len(chatHistory) > 0 {
		messages = append(messages, chatHistory...)
	}
	messages = append(messages, schema.UserMessage(userInput))

	if strings.TrimSpace(modelName) == "" {
		modelName = "smartflow-worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	firstChunk := true
	chunkEmitter := newagentstream.NewChunkEmitter(newagentstream.NewSSEPayloadEmitter(outChan), requestID, modelName, created)
	reasoningSummaryClient := s.llmService.LiteClient()
	if reasoningSummaryClient == nil {
		reasoningSummaryClient = s.llmService.ProClient()
	}
	chunkEmitter.SetReasoningSummaryFunc(s.makeReasoningSummaryFunc(reasoningSummaryClient))
	chunkEmitter.SetExtraEventHook(func(extra *newagentstream.OpenAIChunkExtra) {
		s.persistNewAgentTimelineExtraEvent(context.Background(), userID, chatID, extra)
	})
	reasoningDigestor, digestorErr := chunkEmitter.NewReasoningDigestor(ctx, "fallback.speak", "fallback")
	if digestorErr != nil {
		return "", "", 0, nil, digestorErr
	}
	digestorClosed := false
	closeDigestor := func() {
		if reasoningDigestor == nil || digestorClosed {
			return
		}
		digestorClosed = true
		_ = reasoningDigestor.Close(ctx)
	}
	defer closeDigestor()

	var localReasoningStartAt *time.Time
	if reasoningStartAt != nil && !reasoningStartAt.IsZero() {
		startCopy := reasoningStartAt.In(time.Local)
		localReasoningStartAt = &startCopy
	}
	var reasoningEndAt *time.Time

	thinkingMode := llmservice.ThinkingModeDisabled
	if ifThinking {
		thinkingMode = llmservice.ThinkingModeEnabled
	}

	reader, err := llm.Stream(ctx, messages, llmservice.GenerateOptions{
		Thinking: thinkingMode,
	})
	if err != nil {
		return "", "", 0, nil, err
	}
	defer reader.Close()

	var fullText strings.Builder
	var tokenUsage *schema.TokenUsage
	for {
		chunk, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			return "", "", 0, nil, recvErr
		}

		if chunk != nil && chunk.ResponseMeta != nil && chunk.ResponseMeta.Usage != nil {
			tokenUsage = newagentstream.MergeUsage(tokenUsage, chunk.ResponseMeta.Usage)
		}

		if chunk != nil {
			if strings.TrimSpace(chunk.ReasoningContent) != "" && localReasoningStartAt == nil {
				now := time.Now()
				localReasoningStartAt = &now
			}
			if strings.TrimSpace(chunk.Content) != "" && localReasoningStartAt != nil && reasoningEndAt == nil {
				now := time.Now()
				reasoningEndAt = &now
			}
			// 1. fallback 链路同样不能透传 raw reasoning_content；
			// 2. 只把 reasoning 喂给摘要器，正文出现时立即关门丢弃后续摘要。
			if strings.TrimSpace(chunk.ReasoningContent) != "" && reasoningDigestor != nil {
				reasoningDigestor.Append(chunk.ReasoningContent)
			}
			if chunk.Content != "" {
				if reasoningDigestor != nil {
					reasoningDigestor.MarkContentStarted()
				}
				if emitErr := chunkEmitter.EmitAssistantText("fallback.speak", "fallback", chunk.Content, firstChunk); emitErr != nil {
					return "", "", 0, nil, emitErr
				}
				fullText.WriteString(chunk.Content)
				firstChunk = false
			}
		}
	}
	closeDigestor()

	if finishErr := chunkEmitter.EmitFinish("fallback.speak", "fallback"); finishErr != nil {
		return "", "", 0, nil, finishErr
	}
	if doneErr := chunkEmitter.EmitDone(); doneErr != nil {
		return "", "", 0, nil, doneErr
	}

	reasoningDurationSeconds := 0
	if localReasoningStartAt != nil {
		if reasoningEndAt == nil {
			now := time.Now()
			reasoningEndAt = &now
		}
		if reasoningEndAt.After(*localReasoningStartAt) {
			reasoningDurationSeconds = int(reasoningEndAt.Sub(*localReasoningStartAt) / time.Second)
		}
	}

	return fullText.String(), "", reasoningDurationSeconds, tokenUsage, nil
}
