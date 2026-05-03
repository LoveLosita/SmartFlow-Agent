package embed

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	openaiembedding "github.com/cloudwego/eino-ext/libs/acl/openai"
	einoembedding "github.com/cloudwego/eino/components/embedding"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
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
// 1. 对 infra/rag 暴露统一 []float32 结果，屏蔽底层 SDK 的实现差异。
// 2. 文本 embedding 继续走当前稳定的 OpenAI 兼容链路，避免无关模型受影响。
// 3. 多模态 embedding 模型单独走 Ark 原生 `/embeddings/multimodal`，解决 vision 模型与标准 `/embeddings` 不兼容的问题。
type EinoEmbedder struct {
	textClient       einoembedding.Embedder
	multimodalClient *arkruntime.Client
	model            string
	timeout          time.Duration
	dimension        int
}

func NewEinoEmbedder(ctx context.Context, cfg EinoConfig) (*EinoEmbedder, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("eino embedder api key is empty")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, errors.New("eino embedder model is empty")
	}

	timeout := 1200 * time.Millisecond
	if cfg.TimeoutMS > 0 {
		timeout = time.Duration(cfg.TimeoutMS) * time.Millisecond
	}

	baseURL := normalizeEmbeddingBaseURL(cfg.BaseURL)
	model := strings.TrimSpace(cfg.Model)
	httpClient := &http.Client{Timeout: timeout}

	// 1. `doubao-embedding-vision-*` 这类模型不支持标准 `/embeddings`。
	// 2. 这里直接切到 Ark 原生多模态 embedding API，避免再依赖错误 endpoint 拼接。
	// 3. 之所以仍保留文本链路，是为了不影响普通 text embedding 模型的既有行为。
	if isMultimodalEmbeddingModel(model) {
		arkOptions := []arkruntime.ConfigOption{
			arkruntime.WithHTTPClient(httpClient),
		}
		if baseURL != "" {
			arkOptions = append(arkOptions, arkruntime.WithBaseUrl(baseURL))
		}

		return &EinoEmbedder{
			multimodalClient: arkruntime.NewClientWithApiKey(
				strings.TrimSpace(cfg.APIKey),
				arkOptions...,
			),
			model:     model,
			timeout:   timeout,
			dimension: cfg.Dimension,
		}, nil
	}

	clientCfg := &openaiembedding.EmbeddingConfig{
		APIKey:     strings.TrimSpace(cfg.APIKey),
		BaseURL:    baseURL,
		Model:      model,
		HTTPClient: httpClient,
	}
	if cfg.Dimension > 0 {
		clientCfg.Dimensions = &cfg.Dimension
	}

	client, err := openaiembedding.NewEmbeddingClient(ctx, clientCfg)
	if err != nil {
		return nil, err
	}

	return &EinoEmbedder{
		textClient: client,
		model:      model,
		timeout:    timeout,
		dimension:  cfg.Dimension,
	}, nil
}

func (e *EinoEmbedder) Embed(ctx context.Context, texts []string, _ string) (result [][]float32, err error) {
	if e == nil {
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

	// 1. 第三方 SDK 一旦 panic，不应该穿透到 RAG 主链路。
	// 2. 这里统一在模型调用边界 recover，并转成 error 交给上层做降级。
	// 3. 这样 memory 主写链路和 agent 主回复链路都不会因为向量同步失败被直接打崩。
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("eino embedder panic recovered: %v", recovered)
			result = nil
		}
	}()

	if e.multimodalClient != nil {
		return e.embedTextsWithMultimodalAPI(callCtx, texts)
	}
	if e.textClient == nil {
		return nil, errors.New("eino embedder client is not initialized")
	}

	vectors, err := e.textClient.EmbedStrings(callCtx, texts, einoembedding.WithModel(e.model))
	if err != nil {
		return nil, err
	}

	result = make([][]float32, 0, len(vectors))
	for _, vector := range vectors {
		converted := make([]float32, len(vector))
		for i, value := range vector {
			converted[i] = float32(value)
		}
		result = append(result, converted)
	}
	return result, nil
}

func (e *EinoEmbedder) embedTextsWithMultimodalAPI(ctx context.Context, texts []string) ([][]float32, error) {
	if e.multimodalClient == nil {
		return nil, errors.New("eino multimodal embedder client is not initialized")
	}

	vectors := make([][]float32, 0, len(texts))
	for _, text := range texts {
		text := text
		req := arkmodel.MultiModalEmbeddingRequest{
			Model: e.model,
			Input: []arkmodel.MultimodalEmbeddingInput{
				{
					Type: arkmodel.MultiModalEmbeddingInputTypeText,
					Text: &text,
				},
			},
		}
		if e.dimension > 0 {
			req.Dimensions = &e.dimension
		}

		// 1. Ark 的多模态 embedding 请求体是“单条内容由多个 part 组成”。
		// 2. 当前 RAG 这里只传文本，因此每段文本单独发一次，避免把多段文本错误拼成同一个 multimodal sample。
		// 3. 一旦后续真的要做批量多模态 embedding，再单独扩展 batch 接口，而不是在这里偷改语义。
		resp, err := e.multimodalClient.CreateMultiModalEmbeddings(ctx, req)
		if err != nil {
			return nil, err
		}

		converted := make([]float32, len(resp.Data.Embedding))
		copy(converted, resp.Data.Embedding)
		vectors = append(vectors, converted)
	}
	return vectors, nil
}

func isMultimodalEmbeddingModel(model string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "doubao-embedding-vision-")
}

func normalizeEmbeddingBaseURL(raw string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(raw), "/")
	if baseURL == "" {
		return ""
	}

	lowerBaseURL := strings.ToLower(baseURL)

	// 1. 配置里应填写 Ark 服务根路径，而不是具体 embedding endpoint。
	// 2. 这里兼容两类常见误配：`/embeddings` 和 `/embeddings/multimodal`。
	// 3. 统一回退到 `/api/v3` 根路径后，再由对应 SDK 自己追加正确后缀，避免最终 URL 重复拼接。
	if strings.HasSuffix(lowerBaseURL, "/embeddings/multimodal") {
		return strings.TrimSuffix(baseURL, baseURL[len(baseURL)-len("/embeddings/multimodal"):])
	}
	if strings.HasSuffix(lowerBaseURL, "/embeddings") {
		return strings.TrimSuffix(baseURL, baseURL[len(baseURL)-len("/embeddings"):])
	}
	return baseURL
}
