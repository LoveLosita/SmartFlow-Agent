package rag

import (
	"context"
	"time"
)

// Runtime 是 RAG Infra 对业务侧暴露的唯一稳定方法面。
//
// 职责边界：
// 1. 负责承接 memory/web 两类语料的统一入库与检索入口；
// 2. 负责屏蔽底层 Pipeline / Store / Embedder / Reranker 的装配细节；
// 3. 不负责 provider 搜索、HTML 抓取、prompt 注入等业务语义。
type Runtime interface {
	IngestMemory(ctx context.Context, req MemoryIngestRequest) (*IngestResult, error)
	RetrieveMemory(ctx context.Context, req MemoryRetrieveRequest) (*RetrieveResult, error)

	IngestWeb(ctx context.Context, req WebIngestRequest) (*IngestResult, error)
	RetrieveWeb(ctx context.Context, req WebRetrieveRequest) (*RetrieveResult, error)
}

// IngestResult 描述一次统一入库执行摘要。
type IngestResult struct {
	DocumentCount int
	ChunkCount    int
	DocumentIDs   []string
}

// RetrieveHit 是对业务侧暴露的统一命中项。
type RetrieveHit struct {
	ChunkID    string
	DocumentID string
	Text       string
	Score      float64
	Metadata   map[string]any
}

// RetrieveResult 描述一次检索执行摘要。
type RetrieveResult struct {
	Items          []RetrieveHit
	RawCount       int
	FallbackUsed   bool
	FallbackReason string
}

// MemoryIngestItem 是 memory 语料入库项。
type MemoryIngestItem struct {
	MemoryID         int64
	UserID           int
	ConversationID   string
	AssistantID      string
	RunID            string
	MemoryType       string
	Title            string
	Content          string
	Confidence       float64
	Importance       float64
	SensitivityLevel int
	IsExplicit       bool
	Status           string
	TTLAt            *time.Time
	CreatedAt        *time.Time
}

// MemoryIngestRequest 描述一次记忆向量入库请求。
type MemoryIngestRequest struct {
	TraceID string
	Action  string
	Items   []MemoryIngestItem
}

// MemoryRetrieveRequest 描述一次记忆检索请求。
type MemoryRetrieveRequest struct {
	TraceID        string
	Query          string
	TopK           int
	Threshold      float64
	Action         string
	UserID         int
	ConversationID string
	AssistantID    string
	RunID          string
	MemoryTypes    []string
}

// WebIngestItem 是网页语料入库项。
type WebIngestItem struct {
	URL         string
	Title       string
	Content     string
	Snippet     string
	Domain      string
	QueryID     string
	SessionID   string
	PublishedAt *time.Time
	FetchedAt   *time.Time
	SourceRank  int
}

// WebIngestRequest 描述一次网页语料入库请求。
type WebIngestRequest struct {
	TraceID string
	Action  string
	Items   []WebIngestItem
}

// WebRetrieveRequest 描述一次网页检索请求。
type WebRetrieveRequest struct {
	TraceID   string
	Query     string
	TopK      int
	Threshold float64
	Action    string
	QueryID   string
	SessionID string
	Domain    string
}
