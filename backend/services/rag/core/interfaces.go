package core

import "context"

// Chunker 负责文本切块。
type Chunker interface {
	Chunk(ctx context.Context, doc SourceDocument, opt ChunkOption) ([]Chunk, error)
}

// Embedder 负责向量化。
type Embedder interface {
	Embed(ctx context.Context, texts []string, action string) ([][]float32, error)
}

// Retriever 负责召回候选。
type Retriever interface {
	Retrieve(ctx context.Context, req RetrieveRequest) ([]ScoredChunk, error)
}

// Reranker 负责重排候选。
type Reranker interface {
	Rerank(ctx context.Context, query string, candidates []ScoredChunk, topK int) ([]ScoredChunk, error)
}

// VectorStore 负责向量库读写。
type VectorStore interface {
	Upsert(ctx context.Context, rows []VectorRow) error
	Search(ctx context.Context, req VectorSearchRequest) ([]ScoredVectorRow, error)
	Delete(ctx context.Context, ids []string) error
	Get(ctx context.Context, ids []string) ([]VectorRow, error)
}

// CorpusAdapter 负责把业务语料映射成统一文档/过滤条件。
type CorpusAdapter interface {
	Name() string
	BuildIngestDocuments(ctx context.Context, input any) ([]SourceDocument, error)
	BuildRetrieveFilter(ctx context.Context, req any) (map[string]any, error)
}
