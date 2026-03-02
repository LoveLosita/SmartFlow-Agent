package inits

import (
	"context"
	"os"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/spf13/viper"
)

// AIHub 存储不同能力的模型实例
type AIHub struct {
	Strategist *ark.ChatModel // 智力担当：处理复杂排程逻辑
	Worker     *ark.ChatModel // 效率担当：处理简单任务、总结
}

func InitEino() (*AIHub, error) {
	ctx := context.Background()
	worker, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.workerModel"), // 使用的模型版本
		BaseURL: viper.GetString("agent.baseURL"),     // Eino API 的基础 URL
		APIKey:  os.Getenv("ARK_API_KEY"),             // API 密钥
	})
	if err != nil {
		return nil, err
	}
	strategist, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.strategistModel"), // 使用的模型版本
		BaseURL: viper.GetString("agent.baseURL"),         // Eino API 的基础 URL
		APIKey:  os.Getenv("ARK_API_KEY"),                 // API 密钥
	})
	if err != nil {
		return nil, err
	}
	return &AIHub{
		Strategist: strategist,
		Worker:     worker,
	}, nil
}
