package rag

import (
	"context"

	ragconfig "github.com/LoveLosita/smartflow/backend/services/rag/config"
)

// Options 描述 rag-service 需要持有的底层运行时。
type Options struct {
	Runtime Runtime
}

// Service 是 rag-service 对外暴露的统一入口。
//
// 职责边界：
// 1. 负责持有运行时，并把 memory / web 两条能力线统一收口到服务层。
// 2. 负责在服务入口内完成基于配置的运行时装配。
// 3. 不直接承载 chunk / embed / store 的实现细节，这些细节下沉到服务树内部子包。
type Service struct {
	runtime Runtime
}

// New 使用调用方传入的运行时构造服务。
func New(opts Options) *Service {
	return &Service{runtime: opts.Runtime}
}

// NewFromConfig 基于服务树内的配置与工厂能力构造自给自足的 RAG 服务。
func NewFromConfig(ctx context.Context, cfg ragconfig.Config, deps FactoryDeps) (*Service, error) {
	if !cfg.Enabled {
		return New(Options{}), nil
	}
	runtime, err := NewRuntimeFromConfig(ctx, cfg, deps)
	if err != nil {
		return nil, err
	}
	return NewWithRuntime(runtime), nil
}

// Runtime 返回当前服务持有的运行时。
func (s *Service) Runtime() Runtime {
	if s == nil {
		return nil
	}
	return s.runtime
}

// IngestMemory 写入记忆语料。
func (s *Service) IngestMemory(ctx context.Context, req MemoryIngestRequest) (*IngestResult, error) {
	if s == nil || s.runtime == nil {
		return nil, nil
	}
	return s.runtime.IngestMemory(ctx, req)
}

// RetrieveMemory 检索记忆语料。
func (s *Service) RetrieveMemory(ctx context.Context, req MemoryRetrieveRequest) (*RetrieveResult, error) {
	if s == nil || s.runtime == nil {
		return nil, nil
	}
	return s.runtime.RetrieveMemory(ctx, req)
}

// DeleteMemory 删除指定记忆文档。
func (s *Service) DeleteMemory(ctx context.Context, documentIDs []string) error {
	if s == nil || s.runtime == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return s.runtime.DeleteMemory(ctx, documentIDs)
}

// IngestWeb 写入网页语料。
func (s *Service) IngestWeb(ctx context.Context, req WebIngestRequest) (*IngestResult, error) {
	if s == nil || s.runtime == nil {
		return nil, nil
	}
	return s.runtime.IngestWeb(ctx, req)
}

// RetrieveWeb 检索网页语料。
func (s *Service) RetrieveWeb(ctx context.Context, req WebRetrieveRequest) (*RetrieveResult, error) {
	if s == nil || s.runtime == nil {
		return nil, nil
	}
	return s.runtime.RetrieveWeb(ctx, req)
}

// EnsureRuntime 返回一个可继续向下传递的运行时引用。
func (s *Service) EnsureRuntime() Runtime {
	if s == nil {
		return nil
	}
	return s.runtime
}

// SetRuntime 允许在装配阶段延迟注入运行时。
func (s *Service) SetRuntime(runtime Runtime) {
	if s == nil {
		return
	}
	s.runtime = runtime
}

// NewWithRuntime 用显式运行时构造服务。
func NewWithRuntime(runtime Runtime) *Service {
	return New(Options{Runtime: runtime})
}
