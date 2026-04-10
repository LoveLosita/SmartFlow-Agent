package embed

import (
	"context"
	"errors"
	"strings"
	"time"

	openaiembedding "github.com/cloudwego/eino-ext/libs/acl/openai"
	einoembedding "github.com/cloudwego/eino/components/embedding"
)

// EinoConfig 描述 Eino embedding 运行参数。
type EinoConfig struct {
	APIKey    string
	BaseURL   string
	Model     string
	TimeoutMS int
	Dimension int
}

// EinoEmbedder 是基于 Eino 的 embedding 适配器。
//
// 说明：
// 1. 对 infra/rag 暴露统一 []float32 结果，屏蔽 Eino/OpenAI 兼容实现细节；
// 2. 超时由该适配器自身收口，避免业务侧每次调用都手写超时控制；
// 3. 当前底层走 Eino Ext 的 OpenAI 兼容 embedding client，便于接 Ark/OpenAI 兼容接口。
type EinoEmbedder struct {
	client    einoembedding.Embedder
	model     string
	timeout   time.Duration
	dimension int
}

func NewEinoEmbedder(ctx context.Context, cfg EinoConfig) (*EinoEmbedder, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("eino embedder api key is empty")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("eino embedder model is empty")
	}

	clientCfg := &openaiembedding.EmbeddingConfig{
		APIKey:  strings.TrimSpace(cfg.APIKey),
		BaseURL: strings.TrimSpace(cfg.BaseURL),
		Model:   strings.TrimSpace(cfg.Model),
	}
	if cfg.Dimension > 0 {
		clientCfg.Dimensions = &cfg.Dimension
	}

	client, err := openaiembedding.NewEmbeddingClient(ctx, clientCfg)
	if err != nil {
		return nil, err
	}

	timeout := 1200 * time.Millisecond
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}

	return &EinoEmbedder{
		client:    client,
		model:     strings.TrimSpace(cfg.Model),
		timeout:   timeout,
		dimension: cfg.Dimension,
	}, nil
}

func (e *EinoEmbedder) Embed(ctx context.Context, texts []string, _ string) ([][]float32, error) {
	if e == nil || e.client == nil {
		return nil, errors.New("eino embedder is not initialized")
	}
	if len(texts) == 0 {
		return nil, nil
	}

	callCtx := ctx
	cancel := func() {}
	if e.timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, e.timeout)
	}
	defer cancel()

	vectors, err := e.client.EmbedStrings(callCtx, texts, einoembedding.WithModel(e.model))
	if err != nil {
		return nil, err
	}

	result := make([][]float32, 0, len(vectors))
	for _, vector := range vectors {
		converted := make([]float32, len(vector))
		for i, value := range vector {
			converted[i] = float32(value)
		}
		result = append(result, converted)
	}
	return result, nil
}
