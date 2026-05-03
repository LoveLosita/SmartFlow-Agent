package chunk

import (
	"context"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/rag/core"
)

// TextChunker 是默认文本切块器。
type TextChunker struct{}

func NewTextChunker() *TextChunker {
	return &TextChunker{}
}

// Chunk 对文本执行固定窗口切块。
//
// 步骤化说明：
// 1. 先做空白归一，避免无效块进入向量库；
// 2. 再按 chunk_size/overlap 滑窗切割；
// 3. 每块继承原文 metadata，并补充 chunk 序号。
func (c *TextChunker) Chunk(_ context.Context, doc core.SourceDocument, opt core.ChunkOption) ([]core.Chunk, error) {
	if strings.TrimSpace(doc.ID) == "" {
		return nil, fmt.Errorf("empty document id")
	}
	text := strings.TrimSpace(doc.Text)
	if text == "" {
		return nil, nil
	}
	if opt.ChunkSize <= 0 {
		opt.ChunkSize = 400
	}
	if opt.ChunkOverlap < 0 {
		opt.ChunkOverlap = 0
	}
	if opt.ChunkOverlap >= opt.ChunkSize {
		opt.ChunkOverlap = opt.ChunkSize / 5
	}

	runes := []rune(text)
	step := opt.ChunkSize - opt.ChunkOverlap
	if step <= 0 {
		step = opt.ChunkSize
	}

	result := make([]core.Chunk, 0, len(runes)/step+1)
	order := 0
	for start := 0; start < len(runes); start += step {
		end := start + opt.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunkText := strings.TrimSpace(string(runes[start:end]))
		if chunkText == "" {
			continue
		}
		metadata := cloneMap(doc.Metadata)
		metadata["chunk_order"] = order
		result = append(result, core.Chunk{
			ID:         fmt.Sprintf("%s#%d", doc.ID, order),
			DocumentID: doc.ID,
			Text:       chunkText,
			Order:      order,
			Metadata:   metadata,
		})
		order++
		if end == len(runes) {
			break
		}
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
