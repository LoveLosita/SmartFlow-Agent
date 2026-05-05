package utils

import (
	"fmt"

	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
)

// AggregateComparisons 把一轮 LLM 比对结果汇总为最终动作。
//
// 职责边界：
// 1. 纯确定性逻辑，不调 LLM，不调外部服务；
// 2. 按优先级从高到低判定：duplicate > update > conflict > unrelated；
// 3. 多条 update 时选 Score 最高的候选执行 UPDATE。
//
// 汇总规则：
// 1. 出现 duplicate → 最终动作 NONE（新 fact 完全重复，不需要写入）；
// 2. 出现 update → 最终动作 UPDATE（更新 Score 最高的那条旧记忆）；
// 3. 出现 conflict → 最终动作 DELETE + 后续按 ADD 处理（旧记忆过时，先删旧的再写新的）；
// 4. 全部 unrelated → 最终动作 ADD（没有相关旧记忆，直接新增）。
func AggregateComparisons(
	fact memorymodel.NormalizedFact,
	comparisons []memorymodel.ComparisonResult,
	candidates []memorymodel.CandidateSnapshot,
) *memorymodel.FinalDecision {
	// 1. 无候选时直接 ADD，无需走任何判断。
	if len(comparisons) == 0 {
		return &memorymodel.FinalDecision{
			Action: memorymodel.DecisionActionAdd,
			Reason: "无相关旧记忆，直接新增",
		}
	}

	// 2. 建立 memoryID → CandidateSnapshot 映射，用于查找 Score。
	snapshotMap := make(map[int64]memorymodel.CandidateSnapshot, len(candidates))
	for _, c := range candidates {
		snapshotMap[c.MemoryID] = c
	}

	hasDuplicate := false
	var bestUpdate *memorymodel.ComparisonResult
	bestUpdateScore := -1.0
	var conflictResult *memorymodel.ComparisonResult

	for i := range comparisons {
		comp := &comparisons[i]

		switch comp.Relation {
		case memorymodel.RelationDuplicate:
			// 3. 出现一条 duplicate 即可确定最终动作为 NONE。
			hasDuplicate = true

		case memorymodel.RelationUpdate:
			// 4. 多条 update 时，选 Score 最高的那条执行 UPDATE。
			snapshot, ok := snapshotMap[comp.MemoryID]
			score := 0.0
			if ok {
				score = snapshot.Score
			}
			if score > bestUpdateScore {
				bestUpdateScore = score
				bestUpdate = comp
			}

		case memorymodel.RelationConflict:
			// 5. 记录第一条 conflict，用于后续 DELETE + ADD 处理。
			if conflictResult == nil {
				conflictResult = comp
			}
		}
	}

	// 6. 按优先级判定最终动作。
	if hasDuplicate {
		return &memorymodel.FinalDecision{
			Action: memorymodel.DecisionActionNone,
			Reason: "存在完全重复的旧记忆，跳过写入",
		}
	}

	if bestUpdate != nil {
		// 7. UPDATE 动作：使用 LLM 提供的合并后内容。
		title := bestUpdate.UpdatedTitle
		if title == "" {
			title = fact.Title
		}
		content := bestUpdate.UpdatedContent
		reason := bestUpdate.Reason
		if reason == "" {
			reason = "新事实是对旧记忆的修正或补充"
		}
		return &memorymodel.FinalDecision{
			Action:   memorymodel.DecisionActionUpdate,
			TargetID: bestUpdate.MemoryID,
			Title:    title,
			Content:  content,
			Reason:   fmt.Sprintf("更新旧记忆(id=%d): %s", bestUpdate.MemoryID, reason),
		}
	}

	if conflictResult != nil {
		// 8. conflict → 先 DELETE 旧记忆，后续由上层按 ADD 写入新 fact。
		return &memorymodel.FinalDecision{
			Action:   memorymodel.DecisionActionDelete,
			TargetID: conflictResult.MemoryID,
			Reason:   fmt.Sprintf("旧记忆(id=%d)与新事实冲突，删除后新增: %s", conflictResult.MemoryID, conflictResult.Reason),
		}
	}

	// 9. 全部 unrelated → 直接 ADD。
	return &memorymodel.FinalDecision{
		Action: memorymodel.DecisionActionAdd,
		Reason: "无相关旧记忆，直接新增",
	}
}
