package service

import (
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	memoryutils "github.com/LoveLosita/smartflow/backend/services/memory/internal/utils"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
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
		ContentHash:      fallbackContentHash(item.MemoryType, item.Content, strValue(item.ContentHash)),
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

// fallbackContentHash 返回条目可用于服务级去重的内容哈希。
//
// 说明：
// 1. 优先复用库内已落表的 content_hash，避免同一条数据多套算法口径不一致；
// 2. 若历史数据或 RAG metadata 没带 hash，则按“类型 + 规范化内容”补算；
// 3. 若类型非法或正文为空，则返回空字符串，让上游继续走文本兜底去重。
func fallbackContentHash(memoryType, content, currentHash string) string {
	currentHash = strings.TrimSpace(currentHash)
	if currentHash != "" {
		return currentHash
	}

	normalizedType := memorymodel.NormalizeMemoryType(memoryType)
	normalizedContent := normalizeContentForHash(content)
	if normalizedType == "" || normalizedContent == "" {
		return ""
	}
	return memoryutils.HashContent(normalizedType, normalizedContent)
}

func normalizeContentForHash(content string) string {
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	return strings.ToLower(strings.Join(strings.Fields(content), " "))
}
