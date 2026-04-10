package config

import "github.com/spf13/viper"

// Config 是 RAG Core 运行配置。
type Config struct {
	Enabled bool
	TopK    int

	Threshold float64

	RerankerEnabled   bool
	RerankerTimeoutMS int

	ChunkSize    int
	ChunkOverlap int

	RetrieveTimeoutMS int
}

// LoadFromViper 读取 rag 配置并补默认值。
func LoadFromViper() Config {
	cfg := Config{
		Enabled:           viper.GetBool("rag.enabled"),
		TopK:              viper.GetInt("rag.topK"),
		Threshold:         viper.GetFloat64("rag.threshold"),
		RerankerEnabled:   viper.GetBool("rag.reranker.enabled"),
		RerankerTimeoutMS: viper.GetInt("rag.reranker.timeoutMs"),
		ChunkSize:         viper.GetInt("rag.ingest.chunkSize"),
		ChunkOverlap:      viper.GetInt("rag.ingest.chunkOverlap"),
		RetrieveTimeoutMS: viper.GetInt("rag.retrieve.timeoutMs"),
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 8
	}
	if cfg.Threshold < 0 {
		cfg.Threshold = 0
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
	return cfg
}
