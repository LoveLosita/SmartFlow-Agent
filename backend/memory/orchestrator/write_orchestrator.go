package orchestrator

import (
	"context"
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
)

// WriteOrchestrator 是写入链路编排器（Day1 首版）。
//
// 职责边界：
// 1. Day1 只做 mock 抽取 + 标准化，不接 LLM 决策；
// 2. Day2/Day3 再引入冲突消解、重排与向量召回。
type WriteOrchestrator struct{}

func NewWriteOrchestrator() *WriteOrchestrator {
	return &WriteOrchestrator{}
}

// ExtractFacts 执行“候选事实抽取 -> 标准化”链路。
//
// Day1 策略：
// 1. 先用 source_text 直接构造候选事实，确保链路可跑通；
// 2. 后续再替换成 LLM 抽取与结构化决策。
func (o *WriteOrchestrator) ExtractFacts(_ context.Context, payload memorymodel.ExtractJobPayload) ([]memorymodel.NormalizedFact, error) {
	sourceText := strings.TrimSpace(payload.SourceText)
	if sourceText == "" {
		return nil, nil
	}

	candidates := []memorymodel.FactCandidate{
		{
			MemoryType: memorymodel.MemoryTypeFact,
			Title:      "用户近期提及",
			Content:    sourceText,
			Confidence: 0.6,
			IsExplicit: false,
		},
	}
	return memoryutils.NormalizeFacts(candidates), nil
}
