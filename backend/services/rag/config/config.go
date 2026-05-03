package config

import "github.com/spf13/viper"

// Config 是 RAG Core 运行配置。
type Config struct {
	Enabled bool
	Store   string
	TopK    int

	Threshold float64

	EmbedProvider  string
	EmbedModel     string
	EmbedBaseURL   string
	EmbedTimeoutMS int
	EmbedDimension int

	RerankerEnabled   bool
	RerankerProvider  string
	RerankerTimeoutMS int

	ChunkSize    int
	ChunkOverlap int

	RetrieveTimeoutMS int

	MilvusAddress          string
	MilvusToken            string
	MilvusDBName           string
	MilvusCollectionName   string
	MilvusMetricType       string
	MilvusRequestTimeoutMS int
}

// LoadFromViper 读取 rag 配置并补默认值。
func LoadFromViper() Config {
	cfg := Config{
		Enabled:                viper.GetBool("rag.enabled"),
		Store:                  viper.GetString("rag.store"),
		TopK:                   viper.GetInt("rag.topK"),
		Threshold:              viper.GetFloat64("rag.threshold"),
		EmbedProvider:          viper.GetString("rag.embed.provider"),
		EmbedModel:             viper.GetString("rag.embed.model"),
		EmbedBaseURL:           viper.GetString("rag.embed.baseURL"),
		EmbedTimeoutMS:         viper.GetInt("rag.embed.timeoutMs"),
		EmbedDimension:         viper.GetInt("rag.embed.dimension"),
		RerankerEnabled:        viper.GetBool("rag.reranker.enabled"),
		RerankerProvider:       viper.GetString("rag.reranker.provider"),
		RerankerTimeoutMS:      viper.GetInt("rag.reranker.timeoutMs"),
		ChunkSize:              viper.GetInt("rag.ingest.chunkSize"),
		ChunkOverlap:           viper.GetInt("rag.ingest.chunkOverlap"),
		RetrieveTimeoutMS:      viper.GetInt("rag.retrieve.timeoutMs"),
		MilvusAddress:          viper.GetString("rag.milvus.address"),
		MilvusToken:            viper.GetString("rag.milvus.token"),
		MilvusDBName:           viper.GetString("rag.milvus.dbName"),
		MilvusCollectionName:   viper.GetString("rag.milvus.collectionName"),
		MilvusMetricType:       viper.GetString("rag.milvus.metricType"),
		MilvusRequestTimeoutMS: viper.GetInt("rag.milvus.requestTimeoutMs"),
	}
	if cfg.Store == "" {
		cfg.Store = "inmemory"
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 8
	}
	if cfg.Threshold < 0 {
		cfg.Threshold = 0
	}
	if cfg.EmbedProvider == "" {
		cfg.EmbedProvider = "mock"
	}
	if cfg.EmbedBaseURL == "" {
		cfg.EmbedBaseURL = viper.GetString("agent.baseURL")
	}
	if cfg.EmbedTimeoutMS <= 0 {
		cfg.EmbedTimeoutMS = 1200
	}
	if cfg.EmbedDimension <= 0 {
		cfg.EmbedDimension = 1024
	}
	if cfg.RerankerProvider == "" {
		cfg.RerankerProvider = "noop"
	}
	if cfg.RerankerTimeoutMS <= 0 {
		cfg.RerankerTimeoutMS = 1200
	}
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 400
	}
	if cfg.ChunkOverlap < 0 {
		cfg.ChunkOverlap = 80
	}
	if cfg.RetrieveTimeoutMS <= 0 {
		cfg.RetrieveTimeoutMS = 1500
	}
	if cfg.MilvusAddress == "" {
		cfg.MilvusAddress = "http://localhost:19530"
	}
	if cfg.MilvusToken == "" {
		cfg.MilvusToken = "root:Milvus"
	}
	if cfg.MilvusCollectionName == "" {
		cfg.MilvusCollectionName = "smartflow_rag_chunks"
	}
	if cfg.MilvusMetricType == "" {
		cfg.MilvusMetricType = "COSINE"
	}
	if cfg.MilvusRequestTimeoutMS <= 0 {
		cfg.MilvusRequestTimeoutMS = 1500
	}
	return cfg
}
