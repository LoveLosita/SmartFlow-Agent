package llm

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// ArkCallOptions 是直接调用 ark.ChatModel 时使用的通用入参。
type ArkCallOptions struct {
	Temperature float64
	MaxTokens   int
	Thinking    ThinkingMode
}

// CallArkText 调用 ark 模型并返回纯文本。
func CallArkText(ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, options ArkCallOptions) (string, error) {
	if chatModel == nil {
		return "", errors.New("ark model is nil")
	}

	messages := []*schema.Message{
		schema.SystemMessage(systemPrompt),
		schema.UserMessage(userPrompt),
	}
	resp, err := chatModel.Generate(ctx, messages, buildArkOptions(options)...)
	if err != nil {
		return "", err
	}
	if resp == nil {
		return "", errors.New("模型返回为空")
	}

	text := strings.TrimSpace(resp.Content)
	if text == "" {
		return "", errors.New("模型返回内容为空")
	}
	return text, nil
}

// CallArkJSON 调用 ark 模型并直接解析 JSON。
func CallArkJSON[T any](ctx context.Context, chatModel *ark.ChatModel, systemPrompt, userPrompt string, options ArkCallOptions) (*T, string, error) {
	raw, err := CallArkText(ctx, chatModel, systemPrompt, userPrompt, options)
	if err != nil {
		return nil, "", err
	}
	parsed, err := ParseJSONObject[T](raw)
	if err != nil {
		return nil, raw, err
	}
	return parsed, raw, nil
}

func buildArkOptions(options ArkCallOptions) []einoModel.Option {
	thinkingType := arkModel.ThinkingTypeDisabled
	if options.Thinking == ThinkingModeEnabled {
		thinkingType = arkModel.ThinkingTypeEnabled
	}

	opts := []einoModel.Option{
		ark.WithThinking(&arkModel.Thinking{Type: thinkingType}),
		einoModel.WithTemperature(float32(options.Temperature)),
	}
	if options.MaxTokens > 0 {
		opts = append(opts, einoModel.WithMaxTokens(options.MaxTokens))
	}
	return opts
}
