package worker

import (
	"context"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryorchestrator "github.com/LoveLosita/smartflow/backend/memory/orchestrator"
)

// Extractor 是 worker 抽取依赖接口。
//
// 设计目的：
// 1. Day1 先接 mock 编排器跑通状态机；
// 2. Day2/Day3 可无缝替换为真实 LLM 抽取实现。
type Extractor interface {
	ExtractFacts(ctx context.Context, payload memorymodel.ExtractJobPayload) ([]memorymodel.NormalizedFact, error)
}

// NewMockExtractor 返回 Day1 默认 mock 抽取器。
func NewMockExtractor() Extractor {
	return memoryorchestrator.NewWriteOrchestrator()
}
