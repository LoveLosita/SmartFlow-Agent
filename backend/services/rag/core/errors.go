package core

import "errors"

var (
	// ErrInvalidQuery 表示检索请求缺少有效 query。
	ErrInvalidQuery = errors.New("invalid query")
	// ErrInvalidTopK 表示 topK 非法。
	ErrInvalidTopK = errors.New("invalid top_k")
	// ErrNilDependency 表示 pipeline 关键依赖未注入。
	ErrNilDependency = errors.New("nil dependency")
)

const (
	// FallbackReasonRerankFailed 表示 rerank 失败后降级。
	FallbackReasonRerankFailed = "RERANK_FAILED"
)
