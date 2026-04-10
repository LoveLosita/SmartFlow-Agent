package core

import "time"

// SourceDocument 是统一语料文档模型。
//
// 职责边界：
// 1. 只描述“可被切块与索引”的原始文档；
// 2. 不承载业务流程状态。
type SourceDocument struct {
	ID        string
	Text      string
	Title     string
	Metadata  map[string]any
	CreatedAt time.Time
}

// Chunk 是标准切块结果。
type Chunk struct {
	ID         string
	DocumentID string
	Text       string
	Order      int
	Metadata   map[string]any
}

// ChunkOption 控制切块参数。
type ChunkOption struct {
	ChunkSize    int
	ChunkOverlap int
}

// IngestOption 控制入库参数。
type IngestOption struct {
	Chunk ChunkOption
	// Action 用于 embedding 分型（add/update/search）。
	Action string
}

// IngestResult 描述一次入库执行摘要。
type IngestResult struct {
	DocumentCount int
	ChunkCount    int
}

// RetrieveRequest 是统一检索请求。
type RetrieveRequest struct {
	Query       string
	TopK        int
	Threshold   float64
	Action      string
	Filter      map[string]any
	CorpusInput any
}

// ScoredChunk 是统一召回结果。
type ScoredChunk struct {
	ChunkID    string
	DocumentID string
	Text       string
	Score      float64
	Metadata   map[string]any
}

// RetrieveResult 是检索链路执行摘要。
type RetrieveResult struct {
	Items          []ScoredChunk
	RawCount       int
	FallbackUsed   bool
	FallbackReason string
}

// VectorRow 是向量存储标准行。
type VectorRow struct {
	ID        string
	Vector    []float32
	Text      string
	Metadata  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// VectorSearchRequest 是向量检索请求。
type VectorSearchRequest struct {
	QueryVector []float32
	TopK        int
	Filter      map[string]any
}

// ScoredVectorRow 是向量检索结果。
type ScoredVectorRow struct {
	Row   VectorRow
	Score float64
}
