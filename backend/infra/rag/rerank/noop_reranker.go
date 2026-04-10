package rerank

import (
	"context"
	"sort"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

// NoopReranker 是默认重排器（仅按原 score 排序）。
type NoopReranker struct{}

func NewNoopReranker() *NoopReranker {
	return &NoopReranker{}
}

func (r *NoopReranker) Rerank(_ context.Context, _ string, candidates []core.ScoredChunk, topK int) ([]core.ScoredChunk, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	sorted := make([]core.ScoredChunk, len(candidates))
	copy(sorted, candidates)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Score > sorted[j].Score
	})
	if topK <= 0 || topK >= len(sorted) {
		return sorted, nil
	}
	return sorted[:topK], nil
}
