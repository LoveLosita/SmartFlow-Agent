package rag

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	ragchunk "github.com/LoveLosita/smartflow/backend/infra/rag/chunk"
	ragconfig "github.com/LoveLosita/smartflow/backend/infra/rag/config"
	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
	ragembed "github.com/LoveLosita/smartflow/backend/infra/rag/embed"
	ragrerank "github.com/LoveLosita/smartflow/backend/infra/rag/rerank"
	ragstore "github.com/LoveLosita/smartflow/backend/infra/rag/store"
)

// FactoryDeps 描述 Runtime 工厂所需的可选依赖。
//
// 说明：
// 1. Logger 仅作为“当前项目尚无统一日志系统”时的默认落点；
// 2. Observer 是正式的统一观测插槽，后续可替换为项目级 logger/metrics/tracing 适配器；
// 3. 业务侧仍然只拿 Runtime，不直接碰底层装配细节。
type FactoryDeps struct {
	Logger   *log.Logger
	Observer Observer
}

// NewRuntimeFromConfig 按配置统一组装 RAG Runtime。
//
// 设计说明：
// 1. 所有底层实现选择都收口到这里，业务侧不再自行 new store/embedder/reranker；
// 2. 即使后续引入更多 provider，也应优先扩展本工厂，而不是把选择逻辑扩散到业务模块；
// 3. 观测能力也在此统一注入，避免 runtime/store/pipeline 各自偷偷打印日志。
func NewRuntimeFromConfig(ctx context.Context, cfg ragconfig.Config, deps FactoryDeps) (Runtime, error) {
	logger, observer := normalizeFactoryDeps(deps)

	embedder, err := buildEmbedder(ctx, cfg)
	if err != nil {
		return nil, err
	}

	store, err := buildStore(cfg, logger, observer)
	if err != nil {
		return nil, err
	}

	reranker, err := buildReranker(cfg, observer)
	if err != nil {
		return nil, err
	}

	pipeline := core.NewPipeline(ragchunk.NewTextChunker(), embedder, store, reranker)
	pipeline.SetLogger(logger)
	pipeline.SetObserver(observer)
	return newRuntime(cfg, pipeline, observer), nil
}

func normalizeFactoryDeps(deps FactoryDeps) (*log.Logger, Observer) {
	logger := deps.Logger
	if logger == nil {
		logger = log.Default()
	}
	observer := deps.Observer
	if observer == nil {
		observer = NewLoggerObserver(logger)
	}
	return logger, observer
}

func buildEmbedder(ctx context.Context, cfg ragconfig.Config) (core.Embedder, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.EmbedProvider)) {
	case "", "mock":
		return ragembed.NewMockEmbedder(cfg.EmbedDimension), nil
	case "eino":
		// 1. RAG embedding 与普通 LLM 链路保持同一套密钥来源，统一直接读取 ARK_API_KEY；
		// 2. 这样可以避免再维护一层 “env 名称配置 -> 再读环境变量” 的间接映射，减少配置分叉；
		// 3. 若后续真的需要多套 embedding 凭据，再显式设计独立字段，而不是继续隐式透传 env 名称。
		apiKey := strings.TrimSpace(os.Getenv("ARK_API_KEY"))
		if apiKey == "" {
			return nil, fmt.Errorf("rag embed api key is empty: env=%s", "ARK_API_KEY")
		}
		return ragembed.NewEinoEmbedder(ctx, ragembed.EinoConfig{
			APIKey:    apiKey,
			BaseURL:   cfg.EmbedBaseURL,
			Model:     cfg.EmbedModel,
			TimeoutMS: cfg.EmbedTimeoutMS,
			Dimension: cfg.EmbedDimension,
		})
	default:
		return nil, fmt.Errorf("unsupported rag embed provider: %s", cfg.EmbedProvider)
	}
}

func buildStore(cfg ragconfig.Config, logger *log.Logger, observer Observer) (core.VectorStore, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Store)) {
	case "", "inmemory":
		return ragstore.NewInMemoryVectorStore(), nil
	case "milvus":
		return ragstore.NewMilvusStore(ragstore.MilvusConfig{
			Address:          cfg.MilvusAddress,
			Token:            cfg.MilvusToken,
			DBName:           cfg.MilvusDBName,
			CollectionName:   cfg.MilvusCollectionName,
			RequestTimeoutMS: cfg.MilvusRequestTimeoutMS,
			Dimension:        cfg.EmbedDimension,
			MetricType:       cfg.MilvusMetricType,
			Logger:           logger,
			Observer:         observer,
		})
	default:
		return nil, fmt.Errorf("unsupported rag store: %s", cfg.Store)
	}
}

func buildReranker(cfg ragconfig.Config, observer Observer) (core.Reranker, error) {
	if !cfg.RerankerEnabled {
		return ragrerank.NewNoopReranker(), nil
	}

	switch strings.ToLower(strings.TrimSpace(cfg.RerankerProvider)) {
	case "", "noop":
		return ragrerank.NewNoopReranker(), nil
	case "eino":
		if observer != nil {
			observer.Observe(context.Background(), ObserveEvent{
				Level:     ObserveLevelWarn,
				Component: "factory",
				Operation: "reranker_fallback",
				Fields: map[string]any{
					"provider":        "eino",
					"status":          "fallback",
					"fallback_target": "noop",
					"reason":          "reranker_not_implemented",
				},
			})
		}
		return ragrerank.NewNoopReranker(), nil
	default:
		return nil, fmt.Errorf("unsupported rag reranker provider: %s", cfg.RerankerProvider)
	}
}
