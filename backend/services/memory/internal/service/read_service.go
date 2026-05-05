package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	memoryrepo "github.com/LoveLosita/smartflow/backend/services/memory/internal/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/services/memory/internal/utils"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

const (
	defaultRetrieveLimit = 5
	maxRetrieveLimit     = 20
)

// ReadService 负责 memory 模块内部的读取、门控与轻量重排。
//
// 职责边界：
// 1. 负责把 memory_items 读出来并做用户设置过滤；
// 2. 负责最小可用的排序与截断，为后续 prompt 注入提供稳定入口；
// 3. 不直接依赖 agent，不负责真正把记忆拼进 prompt。
type ReadService struct {
	itemRepo     *memoryrepo.ItemRepo
	settingsRepo *memoryrepo.SettingsRepo
	ragRuntime   ragservice.Runtime
	cfg          memorymodel.Config
	observer     memoryobserve.Observer
	metrics      memoryobserve.MetricsRecorder
}

type retrieveTelemetry struct {
	ReadMode         string
	QueryLen         int
	LegacyHitCount   int
	PinnedHitCount   int
	SemanticHitCount int
	DedupDropCount   int
	FinalCount       int
	Degraded         bool
	RAGFallbackUsed  bool
}

type semanticRetrieveTelemetry struct {
	HitCount        int
	Degraded        bool
	RAGFallbackUsed bool
}

func NewReadService(
	itemRepo *memoryrepo.ItemRepo,
	settingsRepo *memoryrepo.SettingsRepo,
	ragRuntime ragservice.Runtime,
	cfg memorymodel.Config,
	observer memoryobserve.Observer,
	metrics memoryobserve.MetricsRecorder,
) *ReadService {
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}
	return &ReadService{
		itemRepo:     itemRepo,
		settingsRepo: settingsRepo,
		ragRuntime:   ragRuntime,
		cfg:          cfg,
		observer:     observer,
		metrics:      metrics,
	}
}

// Retrieve 读取可供后续注入使用的候选记忆。
func (s *ReadService) Retrieve(ctx context.Context, req memorymodel.RetrieveRequest) ([]memorymodel.ItemDTO, error) {
	if s == nil || s.itemRepo == nil || s.settingsRepo == nil {
		return nil, nil
	}
	if req.UserID <= 0 {
		return nil, nil
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	telemetry := retrieveTelemetry{
		ReadMode: s.cfg.EffectiveReadMode(),
		QueryLen: len(strings.TrimSpace(req.Query)),
	}

	setting, err := s.settingsRepo.GetByUserID(ctx, req.UserID)
	if err != nil {
		s.recordRetrieve(ctx, req, telemetry, err)
		return nil, err
	}
	effectiveSetting := memoryutils.EffectiveUserSetting(setting, req.UserID)
	if !effectiveSetting.MemoryEnabled {
		return nil, nil
	}

	limit := normalizeLimit(req.Limit, defaultRetrieveLimit, maxRetrieveLimit)
	if s.cfg.EffectiveReadMode() == memorymodel.MemoryReadModeHybrid {
		items, hybridTelemetry, hybridErr := s.HybridRetrieve(ctx, req, effectiveSetting, limit, now)
		hybridTelemetry.ReadMode = memorymodel.MemoryReadModeHybrid
		hybridTelemetry.QueryLen = telemetry.QueryLen
		s.recordRetrieve(ctx, req, hybridTelemetry, hybridErr)
		return items, hybridErr
	}
	if s.cfg.RAGEnabled && s.ragRuntime != nil && strings.TrimSpace(req.Query) != "" {
		items, ragErr := s.retrieveByRAG(ctx, req, effectiveSetting, limit, now)
		if ragErr == nil && len(items) > 0 {
			telemetry.SemanticHitCount = len(items)
			telemetry.FinalCount = len(items)
			s.recordRetrieve(ctx, req, telemetry, nil)
			return items, nil
		}
		telemetry.Degraded = true
		telemetry.RAGFallbackUsed = true
	}

	items, legacyErr := s.retrieveByLegacy(ctx, req, limit, now, effectiveSetting)
	telemetry.LegacyHitCount = len(items)
	telemetry.FinalCount = len(items)
	s.recordRetrieve(ctx, req, telemetry, legacyErr)
	return items, legacyErr
}

func (s *ReadService) retrieveByLegacy(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	limit int,
	now time.Time,
	effectiveSetting model.MemoryUserSetting,
) ([]memorymodel.ItemDTO, error) {
	if !effectiveSetting.MemoryEnabled {
		return nil, nil
	}
	query := buildReadScopedItemQuery(
		req,
		now,
		[]string{model.MemoryItemStatusActive},
		normalizeLimit(limit*3, limit*3, maxRetrieveLimit*3),
	)

	items, err := s.itemRepo.FindByQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	items = memoryutils.FilterItemsBySetting(items, effectiveSetting)
	if len(items) == 0 {
		return nil, nil
	}

	sort.SliceStable(items, func(i, j int) bool {
		left := scoreRetrievedItem(items[i], now)
		right := scoreRetrievedItem(items[j], now)
		if left == right {
			return items[i].ID > items[j].ID
		}
		return left > right
	})

	if len(items) > limit {
		items = items[:limit]
	}
	_ = s.itemRepo.TouchLastAccessAt(ctx, collectMemoryIDs(items), now)
	return toItemDTOs(items), nil
}

func (s *ReadService) retrieveByRAG(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	effectiveSetting model.MemoryUserSetting,
	limit int,
	now time.Time,
) ([]memorymodel.ItemDTO, error) {
	if !effectiveSetting.MemoryEnabled {
		return nil, nil
	}

	result, err := s.ragRuntime.RetrieveMemory(ctx, buildReadScopedRAGRequest(req, limit, s.cfg.Threshold))
	if err != nil || result == nil || len(result.Items) == 0 {
		return nil, err
	}

	items := make([]memorymodel.ItemDTO, 0, len(result.Items))
	ids := make([]int64, 0, len(result.Items))
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
		if dto.ID > 0 {
			ids = append(ids, dto.ID)
		}
	}
	if len(items) > limit {
		items = items[:limit]
	}
	_ = s.itemRepo.TouchLastAccessAt(ctx, ids, now)
	return items, nil
}

func normalizeRetrieveMemoryTypes(raw []string) []string {
	normalized := normalizeMemoryTypes(raw)
	if len(normalized) > 0 {
		return normalized
	}
	return []string{
		memorymodel.MemoryTypeConstraint,
		memorymodel.MemoryTypePreference,
		memorymodel.MemoryTypeFact,
	}
}

func (s *ReadService) recordRetrieve(
	ctx context.Context,
	req memorymodel.RetrieveRequest,
	telemetry retrieveTelemetry,
	err error,
) {
	if s == nil {
		return
	}

	level := memoryobserve.LevelInfo
	if err != nil {
		level = memoryobserve.LevelWarn
	}
	s.observer.Observe(ctx, memoryobserve.Event{
		Level:     level,
		Component: memoryobserve.ComponentRead,
		Operation: memoryobserve.OperationRetrieve,
		Fields: map[string]any{
			"user_id":            req.UserID,
			"read_mode":          telemetry.ReadMode,
			"query_len":          telemetry.QueryLen,
			"legacy_hit_count":   telemetry.LegacyHitCount,
			"pinned_hit_count":   telemetry.PinnedHitCount,
			"semantic_hit_count": telemetry.SemanticHitCount,
			"dedup_drop_count":   telemetry.DedupDropCount,
			"final_count":        telemetry.FinalCount,
			"degraded":           telemetry.Degraded,
			"rag_fallback_used":  telemetry.RAGFallbackUsed,
			"success":            err == nil,
			"error":              err,
			"error_code":         memoryobserve.ClassifyError(err),
		},
	})

	if telemetry.FinalCount > 0 {
		s.metrics.AddCounter(memoryobserve.MetricRetrieveHitTotal, int64(telemetry.FinalCount), map[string]string{
			"read_mode": strings.TrimSpace(telemetry.ReadMode),
		})
	}
	if telemetry.DedupDropCount > 0 {
		s.metrics.AddCounter(memoryobserve.MetricRetrieveDedupDropTotal, int64(telemetry.DedupDropCount), map[string]string{
			"read_mode": strings.TrimSpace(telemetry.ReadMode),
		})
	}
	if telemetry.RAGFallbackUsed {
		s.metrics.AddCounter(memoryobserve.MetricRAGFallbackTotal, 1, map[string]string{
			"read_mode": strings.TrimSpace(telemetry.ReadMode),
		})
	}
}

// scoreRetrievedItem 计算 legacy 读链路的确定性排序分数。
//
// 说明：
// 1. 这里只保留 importance / confidence / recency / explicit / type 这些稳定特征；
// 2. conversation_id 已不再参与读侧打分，因为同对话信息本就已经在上下文窗口内；
// 3. 若后续需要引入语义分或 reranker，应在 DTO 层补齐对应字段后再统一并入。
func scoreRetrievedItem(item model.MemoryItem, now time.Time) float64 {
	score := 0.35*clamp01(item.Importance) + 0.3*clamp01(item.Confidence) + 0.2*recencyScore(item, now)
	if item.IsExplicit {
		score += 0.1
	}
	switch item.MemoryType {
	case memorymodel.MemoryTypeConstraint:
		score += 0.12
	case memorymodel.MemoryTypePreference:
		score += 0.08
	}
	return score
}

func recencyScore(item model.MemoryItem, now time.Time) float64 {
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

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func collectMemoryIDs(items []model.MemoryItem) []int64 {
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

func buildMemoryDTOFromRetrieveHit(hit ragservice.RetrieveHit) (memorymodel.ItemDTO, int64) {
	memoryID := parseMemoryIDFromDocumentID(hit.DocumentID)
	metadata := hit.Metadata
	content := strings.TrimSpace(hit.Text)
	memoryType := readString(metadata["memory_type"])
	dto := memorymodel.ItemDTO{
		ID:               memoryID,
		UserID:           int(readFloatLike(metadata["user_id"])),
		ConversationID:   readString(metadata["conversation_id"]),
		AssistantID:      readString(metadata["assistant_id"]),
		RunID:            readString(metadata["run_id"]),
		MemoryType:       memoryType,
		Title:            readString(metadata["title"]),
		Content:          content,
		ContentHash:      fallbackContentHash(memoryType, content, readString(metadata["content_hash"])),
		Confidence:       readFloatLike(metadata["confidence"]),
		Importance:       readFloatLike(metadata["importance"]),
		SensitivityLevel: int(readFloatLike(metadata["sensitivity_level"])),
		IsExplicit:       readBoolLike(metadata["is_explicit"]),
		Status:           readString(metadata["status"]),
		TTLAt:            readTimeLike(metadata["ttl_at"]),
	}
	return dto, memoryID
}

func parseMemoryIDFromDocumentID(documentID string) int64 {
	documentID = strings.TrimSpace(documentID)
	if !strings.HasPrefix(documentID, "memory:") {
		return 0
	}
	raw := strings.TrimPrefix(documentID, "memory:")
	if strings.HasPrefix(raw, "uid:") {
		return 0
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func readString(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func readFloatLike(v any) float64 {
	switch value := v.(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int64:
		return float64(value)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func readBoolLike(v any) bool {
	switch value := v.(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func readTimeLike(v any) *time.Time {
	text := readString(v)
	if text == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return nil
	}
	return &parsed
}
