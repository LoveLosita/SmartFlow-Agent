package store

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/infra/rag/core"
)

// MilvusStore 是 Milvus 连接器占位实现。
//
// 说明：
// 1. 本轮先保留接口结构，便于后续平滑替换 InMemoryStore；
// 2. 真实接入时需补充连接池、集合初始化、元数据过滤与错误转换。
type MilvusStore struct{}

func NewMilvusStore() *MilvusStore {
	return &MilvusStore{}
}

func (s *MilvusStore) Upsert(_ context.Context, _ []core.VectorRow) error {
	return errors.New("milvus store is not implemented yet")
}

func (s *MilvusStore) Search(_ context.Context, _ core.VectorSearchRequest) ([]core.ScoredVectorRow, error) {
	return nil, errors.New("milvus store is not implemented yet")
}

func (s *MilvusStore) Delete(_ context.Context, _ []string) error {
	return errors.New("milvus store is not implemented yet")
}

func (s *MilvusStore) Get(_ context.Context, _ []string) ([]core.VectorRow, error) {
	return nil, errors.New("milvus store is not implemented yet")
}
