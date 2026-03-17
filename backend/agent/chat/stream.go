package chat

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// StreamResponse 是 OpenAI/DeepSeek 兼容的流式 chunk 结构。
type StreamResponse struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Created int64          `json:"created"`
	Model   string         `json:"model"`
	Choices []StreamChoice `json:"choices"`
}

type StreamChoice struct {
	Index        int         `json:"index"`
	Delta        StreamDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type StreamDelta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ToOpenAIStream 将单个 Eino chunk 转为 OpenAI 兼容 JSON。
func ToOpenAIStream(chunk *schema.Message, requestID, modelName string, created int64, includeRole bool) (string, error) {
	delta := StreamDelta{}
	if includeRole {
		delta.Role = "assistant"
	}
	if chunk != nil {
		delta.Content = chunk.Content
		delta.ReasoningContent = chunk.ReasoningContent
	}

	if delta.Role == "" && delta.Content == "" && delta.ReasoningContent == "" {
		return "", nil
	}

	dto := StreamResponse{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   modelName,
		Choices: []StreamChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: nil,
		}},
	}
	jsonBytes, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// ToOpenAIFinishStream 生成结束 chunk（finish_reason=stop）。
func ToOpenAIFinishStream(requestID, modelName string, created int64) (string, error) {
	stop := "stop"
	dto := StreamResponse{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   modelName,
		Choices: []StreamChoice{{
			Index:        0,
			Delta:        StreamDelta{},
			FinishReason: &stop,
		}},
	}
	jsonBytes, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	return string(jsonBytes), nil
}

// StreamChat 负责模型流式输出，并在关键节点打点：
// 1) 流连接建立（llm.Stream 返回）
// 2) 首包到达（首字延迟）
// 3) 流式输出结束
func StreamChat(
	ctx context.Context,
	llm *ark.ChatModel,
	modelName string,
	userInput string,
	ifThinking bool,
	chatHistory []*schema.Message,
	outChan chan<- string,
	traceID string,
	chatID string,
	requestStart time.Time,
) (string, *schema.TokenUsage, error) {
	/*callStart := time.Now()*/

	messages := make([]*schema.Message, 0)
	messages = append(messages, schema.SystemMessage(SystemPrompt))
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

	/*connectStart := time.Now()*/
	reader, err := llm.Stream(ctx, messages, ark.WithThinking(thinking))
	if err != nil {
		return "", nil, err
	}
	defer reader.Close()

	if strings.TrimSpace(modelName) == "" {
		modelName = "smartflow-worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	firstChunk := true
	chunkCount := 0
	var tokenUsage *schema.TokenUsage
	/*streamRecvStart := time.Now()

	log.Printf("打点|流连接建立|trace_id=%s|chat_id=%s|request_id=%s|本步耗时_ms=%d|请求累计_ms=%d|history_len=%d",
		traceID,
		chatID,
		requestID,
		time.Since(connectStart).Milliseconds(),
		time.Since(requestStart).Milliseconds(),
		len(chatHistory),
	)*/

	var fullText strings.Builder
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", nil, err
		}

		// 优先记录模型真实 usage（通常在尾块返回，部分模型也可能中途返回）。
		if chunk != nil && chunk.ResponseMeta != nil && chunk.ResponseMeta.Usage != nil {
			tokenUsage = mergeTokenUsage(tokenUsage, chunk.ResponseMeta.Usage)
		}

		fullText.WriteString(chunk.Content)

		payload, err := ToOpenAIStream(chunk, requestID, modelName, created, firstChunk)
		if err != nil {
			return "", nil, err
		}
		if payload != "" {
			outChan <- payload
			chunkCount++
			/*if firstChunk {
				log.Printf("打点|首包到达|trace_id=%s|chat_id=%s|request_id=%s|本步耗时_ms=%d|请求累计_ms=%d",
					traceID,
					chatID,
					requestID,
					time.Since(streamRecvStart).Milliseconds(),
					time.Since(requestStart).Milliseconds(),
				)
				firstChunk = false
			}*/
		}
	}

	finishChunk, err := ToOpenAIFinishStream(requestID, modelName, created)
	if err != nil {
		return "", nil, err
	}
	outChan <- finishChunk
	outChan <- "[DONE]"

	/*log.Printf("打点|流式输出结束|trace_id=%s|chat_id=%s|request_id=%s|chunks=%d|reply_chars=%d|本步耗时_ms=%d|请求累计_ms=%d",
		traceID,
		chatID,
		requestID,
		chunkCount,
		len(fullText.String()),
		time.Since(callStart).Milliseconds(),
		time.Since(requestStart).Milliseconds(),
	)*/

	return fullText.String(), tokenUsage, nil
}

// mergeTokenUsage 合并流式分片中的 usage。
//
// 设计说明：
// 1. 不同模型的 usage 回传时机不同（中间块/尾块）；
// 2. 这里按“更大值覆盖”合并，确保最终拿到完整统计；
// 3. 只用于统计，不影响流式正文输出。
func mergeTokenUsage(base *schema.TokenUsage, incoming *schema.TokenUsage) *schema.TokenUsage {
	if incoming == nil {
		return base
	}
	if base == nil {
		copied := *incoming
		return &copied
	}

	merged := *base
	if incoming.PromptTokens > merged.PromptTokens {
		merged.PromptTokens = incoming.PromptTokens
	}
	if incoming.CompletionTokens > merged.CompletionTokens {
		merged.CompletionTokens = incoming.CompletionTokens
	}
	if incoming.TotalTokens > merged.TotalTokens {
		merged.TotalTokens = incoming.TotalTokens
	}
	if incoming.PromptTokenDetails.CachedTokens > merged.PromptTokenDetails.CachedTokens {
		merged.PromptTokenDetails.CachedTokens = incoming.PromptTokenDetails.CachedTokens
	}
	if incoming.CompletionTokensDetails.ReasoningTokens > merged.CompletionTokensDetails.ReasoningTokens {
		merged.CompletionTokensDetails.ReasoningTokens = incoming.CompletionTokensDetails.ReasoningTokens
	}
	return &merged
}
