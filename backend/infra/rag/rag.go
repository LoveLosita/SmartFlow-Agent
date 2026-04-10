package rag

import (
	"github.com/LoveLosita/smartflow/backend/infra/rag/chunk"
	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
	"github.com/LoveLosita/smartflow/backend/infra/rag/embed"
	"github.com/LoveLosita/smartflow/backend/infra/rag/rerank"
	"github.com/LoveLosita/smartflow/backend/infra/rag/store"
)

// NewDefaultPipeline 构造默认可运行的 RAG Pipeline。
//
// 当前策略：
// 1. 默认使用本地 MockEmbedder + InMemoryStore，保证零外部依赖可运行；
// 2. 后续切 Milvus / Eino 时仅替换依赖，不改业务调用方式。
func NewDefaultPipeline() *core.Pipeline {
	return core.NewPipeline(
		chunk.NewTextChunker(),
		embed.NewMockEmbedder(16),
		store.NewInMemoryVectorStore(),
		rerank.NewNoopReranker(),
	)
}
