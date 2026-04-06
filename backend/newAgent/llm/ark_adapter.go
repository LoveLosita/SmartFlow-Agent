package newagentllm

import (
	"context"
	"errors"
	"io"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// WrapArkClient 将 ark.ChatModel 适配为 newAgent 的统一 Client。
//
// 职责边界：
// 1. generateText：调用 ark.ChatModel.Generate（非流式），供 GenerateJSON 使用；
// 2. streamText：调用 ark.ChatModel.Stream（流式），供 EmitPseudoAssistantText 等使用；
// 3. 两者共用 buildArkStreamOptions 统一构造调用选项。
func WrapArkClient(arkChatModel *ark.ChatModel) *Client {
	if arkChatModel == nil {
		return nil
	}

	// 非流式文本生成，供 GenerateJSON / GenerateText 调用路径使用。
	generateFunc := func(ctx context.Context, messages []*schema.Message, options GenerateOptions) (*TextResult, error) {
		arkOpts := buildArkStreamOptions(options)
		msg, err := arkChatModel.Generate(ctx, messages, arkOpts...)
		if err != nil {
			return nil, err
		}
		if msg == nil {
			return nil, errors.New("ark model returned nil message")
		}
		return &TextResult{Text: msg.Content}, nil
	}

	// 流式文本生成。
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

// buildArkStreamOptions 将 newAgent 的 GenerateOptions 转换为 ark 的流式调用选项。
func buildArkStreamOptions(options GenerateOptions) []einoModel.Option {
	// Thinking
	thinkingType := arkModel.ThinkingTypeDisabled
	if options.Thinking == ThinkingModeEnabled {
		thinkingType = arkModel.ThinkingTypeEnabled
	}

	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: thinkingType}),
	}

	// Temperature
	if options.Temperature > 0 {
		opts = append(opts, einoModel.WithTemperature(float32(options.Temperature)))
	}

	// MaxTokens
	if options.MaxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(options.MaxTokens))
	}

	return opts
}

// arkStreamReaderAdapter 适配 ark.ChatModel.Stream 返回的 reader。
// ark.Stream 返回 schema.StreamReader[*schema.Message]，其 Close() 方法无返回值
// 而我们的 StreamReader 接口要求 Close() error
type arkStreamReaderAdapter struct {
	reader *schema.StreamReader[*schema.Message]
}

// Recv 转发到 ark reader 的 Recv 方法。
func (r *arkStreamReaderAdapter) Recv() (*schema.Message, error) {
	if r == nil || r.reader == nil {
		return nil, io.EOF
	}
	return r.reader.Recv()
}

// Close 转发到 ark reader 的 Close 方法。
// ark 的 Close() 无返回值，我们适配为返回 nil
func (r *arkStreamReaderAdapter) Close() error {
	if r == nil || r.reader == nil {
		return nil
	}
	r.reader.Close()
	return nil
}
