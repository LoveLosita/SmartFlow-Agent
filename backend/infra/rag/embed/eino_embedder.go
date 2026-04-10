package embed

import (
	"context"
	"errors"
)

// EinoEmbedder 是 Eino embedding 的占位实现。
//
// 说明：
// 1. 本轮先占位接口，避免过早耦合具体 Provider；
// 2. 后续接入真实 embedding 时，只替换此文件内部实现。
type EinoEmbedder struct{}

func NewEinoEmbedder() *EinoEmbedder {
	return &EinoEmbedder{}
}

func (e *EinoEmbedder) Embed(_ context.Context, _ []string, _ string) ([][]float32, error) {
	return nil, errors.New("eino embedder is not implemented yet")
}
