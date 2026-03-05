package agent

import (
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// StreamResponse 专为 Apifox/前端 识别设计的极简结构
type StreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

// ToStreamResponseDTO 将 Eino 的内部 Chunk 转换为 StreamResponse DTO
func ToStreamResponseDTO(chunk *schema.Message) StreamResponse {
	var dto StreamResponse
	dto.Choices = append(dto.Choices, struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	}{})
	dto.Choices[0].Delta.Content = chunk.Content
	return dto
}

func ToStreamReasoningResponseDTO(chunk *schema.Message) StreamResponse {
	var dto StreamResponse
	dto.Choices = append(dto.Choices, struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	}{})
	dto.Choices[0].Delta.Content = chunk.ReasoningContent
	return dto
}

// ToOpenAIStream 负责将 Eino 的内部 Chunk 转换为 OpenAI 兼容的 data: {JSON} 字符串
func ToOpenAIStream(chunk *schema.Message) (string, error) {
	var dto StreamResponse
	if chunk.ReasoningContent != "" {
		dto = ToStreamReasoningResponseDTO(chunk)
	} else {
		dto = ToStreamResponseDTO(chunk)
	}
	jsonBytes, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	// 严格遵循 SSE 协议格式
	return string(jsonBytes), nil
}

func StreamChat(ctx context.Context, llm *ark.ChatModel, userInput string, ifThinking bool, chatHistory []*schema.Message, outChan chan<- string) (string, error) {
	// 1. 组装消息
	messages := make([]*schema.Message, 0)
	// A. 塞入 System Message (人设)
	messages = append(messages, schema.SystemMessage(SystemPrompt))
	// B. 塞入历史记录 (上下文)
	if len(chatHistory) > 0 {
		messages = append(messages, chatHistory...)
	}
	// C. 塞入用户当前的消息 (当前需求)
	messages = append(messages, schema.UserMessage(userInput))
	// 2. 调用流式接口
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
	defer reader.Close() // 记得关闭 Reader

	// 3. 循环读取直到结束
	var fullText strings.Builder
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break // 读取完成
		}
		if err != nil {
			return "", err
		}
		/*if chunk.Content == "" { // 过滤掉空内容，避免发送无效消息
			continue
		}*/
		fullText.WriteString(chunk.Content)
		// 将内容发送到通道中供前端消费
		retChuck, err := ToOpenAIStream(chunk)
		if err != nil {
			return "", err
		}
		outChan <- retChuck
	}
	return fullText.String(), nil
}
