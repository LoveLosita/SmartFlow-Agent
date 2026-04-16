package service

import (
	"context"
	"strings"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	"github.com/LoveLosita/smartflow/backend/model"
)

// HybridRetrieve 统一承接读取侧混合召回链路。
//
// 步骤化说明：
// 1. 结构化路由先取 constraint / 高置信 preference，给模型一份稳定“硬约束底座”；
// 2. 再补语义候选，优先走 RAG；RAG 报错或 0 命中时都回退 MySQL，保证链路韧性；
// 3. 两路结果统一做三级去重、排序与类型预算裁剪，只对最终真正注入的条目刷新 last_access_at；
// 4. 旧 legacy 链路完全保留，方便通过配置快速回滚。
func (s *ReadService) HybridRetrieve(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	limit int,
	now time.Time,
) ([]memorymodel.ItemDTO, error) {
	if s == nil || s.itemRepo == nil {
		return nil, nil
	}
	if !effectiveSetting.MemoryEnabled {
		return nil, nil
	}

	pinnedItems, err := s.retrievePinnedCandidates(ctx, req, effectiveSetting, now)
	if err != nil {
		return nil, err
	}
	semanticItems, err := s.retrieveSemanticCandidates(ctx, req, effectiveSetting, limit, now)
	if err != nil {
		return nil, err
	}

	merged := make([]memorymodel.ItemDTO, 0, len(pinnedItems)+len(semanticItems))
	merged = append(merged, pinnedItems...)
	merged = append(merged, semanticItems...)
	if len(merged) == 0 {
		return nil, nil
	}

	merged = dedupByID(merged)
	merged = dedupByHash(merged)
	merged = dedupByText(merged)
	merged = RankItems(merged, now)
	merged = applyTypeBudget(merged, s.cfg)
	if len(merged) == 0 {
		return nil, nil
	}

	_ = s.itemRepo.TouchLastAccessAt(ctx, collectItemDTOIDs(merged), now)
	return merged, nil
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
) ([]memorymodel.ItemDTO, error) {
	queryText := strings.TrimSpace(req.Query)
	if queryText == "" {
		return nil, nil
	}

	candidateLimit := hybridSemanticTopK(s.cfg, limit)
	if s.cfg.RAGEnabled && s.ragRuntime != nil {
		items, err := s.retrieveSemanticCandidatesByRAG(ctx, req, effectiveSetting, candidateLimit, now)
		if shouldReturnSemanticRAGResult(items, err) {
			return items, nil
		}
	}
	return s.retrieveSemanticCandidatesByMySQL(ctx, req, effectiveSetting, candidateLimit, now)
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
		normalizeLimit(candidateLimit*3, candidateLimit*3, maxRetrieveLimit*3),
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

// applyTypeBudget 在排序结果上应用四类记忆预算。
//
// 说明：
// 1. 每种类型先保底自己的预算上限，避免 fact 抢掉 constraint 的位置；
// 2. 裁剪时保持当前排序顺序，不在这里重新打分；
// 3. 最终总量由四类预算之和共同决定，默认 18 条。
func applyTypeBudget(items []memorymodel.ItemDTO, cfg memorymodel.Config) []memorymodel.ItemDTO {
	if len(items) == 0 {
		return nil
	}

	budgetByType := map[string]int{
		memorymodel.MemoryTypeConstraint: cfg.EffectiveReadConstraintLimit(),
		memorymodel.MemoryTypePreference: cfg.EffectiveReadPreferenceLimit(),
		memorymodel.MemoryTypeFact:       cfg.EffectiveReadFactLimit(),
		memorymodel.MemoryTypeTodoHint:   cfg.EffectiveReadTodoHintLimit(),
	}
	usedByType := make(map[string]int, len(budgetByType))
	result := make([]memorymodel.ItemDTO, 0, minInt(len(items), cfg.TotalReadBudget()))
	for _, item := range items {
		if len(result) >= cfg.TotalReadBudget() {
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

func hybridSemanticTopK(cfg memorymodel.Config, limit int) int {
	if cfg.TotalReadBudget() > limit {
		return cfg.TotalReadBudget()
	}
	return limit
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
	case memorymodel.MemoryTypeTodoHint:
		return "待办线索"
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
