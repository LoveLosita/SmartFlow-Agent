package inits

import (
	"context"
	"os"

	"github.com/cloudwego/eino-ext/components/model/ark"
	"github.com/spf13/viper"
)

// AIHub 存储三级模型的实例，按能力分级调度。
//
// 分级策略：
// 1. Lite：轻量模型，用于标题生成等低复杂度、低延迟场景；
// 2. Pro：标准模型，用于 Chat 路由/闲聊/深度回答/Deliver 总结；
// 3. Max：高能力模型，用于 Plan 规划和 Execute ReAct 等需要深度推理的场景。
type AIHub struct {
	Lite *ark.ChatModel // 轻量模型：标题生成等低复杂度任务
	Pro  *ark.ChatModel // 标准模型：Chat 路由、闲聊、交付总结
	Max  *ark.ChatModel // 高能力模型：Plan 规划、Execute ReAct
}

func InitEino() (*AIHub, error) {
	ctx := context.Background()
	baseURL := viper.GetString("agent.baseURL")
	apiKey := os.Getenv("ARK_API_KEY")

	// 1. Lite 模型：标题生成等低复杂度场景，优先控制成本和延迟。
	lite, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.liteModel"),
		BaseURL: baseURL,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, err
	}
	// 2. Pro 模型：Chat 路由/闲聊/交付总结等标准复杂度场景。
	pro, err := ark.NewChatModel(ctx, &ark.ChatModelConfig{
		Model:   viper.GetString("agent.proModel"),
		BaseURL: baseURL,
		APIKey:  apiKey,
	})
	if err != nil {
		return nil, err
	}
	// 3. Max 模型：Plan 规划和 Execute ReAct 等需要深度推理的场景。
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
