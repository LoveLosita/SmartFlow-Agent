package agent

import (
	"context"
	"encoding/json"
	"io"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
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

// ToOpenAIStream 负责将 Eino 的内部 Chunk 转换为 OpenAI 兼容的 data: {JSON} 字符串
func ToOpenAIStream(chunk *schema.Message) (string, error) {
	dto := ToStreamResponseDTO(chunk)

	jsonBytes, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	// 严格遵循 SSE 协议格式
	return string(jsonBytes), nil
}

func StreamChat(ctx context.Context, llm *ark.ChatModel, userInput string, outChan chan<- string) error {
	// 1. 组装消息
	messages := []*schema.Message{
		schema.SystemMessage("你是一位时间管理大师兼日程安排专家兼个人助理，协助用户高效安排日程，优化时间利用率。"),
		schema.UserMessage(userInput),
	}

	// 2. 调用流式接口
	reader, err := llm.Stream(ctx, messages)
	if err != nil {
		return err
	}
	defer reader.Close() // 记得关闭 Reader

	// 3. 循环读取直到结束
	for {
		chunk, err := reader.Recv()
		if err == io.EOF {
			break // 读取完成
		}
		if err != nil {
			return err
		}
		// 将内容发送到通道中供前端消费
		retChuck, err := ToOpenAIStream(chunk)
		if err != nil {
			return err
		}
		outChan <- retChuck
	}
	return nil
}
