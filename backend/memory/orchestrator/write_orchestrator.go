package orchestrator

import (
	"context"
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
)

// WriteOrchestrator 是 Day1 的本地回退版本。
//
// 职责边界：
// 1. 只做最保守的“从 source_text 直接生成一条候选事实”；
// 2. 不依赖 LLM，便于在模型不可用时保底；
// 3. 后续会逐步被 LLM 版编排器取代，但不会直接删掉，方便回退。
type WriteOrchestrator struct{}

func NewWriteOrchestrator() *WriteOrchestrator {
	return &WriteOrchestrator{}
}

// ExtractFacts 执行最小回退链路。
func (o *WriteOrchestrator) ExtractFacts(_ context.Context, payload memorymodel.ExtractJobPayload) ([]memorymodel.NormalizedFact, error) {
	sourceText := strings.TrimSpace(payload.SourceText)
	if sourceText == "" {
		return nil, nil
	}

	candidates := []memorymodel.FactCandidate{{
		MemoryType:       memorymodel.MemoryTypeFact,
		Title:            "用户提到",
		Content:          sourceText,
		Confidence:       0.6,
		Importance:       0.6,
		SensitivityLevel: 0,
		IsExplicit:       false,
	}}
	return memoryutils.NormalizeFacts(candidates), nil
}
