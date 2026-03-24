package agentchat

import (
	"context"
	"io"
	"strings"
	"time"

	agentllm "github.com/LoveLosita/smartflow/backend/agent2/llm"
	agentstream "github.com/LoveLosita/smartflow/backend/agent2/stream"
	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

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
			tokenUsage = agentllm.MergeUsage(tokenUsage, chunk.ResponseMeta.Usage)
		}

		fullText.WriteString(chunk.Content)

		payload, err := agentstream.ToOpenAIStream(chunk, requestID, modelName, created, firstChunk)
		if err != nil {
			return "", nil, err
		}
		if payload != "" {
			outChan <- payload
			chunkCount++
			firstChunk = false
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

	finishChunk, err := agentstream.ToOpenAIFinishStream(requestID, modelName, created)
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
