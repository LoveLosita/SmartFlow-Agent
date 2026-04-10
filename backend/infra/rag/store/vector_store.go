package store

import "github.com/LoveLosita/smartflow/backend/infra/rag/core"

// EnsureCompile 用于静态校验实现是否满足接口。
func EnsureCompile() {
	var _ core.VectorStore = (*InMemoryVectorStore)(nil)
	var _ core.VectorStore = (*MilvusStore)(nil)
}
