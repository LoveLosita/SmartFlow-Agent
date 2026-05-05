package service

import (
	"sort"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
)

// RankItems 对读取结果做统一重排。
//
// 步骤化说明：
// 1. 先基于 importance / confidence / recency 构造基础分，保持和旧链路相近的排序直觉；
// 2. 再叠加“显式记忆 / 类型优先级”奖励，让 constraint 与 preference 更稳定地排在前面；
// 3. 同分按 ID 降序，保证排序在日志与测试里具备稳定性。
func RankItems(items []memorymodel.ItemDTO, now time.Time) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}

	ranked := make([]memorymodel.ItemDTO, len(items))
	copy(ranked, items)
	sort.SliceStable(ranked, func(i, j int) bool {
		left := scoreRankedItem(ranked[i], now)
		right := scoreRankedItem(ranked[j], now)
		if left == right {
			return ranked[i].ID > ranked[j].ID
		}
		return left > right
	})
	return ranked
}

// scoreRankedItem 计算 hybrid 读链路的统一重排分数。
//
// 说明：
// 1. 这里仍然只依赖条目自身属性，不引入 conversation_id 加分；
// 2. 原因是同对话内容本就已经存在于上下文窗口，记忆读侧应专注跨对话补充；
// 3. 类型加权仍然保留，用于确保 constraint / preference 的业务优先级稳定生效。
func scoreRankedItem(item memorymodel.ItemDTO, now time.Time) float64 {
	score := 0.35*clamp01(item.Importance) + 0.3*clamp01(item.Confidence) + 0.2*recencyScoreDTO(item, now)
	if item.IsExplicit {
		score += 0.1
	}
	switch memorymodel.NormalizeMemoryType(item.MemoryType) {
	case memorymodel.MemoryTypeConstraint:
		score += 0.15
	case memorymodel.MemoryTypePreference:
		score += 0.10
	}
	return score
}

func recencyScoreDTO(item memorymodel.ItemDTO, now time.Time) float64 {
	base := item.UpdatedAt
	if base == nil {
		base = item.CreatedAt
	}
	if base == nil || now.Before(*base) {
		return 0.5
	}

	age := now.Sub(*base)
	switch {
	case age <= 24*time.Hour:
		return 1
	case age <= 7*24*time.Hour:
		return 0.85
	case age <= 30*24*time.Hour:
		return 0.65
	case age <= 90*24*time.Hour:
		return 0.45
	default:
		return 0.25
	}
}
