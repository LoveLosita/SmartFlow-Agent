package service

import (
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	"github.com/LoveLosita/smartflow/backend/model"
)

func toItemDTO(item model.MemoryItem) memorymodel.ItemDTO {
	return memorymodel.ItemDTO{
		ID:               item.ID,
		UserID:           item.UserID,
		ConversationID:   strValue(item.ConversationID),
		AssistantID:      strValue(item.AssistantID),
		RunID:            strValue(item.RunID),
		MemoryType:       item.MemoryType,
		Title:            item.Title,
		Content:          item.Content,
		Confidence:       item.Confidence,
		Importance:       item.Importance,
		SensitivityLevel: item.SensitivityLevel,
		IsExplicit:       item.IsExplicit,
		Status:           item.Status,
		TTLAt:            item.TTLAt,
		CreatedAt:        item.CreatedAt,
		UpdatedAt:        item.UpdatedAt,
	}
}

func toItemDTOs(items []model.MemoryItem) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}
	result := make([]memorymodel.ItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toItemDTO(item))
	}
	return result
}

func toUserSettingDTO(setting model.MemoryUserSetting) memorymodel.UserSettingDTO {
	return memorymodel.UserSettingDTO{
		UserID:                 setting.UserID,
		MemoryEnabled:          setting.MemoryEnabled,
		ImplicitMemoryEnabled:  setting.ImplicitMemoryEnabled,
		SensitiveMemoryEnabled: setting.SensitiveMemoryEnabled,
		UpdatedAt:              setting.UpdatedAt,
	}
}

func normalizeMemoryTypes(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	result := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		normalized := memorymodel.NormalizeMemoryType(item)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func normalizeManageStatuses(raw []string) []string {
	if len(raw) == 0 {
		return []string{
			model.MemoryItemStatusActive,
			model.MemoryItemStatusArchived,
		}
	}

	result := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		status := strings.ToLower(strings.TrimSpace(item))
		if status != model.MemoryItemStatusActive &&
			status != model.MemoryItemStatusArchived &&
			status != model.MemoryItemStatusDeleted {
			continue
		}
		if _, exists := seen[status]; exists {
			continue
		}
		seen[status] = struct{}{}
		result = append(result, status)
	}
	if len(result) == 0 {
		return []string{
			model.MemoryItemStatusActive,
			model.MemoryItemStatusArchived,
		}
	}
	return result
}

func normalizeLimit(limit, defaultValue, maxValue int) int {
	if limit <= 0 {
		limit = defaultValue
	}
	if maxValue > 0 && limit > maxValue {
		return maxValue
	}
	return limit
}

func strValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
