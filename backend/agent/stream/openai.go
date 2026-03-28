package agentstream

import (
	"encoding/json"

	"github.com/cloudwego/eino/schema"
)

// OpenAIChunkResponse 是 OpenAI 兼容的流式 chunk DTO。
//
// 之所以单独放到 Agent/stream：
// 1. 未来无论 quicknote、taskquery 还是 schedule，只要需要 SSE 都会复用这套协议壳；
// 2. 这样 node/graph 层只关注“我要推什么内容”，不再自己拼 JSON；
// 3. 后续如果前端协议升级，也能在这里集中改。
type OpenAIChunkResponse struct {
	ID      string              `json:"id"`
	Object  string              `json:"object"`
	Created int64               `json:"created"`
	Model   string              `json:"model"`
	Choices []OpenAIChunkChoice `json:"choices"`
}

// OpenAIChunkChoice 对应 OpenAI choices[0]。
type OpenAIChunkChoice struct {
	Index        int              `json:"index"`
	Delta        OpenAIChunkDelta `json:"delta"`
	FinishReason *string          `json:"finish_reason"`
}

// OpenAIChunkDelta 是真正承载 role/content/reasoning 的位置。
type OpenAIChunkDelta struct {
	Role             string `json:"role,omitempty"`
	Content          string `json:"content,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// ToOpenAIStream 把 Eino message 转成 OpenAI 兼容 chunk。
//
// 职责边界：
// 1. 负责把 chunk.Content / chunk.ReasoningContent 映射到协议字段；
// 2. 负责按 includeRole 决定是否在首块带上 assistant 角色；
// 3. 不负责发送，也不负责决定“这个 chunk 该不该推”。
func ToOpenAIStream(chunk *schema.Message, requestID, modelName string, created int64, includeRole bool) (string, error) {
	delta := OpenAIChunkDelta{}
	if includeRole {
		delta.Role = "assistant"
	}
	if chunk != nil {
		delta.Content = chunk.Content
		delta.ReasoningContent = chunk.ReasoningContent
	}
	return buildOpenAIChunkPayload(requestID, modelName, created, delta, nil)
}

// ToOpenAIReasoningChunk 直接构造一个 reasoning chunk。
func ToOpenAIReasoningChunk(requestID, modelName string, created int64, reasoning string, includeRole bool) (string, error) {
	delta := OpenAIChunkDelta{ReasoningContent: reasoning}
	if includeRole {
		delta.Role = "assistant"
	}
	return buildOpenAIChunkPayload(requestID, modelName, created, delta, nil)
}

// ToOpenAIAssistantChunk 直接构造一个正文 chunk。
func ToOpenAIAssistantChunk(requestID, modelName string, created int64, content string, includeRole bool) (string, error) {
	delta := OpenAIChunkDelta{Content: content}
	if includeRole {
		delta.Role = "assistant"
	}
	return buildOpenAIChunkPayload(requestID, modelName, created, delta, nil)
}

// ToOpenAIFinishStream 生成流式结束 chunk（finish_reason=stop）。
func ToOpenAIFinishStream(requestID, modelName string, created int64) (string, error) {
	stop := "stop"
	return buildOpenAIChunkPayload(requestID, modelName, created, OpenAIChunkDelta{}, &stop)
}

func buildOpenAIChunkPayload(requestID, modelName string, created int64, delta OpenAIChunkDelta, finishReason *string) (string, error) {
	// 1. 若既没有 role，也没有正文/思考，也没有 finish_reason，则视为“空块”，直接跳过。
	// 2. 这样可以避免上层每次都自己写一遍空块判断。
	if delta.Role == "" && delta.Content == "" && delta.ReasoningContent == "" && finishReason == nil {
		return "", nil
	}

	dto := OpenAIChunkResponse{
		ID:      requestID,
		Object:  "chat.completion.chunk",
		Created: created,
		Model:   modelName,
		Choices: []OpenAIChunkChoice{{
			Index:        0,
			Delta:        delta,
			FinishReason: finishReason,
		}},
	}
	data, err := json.Marshal(dto)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
