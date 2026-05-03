package store

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/rag/core"
)

// InMemoryVectorStore 是本地开发用向量存储实现。
//
// 注意：
// 1. 仅用于开发调试，不建议生产使用；
// 2. 真实环境可替换为 MilvusStore，接口保持一致。
type InMemoryVectorStore struct {
	mu   sync.RWMutex
	rows map[string]core.VectorRow
}

func NewInMemoryVectorStore() *InMemoryVectorStore {
	return &InMemoryVectorStore{
		rows: make(map[string]core.VectorRow),
	}
}

func (s *InMemoryVectorStore) Upsert(_ context.Context, rows []core.VectorRow) error {
	if s == nil {
		return errors.New("inmemory vector store is nil")
	}
	if len(rows) == 0 {
		return nil
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.rows == nil {
		s.rows = make(map[string]core.VectorRow)
	}
	for _, row := range rows {
		current, exists := s.rows[row.ID]
		if exists {
			row.CreatedAt = current.CreatedAt
			row.UpdatedAt = now
		} else {
			if row.CreatedAt.IsZero() {
				row.CreatedAt = now
			}
			row.UpdatedAt = now
		}
		s.rows[row.ID] = row
	}
	return nil
}

func (s *InMemoryVectorStore) Search(_ context.Context, req core.VectorSearchRequest) ([]core.ScoredVectorRow, error) {
	if s == nil {
		return nil, errors.New("inmemory vector store is nil")
	}
	topK := req.TopK
	if topK <= 0 {
		topK = 8
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]core.ScoredVectorRow, 0, len(s.rows))
	for _, row := range s.rows {
		if !matchMetadataFilter(row.Metadata, req.Filter) {
			continue
		}
		score := cosineSimilarity(req.QueryVector, row.Vector)
		result = append(result, core.ScoredVectorRow{
			Row:   row,
			Score: score,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Score > result[j].Score
	})
	if len(result) <= topK {
		return result, nil
	}
	return result[:topK], nil
}

func (s *InMemoryVectorStore) Delete(_ context.Context, ids []string) error {
	if s == nil {
		return errors.New("inmemory vector store is nil")
	}
	if len(ids) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range ids {
		delete(s.rows, id)
	}
	return nil
}

func (s *InMemoryVectorStore) Get(_ context.Context, ids []string) ([]core.VectorRow, error) {
	if s == nil {
		return nil, errors.New("inmemory vector store is nil")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]core.VectorRow, 0, len(ids))
	for _, id := range ids {
		row, exists := s.rows[id]
		if !exists {
			continue
		}
		result = append(result, row)
	}
	return result, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := 0; i < n; i++ {
		av := float64(a[i])
		bv := float64(b[i])
		dot += av * bv
		normA += av * av
		normB += bv * bv
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func matchMetadataFilter(metadata map[string]any, filter map[string]any) bool {
	if len(filter) == 0 {
		return true
	}
	for key, wanted := range filter {
		got, exists := metadata[key]
		if !exists {
			return false
		}
		if !equalAny(got, wanted) {
			return false
		}
	}
	return true
}

func equalAny(left any, right any) bool {
	return toString(left) == toString(right)
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	return fmtAny(v)
}

func fmtAny(v any) string {
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}
