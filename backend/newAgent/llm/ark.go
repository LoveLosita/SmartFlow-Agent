package newagentllm

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudwego/eino-ext/components/model/ark"
	einoModel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	arkModel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

// ArkCallOptions 是基于 ark.ChatModel 的通用调用选项。
//
// 设计目的：
// 1. 当前 route / quicknote 都还直接持有 *ark.ChatModel；
// 2. 在它们完全收敛到更抽象的 Client 前，先把重复的 ark 调用样板抽成公共层；
// 3. 这样本轮就能先删除 route/quicknote 里那几份重复的 Generate 样板代码。
type ArkCallOptions struct {
	Temperature float64
	MaxTokens   int
	Thinking    ThinkingMode
}

// CallArkText 调用 ark 模型并返回纯文本。
//
// 职责边界：
// 1. 负责拼 system + user 两段消息；
// 2. 负责统一配置 thinking / temperature / maxTokens；
// 3. 负责拦截空响应；
// 4. 不负责 JSON 解析，不负责业务字段校验。
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
