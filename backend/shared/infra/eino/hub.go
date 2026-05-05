package einoinfra

import (
	"context"
	"os"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/spf13/viper"
)

// AIHub 承载当前进程内复用的 Ark ChatModel 句柄。
type AIHub struct {
	Lite *ark.ChatModel
	Pro  *ark.ChatModel
	Max  *ark.ChatModel
}

// InitEino 按统一配置创建当前进程复用的 Eino 模型句柄。
func InitEino() (*AIHub, error) {
	ctx := context.Background()
	baseURL := viper.GetString("agent.baseURL")
	apiKey := os.Getenv("ARK_API_KEY")

	lite, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.liteModel"),
		BaseURL: baseURL,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, err
	}

	pro, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.proModel"),
		BaseURL: baseURL,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, err
	}

	maxModel, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.maxModel"),
		BaseURL: baseURL,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, err
	}

	return &AIHub{
		Lite: lite,
		Pro:  pro,
		Max:  maxModel,
	}, nil
}
