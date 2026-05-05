package service

import (
	"context"
	"strings"
	"time"

	memoryutils "github.com/LoveLosita/smartflow/backend/services/memory/internal/utils"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

// HybridRetrieve 统一承接读取侧 RAG-first 召回链路。
//
// 步骤化说明：
// 1. 优先走 RAG 语义搜索，按 query 相关性召回候选记忆；
// 2. RAG 报错或 0 命中时回退 MySQL，保证链路韧性；
// 3. 召回结果做三级去重、排序与类型预算裁剪（总量不超过调用方 limit）；
// 4. 旧 legacy 链路完全保留，方便通过配置快速回滚。
func (s *ReadService) HybridRetrieve(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	limit int,
	now time.Time,
) ([]memorymodel.ItemDTO, retrieveTelemetry, error) {
	telemetry := retrieveTelemetry{}
	if s == nil || s.itemRepo == nil {
		return nil, telemetry, nil
	}
	if !effectiveSetting.MemoryEnabled {
		return nil, telemetry, nil
	}

	// RAG-first：只走语义召回，不再全量拉 MySQL pinned。
	items, semanticTelemetry, err := s.retrieveSemanticCandidates(ctx, req, effectiveSetting, limit, now)
	if err != nil {
		return nil, telemetry, err
	}
	telemetry.SemanticHitCount = semanticTelemetry.HitCount
	telemetry.Degraded = semanticTelemetry.Degraded
	telemetry.RAGFallbackUsed = semanticTelemetry.RAGFallbackUsed

	if len(items) == 0 {
		return nil, telemetry, nil
	}

	beforeDedupCount := len(items)
	items = dedupByID(items)
	items = dedupByHash(items)
	items = dedupByText(items)
	telemetry.DedupDropCount = beforeDedupCount - len(items)
	items = RankItems(items, now)
	items = applyTypeBudget(items, s.cfg, limit)
	if len(items) == 0 {
		return nil, telemetry, nil
	}
	telemetry.FinalCount = len(items)

	_ = s.itemRepo.TouchLastAccessAt(ctx, collectItemDTOIDs(items), now)
	return items, telemetry, nil
}

func (s *ReadService) retrievePinnedCandidates(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	now time.Time,
) ([]memorymodel.ItemDTO, error) {
	query := buildReadScopedItemQuery(req, now, nil, 0)
	items, err := s.itemRepo.FindPinnedByUser(ctx, query, s.cfg.EffectiveReadPreferenceLimit())
	if err != nil {
		return nil, err
	}
	items = memoryutils.FilterItemsBySetting(items, effectiveSetting)
	return toItemDTOs(items), nil
}

func (s *ReadService) retrieveSemanticCandidates(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	limit int,
	now time.Time,
) ([]memorymodel.ItemDTO, semanticRetrieveTelemetry, error) {
	telemetry := semanticRetrieveTelemetry{}
	queryText := strings.TrimSpace(req.Query)
	if queryText == "" {
		return nil, telemetry, nil
	}

	candidateLimit := hybridSemanticTopK(s.cfg, limit)
	if s.cfg.RAGEnabled && s.ragRuntime != nil {
		items, err := s.retrieveSemanticCandidatesByRAG(ctx, req, effectiveSetting, candidateLimit, now)
		if shouldReturnSemanticRAGResult(items, err) {
			telemetry.HitCount = len(items)
			return items, telemetry, nil
		}
		telemetry.Degraded = true
		telemetry.RAGFallbackUsed = true
	}
	items, err := s.retrieveSemanticCandidatesByMySQL(ctx, req, effectiveSetting, candidateLimit, now)
	telemetry.HitCount = len(items)
	return items, telemetry, err
}

func (s *ReadService) retrieveSemanticCandidatesByRAG(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	candidateLimit int,
	now time.Time,
) ([]memorymodel.ItemDTO, error) {
	result, err := s.ragRuntime.RetrieveMemory(ctx, buildReadScopedRAGRequest(req, candidateLimit, s.cfg.Threshold))
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Items) == 0 {
		return nil, nil
	}

	items := make([]memorymodel.ItemDTO, 0, len(result.Items))
	for _, hit := range result.Items {
		dto, memoryID := buildMemoryDTOFromRetrieveHit(hit)
		if !effectiveSetting.ImplicitMemoryEnabled && !dto.IsExplicit {
			continue
		}
		if !effectiveSetting.SensitiveMemoryEnabled && dto.SensitivityLevel > 0 {
			continue
		}
		if dto.ID <= 0 && memoryID > 0 {
			dto.ID = memoryID
		}
		items = append(items, dto)
	}
	return items, nil
}

func (s *ReadService) retrieveSemanticCandidatesByMySQL(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	candidateLimit int,
	now time.Time,
) ([]memorymodel.ItemDTO, error) {
	query := buildReadScopedItemQuery(
		req,
		now,
		[]string{model.MemoryItemStatusActive},
		normalizeLimit(candidateLimit, candidateLimit, maxRetrieveLimit),
	)

	items, err := s.itemRepo.FindByQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	items = memoryutils.FilterItemsBySetting(items, effectiveSetting)
	return toItemDTOs(items), nil
}

// dedupByID 按 memory_id 去重，后出现的结果覆盖先出现的结果。
func dedupByID(items []memorymodel.ItemDTO) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}

	seen := make(map[int64]struct{}, len(items))
	result := make([]memorymodel.ItemDTO, 0, len(items))
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.ID <= 0 {
			result = append(result, item)
			continue
		}
		if _, exists := seen[item.ID]; exists {
			continue
		}
		seen[item.ID] = struct{}{}
		result = append(result, item)
	}
	reverseItemDTOs(result)
	return result
}

// dedupByHash 按 content_hash 去重；缺失 hash 时跳过，保留 importance 更高的条目。
func dedupByHash(items []memorymodel.ItemDTO) []memorymodel.ItemDTO {
	return dedupByKey(items, func(item memorymodel.ItemDTO) string {
		return fallbackContentHash(item.MemoryType, item.Content, item.ContentHash)
	})
}

// dedupByText 按“类型标签 + 文本”兜底去重，用于覆盖历史数据未带 hash 的场景。
func dedupByText(items []memorymodel.ItemDTO) []memorymodel.ItemDTO {
	return dedupByKey(items, func(item memorymodel.ItemDTO) string {
		text := strings.TrimSpace(item.Content)
		if text == "" {
			text = strings.TrimSpace(item.Title)
		}
		if text == "" {
			return ""
		}
		return renderMemoryTypeLabelForDedup(item.MemoryType) + "::" + normalizeContentForHash(text)
	})
}

func dedupByKey(items []memorymodel.ItemDTO, keyBuilder func(item memorymodel.ItemDTO) string) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}

	selectedIndex := make(map[string]int, len(items))
	for index, item := range items {
		key := strings.TrimSpace(keyBuilder(item))
		if key == "" {
			continue
		}
		if previous, exists := selectedIndex[key]; exists {
			if preferCurrentItem(items[previous], item) {
				selectedIndex[key] = index
			}
			continue
		}
		selectedIndex[key] = index
	}

	result := make([]memorymodel.ItemDTO, 0, len(items))
	for index, item := range items {
		key := strings.TrimSpace(keyBuilder(item))
		if key == "" {
			result = append(result, item)
			continue
		}
		if selectedIndex[key] == index {
			result = append(result, item)
		}
	}
	return result
}

func preferCurrentItem(previous memorymodel.ItemDTO, current memorymodel.ItemDTO) bool {
	if current.Importance != previous.Importance {
		return current.Importance > previous.Importance
	}
	if current.Confidence != previous.Confidence {
		return current.Confidence > previous.Confidence
	}
	return true
}

// applyTypeBudget 在排序结果上应用四类记忆预算，并以 callerLimit 作为总量硬上限。
//
// 说明：
// 1. 每种类型先保底自己的预算上限，避免 fact 抢掉 constraint 的位置；
// 2. 裁剪时保持当前排序顺序，不在这里重新打分；
// 3. 最终总量不超过 min(callerLimit, cfg.TotalReadBudget())。
func applyTypeBudget(items []memorymodel.ItemDTO, cfg memorymodel.Config, callerLimit int) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}

	hardCap := cfg.TotalReadBudget()
	if callerLimit > 0 && callerLimit < hardCap {
		hardCap = callerLimit
	}

	budgetByType := map[string]int{
		memorymodel.MemoryTypeConstraint: cfg.EffectiveReadConstraintLimit(),
		memorymodel.MemoryTypePreference: cfg.EffectiveReadPreferenceLimit(),
		memorymodel.MemoryTypeFact:       cfg.EffectiveReadFactLimit(),
	}
	usedByType := make(map[string]int, len(budgetByType))
	result := make([]memorymodel.ItemDTO, 0, minInt(len(items), hardCap))
	for _, item := range items {
		if len(result) >= hardCap {
			break
		}

		memoryType := resolveBudgetMemoryType(item.MemoryType)
		if usedByType[memoryType] >= budgetByType[memoryType] {
			continue
		}
		usedByType[memoryType]++
		result = append(result, item)
	}
	return result
}

// hybridSemanticTopK 计算语义召回的候选集大小。
// 使用 callerLimit 的 2 倍作为 TopK，保证去重后仍有足够结果填充预算。
func hybridSemanticTopK(cfg memorymodel.Config, limit int) int {
	return limit * 2
}

func resolveBudgetMemoryType(memoryType string) string {
	normalized := memorymodel.NormalizeMemoryType(memoryType)
	if normalized == "" {
		return memorymodel.MemoryTypeFact
	}
	return normalized
}

func renderMemoryTypeLabelForDedup(memoryType string) string {
	switch memorymodel.NormalizeMemoryType(memoryType) {
	case memorymodel.MemoryTypePreference:
		return "偏好"
	case memorymodel.MemoryTypeConstraint:
		return "约束"
	case memorymodel.MemoryTypeFact:
		return "事实"
	default:
		return "记忆"
	}
}

func collectItemDTOIDs(items []memorymodel.ItemDTO) []int64 {
	if len(items) == 0 {
		return nil
	}

	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.ID <= 0 {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids
}

func reverseItemDTOs(items []memorymodel.ItemDTO) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
