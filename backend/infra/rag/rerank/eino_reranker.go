package rerank

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

// EinoReranker 是 Eino 重排器占位实现。
type EinoReranker struct{}

func NewEinoReranker() *EinoReranker {
	return &EinoReranker{}
}

func (r *EinoReranker) Rerank(_ context.Context, _ string, _ []core.ScoredChunk, _ int) ([]core.ScoredChunk, error) {
	return nil, errors.New("eino reranker is not implemented yet")
}
