package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

const defaultDim = 16

// MockEmbedder 是本地可运行的占位向量化实现。
//
// 说明：
// 1. 该实现用于开发阶段打通链路，不代表真实语义向量质量；
// 2. 后续可替换为 Eino embedding 实现，接口保持不变。
type MockEmbedder struct {
	dim int
}

func NewMockEmbedder(dim int) *MockEmbedder {
	if dim <= 0 {
		dim = defaultDim
	}
	return &MockEmbedder{dim: dim}
}

func (e *MockEmbedder) Embed(_ context.Context, texts []string, _ string) ([][]float32, error) {
	result := make([][]float32, 0, len(texts))
	for _, text := range texts {
		result = append(result, e.embedOne(text))
	}
	return result, nil
}

func (e *MockEmbedder) embedOne(text string) []float32 {
	normalized := strings.TrimSpace(strings.ToLower(text))
	sum := sha256.Sum256([]byte(normalized))
	vec := make([]float32, e.dim)
	for i := 0; i < e.dim; i++ {
		offset := (i * 4) % len(sum)
		v := binary.BigEndian.Uint32(sum[offset : offset+4])
		vec[i] = float32(v%1000) / 1000
	}
	return vec
}
