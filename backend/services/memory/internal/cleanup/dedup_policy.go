package cleanup

import (
	"sort"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

const dedupRecentTieWindow = 24 * time.Hour

// DedupDecision 描述单个重复组的治理结论。
type DedupDecision struct {
	Keep    model.MemoryItem
	Archive []model.MemoryItem
}

// DecideDedupGroup 决定一组重复 active 记忆中“保留谁、归档谁”。
//
// 步骤化说明：
// 1. 先按“最近更新时间”判断谁更值得保留，符合治理计划里的“优先保留最近更新”；
// 2. 若更新时间非常接近，再比较 confidence/importance，避免刚好相差几秒就误保留低质量版本；
// 3. 最后用主键逆序兜底，保证同组治理结果稳定可复现。
func DecideDedupGroup(items []model.MemoryItem) DedupDecision {
	if len(items) == 0 {
		return DedupDecision{}
	}

	ordered := make([]model.MemoryItem, len(items))
	copy(ordered, items)
	sort.SliceStable(ordered, func(i, j int) bool {
		return preferDedupKeep(ordered[i], ordered[j])
	})

	return DedupDecision{
		Keep:    ordered[0],
		Archive: ordered[1:],
	}
}

func preferDedupKeep(left model.MemoryItem, right model.MemoryItem) bool {
	leftTime := dedupBaseTime(left)
	rightTime := dedupBaseTime(right)

	diff := leftTime.Sub(rightTime)
	if diff < 0 {
		diff = -diff
	}
	if diff > dedupRecentTieWindow {
		return leftTime.After(rightTime)
	}

	if left.Confidence != right.Confidence {
		return left.Confidence > right.Confidence
	}
	if left.Importance != right.Importance {
		return left.Importance > right.Importance
	}
	if !leftTime.Equal(rightTime) {
		return leftTime.After(rightTime)
	}
	return left.ID > right.ID
}

func dedupBaseTime(item model.MemoryItem) time.Time {
	if item.UpdatedAt != nil {
		return *item.UpdatedAt
	}
	if item.CreatedAt != nil {
		return *item.CreatedAt
	}
	return time.Time{}
}
