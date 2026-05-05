package utils

import (
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

// EffectiveUserSetting 返回用户记忆设置的生效值。
//
// 规则说明：
// 1. 用户未显式配置时，走系统默认值；
// 2. 默认允许普通记忆和隐式记忆，但默认关闭敏感记忆；
// 3. 返回值始终是完整对象，方便调用方直接使用，不再分支判空。
func EffectiveUserSetting(setting *model.MemoryUserSetting, userID int) model.MemoryUserSetting {
	if setting == nil {
		return model.MemoryUserSetting{
			UserID:                 userID,
			MemoryEnabled:          true,
			ImplicitMemoryEnabled:  true,
			SensitiveMemoryEnabled: false,
		}
	}
	return *setting
}

// FilterFactsBySetting 按用户记忆开关过滤候选事实。
func FilterFactsBySetting(facts []memorymodel.NormalizedFact, setting model.MemoryUserSetting) []memorymodel.NormalizedFact {
	if !setting.MemoryEnabled || len(facts) == 0 {
		return nil
	}

	result := make([]memorymodel.NormalizedFact, 0, len(facts))
	for _, fact := range facts {
		if !setting.ImplicitMemoryEnabled && !fact.IsExplicit {
			continue
		}
		if !setting.SensitiveMemoryEnabled && fact.SensitivityLevel > 0 {
			continue
		}
		result = append(result, fact)
	}
	return result
}

// FilterItemsBySetting 按用户记忆开关过滤已入库记忆。
func FilterItemsBySetting(items []model.MemoryItem, setting model.MemoryUserSetting) []model.MemoryItem {
	if !setting.MemoryEnabled || len(items) == 0 {
		return nil
	}

	result := make([]model.MemoryItem, 0, len(items))
	for _, item := range items {
		if !setting.ImplicitMemoryEnabled && !item.IsExplicit {
			continue
		}
		if !setting.SensitiveMemoryEnabled && item.SensitivityLevel > 0 {
			continue
		}
		result = append(result, item)
	}
	return result
}

// FilterFactsByConfidence 按置信度阈值过滤候选事实。
//
// 说明：
// 1. minConfidence <= 0 时不做过滤，保持向后兼容；
// 2. 过滤在 FilterFactsBySetting 之后执行，是写入链路的第二道程序化门槛；
// 3. 阈值由 memory.write.minConfidence 配置控制，默认 0.5。
func FilterFactsByConfidence(facts []memorymodel.NormalizedFact, minConfidence float64) []memorymodel.NormalizedFact {
	if minConfidence <= 0 || len(facts) == 0 {
		return facts
	}
	result := make([]memorymodel.NormalizedFact, 0, len(facts))
	for _, fact := range facts {
		if fact.Confidence >= minConfidence {
			result = append(result, fact)
		}
	}
	return result
}
