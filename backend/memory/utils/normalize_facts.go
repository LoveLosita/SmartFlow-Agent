package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
)

const (
	maxTitleLength   = 64
	maxContentLength = 1000
)

// NormalizeFacts 对候选事实做标准化与过滤。
//
// 步骤：
// 1. 标准化 memory_type 与文本字段，丢弃空值和非法类型；
// 2. 对超长内容截断，避免脏数据污染后续链路；
// 3. 基于“类型+标准化内容”做去重，避免同一轮重复写入。
func NormalizeFacts(candidates []memorymodel.FactCandidate) []memorymodel.NormalizedFact {
	if len(candidates) == 0 {
		return nil
	}

	result := make([]memorymodel.NormalizedFact, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		memoryType := memorymodel.NormalizeMemoryType(candidate.MemoryType)
		if memoryType == "" {
			continue
		}

		content := normalizeWhitespace(candidate.Content)
		if content == "" {
			continue
		}
		content = truncateByRune(content, maxContentLength)

		title := normalizeWhitespace(candidate.Title)
		if title == "" {
			title = truncateByRune(content, maxTitleLength)
		}
		title = truncateByRune(title, maxTitleLength)

		confidence := clamp01(candidate.Confidence)
		if confidence == 0 {
			confidence = 0.6
		}
		importance := clamp01(candidate.Importance)
		if importance == 0 {
			importance = defaultImportanceByType(memoryType)
		}
		sensitivityLevel := clampInt(candidate.SensitivityLevel, 0, 2)

		normalizedContent := strings.ToLower(content)
		contentHash := HashContent(memoryType, normalizedContent)
		dedupKey := fmt.Sprintf("%s:%s", memoryType, contentHash)
		if _, exists := seen[dedupKey]; exists {
			continue
		}
		seen[dedupKey] = struct{}{}

		result = append(result, memorymodel.NormalizedFact{
			MemoryType:        memoryType,
			Title:             title,
			Content:           content,
			NormalizedContent: normalizedContent,
			ContentHash:       contentHash,
			Confidence:        confidence,
			Importance:        importance,
			SensitivityLevel:  sensitivityLevel,
			IsExplicit:        candidate.IsExplicit,
		})
	}
	return result
}

func normalizeWhitespace(raw string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
}

func truncateByRune(raw string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(raw)
	if len(runes) <= max {
		return raw
	}
	return string(runes[:max])
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampInt(v, minValue, maxValue int) int {
	if v < minValue {
		return minValue
	}
	if v > maxValue {
		return maxValue
	}
	return v
}

func defaultImportanceByType(memoryType string) float64 {
	switch memoryType {
	case memorymodel.MemoryTypePreference:
		return 0.85
	case memorymodel.MemoryTypeConstraint:
		return 0.95
	default:
		return 0.6
	}
}

// HashContent 计算记忆内容的去重哈希。
// 算法：sha256(memoryType + "::" + normalizedContent)
// 说明：导出此函数是为了让决策层 apply_actions 也能复用同一算法，避免哈希不一致导致去重失效。
func HashContent(memoryType, normalizedContent string) string {
	sum := sha256.Sum256([]byte(memoryType + "::" + normalizedContent))
	return hex.EncodeToString(sum[:])
}
