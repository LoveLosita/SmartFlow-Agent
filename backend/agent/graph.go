package agent

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

// StreamResponse 为 OpenAI/DeepSeek 兼容的流式 chunk 结构。
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

func StreamChat(ctx context.Context, llm *ark.ChatModel, modelName string, userInput string, ifThinking bool, chatHistory []*schema.Message, outChan chan<- string) (string, error) {
	// 1) 组装提示消息
	messages := make([]*schema.Message, 0)
	messages = append(messages, schema.SystemMessage(SystemPrompt))
	if len(chatHistory) > 0 {
		messages = append(messages, chatHistory...)
	}
	messages = append(messages, schema.UserMessage(userInput))

	// 2) 发起流式请求
	var thinking *ark.Thinking
	if ifThinking {
		thinking = &arkModel.Thinking{Type: arkModel.ThinkingTypeEnabled}
	} else {
		thinking = &arkModel.Thinking{Type: arkModel.ThinkingTypeDisabled}
	}
	reader, err := llm.Stream(ctx, messages, ark.WithThinking(thinking))
	if err != nil {
		return "", err
	}
	defer reader.Close()

	if strings.TrimSpace(modelName) == "" {
		modelName = "smartflow-worker"
	}
	requestID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	firstChunk := true

	// 3) 持续转发 chunk
	var fullText strings.Builder
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		fullText.WriteString(chunk.Content)

		payload, err := ToOpenAIStream(chunk, requestID, modelName, created, firstChunk)
		if err != nil {
			return "", err
		}
		if payload != "" {
			outChan <- payload
			firstChunk = false
		}
	}

	// 4) 发送结束 chunk 和 [DONE]
	finishChunk, err := ToOpenAIFinishStream(requestID, modelName, created)
	if err != nil {
		return "", err
	}
	outChan <- finishChunk
	outChan <- "[DONE]"

	return fullText.String(), nil
}
