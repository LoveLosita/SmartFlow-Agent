package retrieve

import (
	"context"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

// VectorRetriever 是通用检索器（embed + vector search）。
type VectorRetriever struct {
	embedder core.Embedder
	store    core.VectorStore
}

func NewVectorRetriever(embedder core.Embedder, store core.VectorStore) *VectorRetriever {
	return &VectorRetriever{
		embedder: embedder,
		store:    store,
	}
}

func (r *VectorRetriever) Retrieve(ctx context.Context, req core.RetrieveRequest) ([]core.ScoredChunk, error) {
	if r == nil || r.embedder == nil || r.store == nil {
		return nil, core.ErrNilDependency
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return nil, core.ErrInvalidQuery
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 8
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "search"
	}

	vectors, err := r.embedder.Embed(ctx, []string{query}, action)
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("embedding query length mismatch: %d", len(vectors))
	}

	rows, err := r.store.Search(ctx, core.VectorSearchRequest{
		QueryVector: vectors[0],
		TopK:        topK,
		Filter:      req.Filter,
	})
	if err != nil {
		return nil, err
	}

	result := make([]core.ScoredChunk, 0, len(rows))
	for _, row := range rows {
		if row.Score < req.Threshold {
			continue
		}
		result = append(result, core.ScoredChunk{
			ChunkID:    row.Row.ID,
			DocumentID: asString(row.Row.Metadata["document_id"]),
			Text:       row.Row.Text,
			Score:      row.Score,
			Metadata:   cloneMap(row.Row.Metadata),
		})
	}
	return result, nil
}

func cloneMap(src map[string]any) map[string]any {
	if len(src) == 0 {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func asString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
