package llm

import (
	"context"
	"errors"
	"io"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// WrapArkClient 将 ark.ChatModel 适配为统一 Client。
// 1. generateText 走 Generate，供 GenerateJSON/GenerateText 使用。
// 2. streamText 走 Stream，供需要流式输出的场景使用。
// 3. 两条路径共用同一套参数转换逻辑。
func WrapArkClient(arkChatModel *ark.ChatModel) *Client {
	if arkChatModel == nil {
		return nil
	}

	generateFunc := func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (*TextResult, error) {
		arkOpts := buildArkStreamOptions(options)
		msg, err := arkChatModel.Generate(ctx, messages, arkOpts...)
		if err != nil {
			return nil, err
		}
		if msg == nil {
			return nil, errors.New("ark model returned nil message")
		}

		var usage *schema.TokenUsage
		finishReason := ""
		if msg.ResponseMeta != nil {
			usage = CloneUsage(msg.ResponseMeta.Usage)
			finishReason = msg.ResponseMeta.FinishReason
		}

		return &TextResult{
			Text:         msg.Content,
			Usage:        usage,
			FinishReason: finishReason,
		}, nil
	}

	streamFunc := func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (StreamReader, error) {
		arkOpts := buildArkStreamOptions(options)
		reader, err := arkChatModel.Stream(ctx, messages, arkOpts...)
		if err != nil {
			return nil, err
		}
		return &arkStreamReaderAdapter{reader: reader}, nil
	}

	return NewClient(generateFunc, streamFunc)
}

// buildArkStreamOptions 将统一的 GenerateOptions 转换为 ark 的流式调用参数。
func buildArkStreamOptions(options GenerateOptions) []einoModel.Option {
	thinkingEnabled := options.Thinking == ThinkingModeEnabled

	thinkingType := arkModel.ThinkingTypeDisabled
	if thinkingEnabled {
		thinkingType = arkModel.ThinkingTypeEnabled
	}
	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: thinkingType}),
	}

	if thinkingEnabled {
		opts = append(opts, einoModel.WithTemperature(1.0))
	} else if options.Temperature > 0 {
		opts = append(opts, einoModel.WithTemperature(float32(options.Temperature)))
	}

	maxTokens := options.MaxTokens
	if thinkingEnabled {
		const minThinkingBudget = 16000
		if maxTokens < minThinkingBudget {
			maxTokens = minThinkingBudget
		}
	}
	if maxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(maxTokens))
	}

	return opts
}

// arkStreamReaderAdapter 把 ark 的流式 reader 转成统一的 StreamReader 接口。
type arkStreamReaderAdapter struct {
	reader *schema.StreamReader[*schema.Message]
}

// Recv 转发到底层 reader。
func (r *arkStreamReaderAdapter) Recv() (*schema.Message, error) {
	if r == nil || r.reader == nil {
		return nil, io.EOF
	}
	return r.reader.Recv()
}

// Close 适配 ark reader 的 Close 行为。
func (r *arkStreamReaderAdapter) Close() error {
	if r == nil || r.reader == nil {
		return nil
	}
	r.reader.Close()
	return nil
}
