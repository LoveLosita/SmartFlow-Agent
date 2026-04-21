package agentsvc

import (
	"context"
	"io"
	"strings"
	"time"

	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// streamChatFallback 是 graph 执行失败时的降级流式聊天。
// 内联了旧 agentchat.StreamChat 的核心逻辑，不再依赖 agent/ 包。
func (s *AgentService) streamChatFallback(
	ctx context.Context,
	llm *ark.ChatModel,
	modelName string,
	userInput string,
	ifThinking bool,
	chatHistory []*schema.Message,
	outChan chan<- string,
	reasoningStartAt *time.Time,
) (string, string, int, *schema.TokenUsage, error) {
	messages := make([]*schema.Message, 0, len(chatHistory)+2)
	messages = append(messages, schema.SystemMessage(newagentprompt.SystemPrompt))
	if len(chatHistory) > 0 {
		messages = append(messages, chatHistory...)
	}
	messages = append(messages, schema.UserMessage(userInput))

	var thinking *ark.Thinking
	if ifThinking {
		thinking = &arkModel.Thinking{Type: arkModel.ThinkingTypeEnabled}
	} else {
		thinking = &arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}
	}

	if strings.TrimSpace(modelName) == "" {
		modelName = "smartflow-worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	firstChunk := true

	var localReasoningStartAt *time.Time
	if reasoningStartAt != nil && !reasoningStartAt.IsZero() {
		startCopy := reasoningStartAt.In(time.Local)
		localReasoningStartAt = &startCopy
	}
	var reasoningEndAt *time.Time

	reader, err := llm.Stream(ctx, messages, ark.WithThinking(thinking))
	if err != nil {
		return "", "", 0, nil, err
	}
	defer reader.Close()

	var fullText strings.Builder
	var reasoningText strings.Builder
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
			fullText.WriteString(chunk.Content)
			reasoningText.WriteString(chunk.ReasoningContent)
		}

		payload, payloadErr := newagentstream.ToOpenAIStream(chunk, requestID, modelName, created, firstChunk)
		if payloadErr != nil {
			return "", "", 0, nil, payloadErr
		}
		if payload != "" {
			outChan <- payload
			firstChunk = false
		}
	}

	finishChunk, finishErr := newagentstream.ToOpenAIFinishStream(requestID, modelName, created)
	if finishErr != nil {
		return "", "", 0, nil, finishErr
	}
	outChan <- finishChunk
	outChan <- "[DONE]"

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

	return fullText.String(), reasoningText.String(), reasoningDurationSeconds, tokenUsage, nil
}
