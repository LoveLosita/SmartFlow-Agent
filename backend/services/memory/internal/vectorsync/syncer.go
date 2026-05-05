package vectorsync

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	memoryrepo "github.com/LoveLosita/smartflow/backend/services/memory/internal/repo"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
)

// Syncer 负责 memory_items 与向量库之间的最小桥接。
//
// 职责边界：
// 1. 只负责“把已经落库的记忆同步到 RAG / 从 RAG 删除”；
// 2. 不负责决定哪些记忆该写、该删、该恢复，这些决策仍由上游 service/worker/cleanup 控制；
// 3. 同步失败时只回写 vector_status 并打观测，不反向回滚业务事务，避免把在线链路拖成强依赖。
type Syncer struct {
	ragRuntime ragservice.Runtime
	itemRepo   *memoryrepo.ItemRepo
	observer   memoryobserve.Observer
	metrics    memoryobserve.MetricsRecorder
	logger     *log.Logger
}

func NewSyncer(
	ragRuntime ragservice.Runtime,
	itemRepo *memoryrepo.ItemRepo,
	observer memoryobserve.Observer,
	metrics memoryobserve.MetricsRecorder,
) *Syncer {
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}
	return &Syncer{
		ragRuntime: ragRuntime,
		itemRepo:   itemRepo,
		observer:   observer,
		metrics:    metrics,
		logger:     log.Default(),
	}
}

// Upsert 把新增/修改/恢复后的记忆同步到向量库。
func (s *Syncer) Upsert(ctx context.Context, traceID string, items []model.MemoryItem) {
	if s == nil || s.ragRuntime == nil || s.itemRepo == nil || len(items) == 0 {
		return
	}

	requestItems := make([]ragservice.MemoryIngestItem, 0, len(items))
	for _, item := range items {
		requestItems = append(requestItems, ragservice.MemoryIngestItem{
			MemoryID:         item.ID,
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
		})
	}

	result, err := s.ragRuntime.IngestMemory(memoryobserve.WithFields(ctx, map[string]any{
		"trace_id": traceID,
	}), ragservice.MemoryIngestRequest{
		TraceID: traceID,
		Action:  "add",
		Items:   requestItems,
	})
	if err != nil {
		s.observer.Observe(ctx, memoryobserve.Event{
			Level:     memoryobserve.LevelWarn,
			Component: memoryobserve.ComponentWrite,
			Operation: "vector_upsert",
			Fields: map[string]any{
				"trace_id":   traceID,
				"item_count": len(items),
				"success":    false,
				"error":      err,
				"error_code": memoryobserve.ClassifyError(err),
			},
		})
		for _, item := range items {
			_ = s.itemRepo.UpdateVectorStateByID(ctx, item.ID, "failed", nil)
		}
		return
	}

	vectorIDMap := make(map[int64]string, len(result.DocumentIDs))
	for _, documentID := range result.DocumentIDs {
		memoryID := parseMemoryID(documentID)
		if memoryID <= 0 {
			continue
		}
		vectorIDMap[memoryID] = documentID
	}

	for _, item := range items {
		vectorID := strPtrOrNil(vectorIDMap[item.ID])
		_ = s.itemRepo.UpdateVectorStateByID(ctx, item.ID, "synced", vectorID)
	}
	s.observer.Observe(ctx, memoryobserve.Event{
		Level:     memoryobserve.LevelInfo,
		Component: memoryobserve.ComponentWrite,
		Operation: "vector_upsert",
		Fields: map[string]any{
			"trace_id":       traceID,
			"item_count":     len(items),
			"document_count": len(result.DocumentIDs),
			"success":        true,
		},
	})
}

// Delete 把一批记忆对应的向量从向量库中删除。
func (s *Syncer) Delete(ctx context.Context, traceID string, memoryIDs []int64) {
	if s == nil || len(memoryIDs) == 0 {
		return
	}
	if s.ragRuntime == nil || s.itemRepo == nil {
		return
	}

	documentIDs := make([]string, 0, len(memoryIDs))
	for _, id := range memoryIDs {
		documentIDs = append(documentIDs, fmt.Sprintf("memory:%d", id))
	}

	err := s.ragRuntime.DeleteMemory(memoryobserve.WithFields(ctx, map[string]any{
		"trace_id": traceID,
	}), documentIDs)
	if err != nil {
		s.observer.Observe(ctx, memoryobserve.Event{
			Level:     memoryobserve.LevelWarn,
			Component: memoryobserve.ComponentWrite,
			Operation: "vector_delete",
			Fields: map[string]any{
				"trace_id":   traceID,
				"item_count": len(memoryIDs),
				"success":    false,
				"error":      err,
				"error_code": memoryobserve.ClassifyError(err),
			},
		})
		for _, memoryID := range memoryIDs {
			_ = s.itemRepo.UpdateVectorStateByID(ctx, memoryID, "failed", nil)
		}
		return
	}

	for _, memoryID := range memoryIDs {
		_ = s.itemRepo.UpdateVectorStateByID(ctx, memoryID, "deleted", nil)
	}
	s.observer.Observe(ctx, memoryobserve.Event{
		Level:     memoryobserve.LevelInfo,
		Component: memoryobserve.ComponentWrite,
		Operation: "vector_delete",
		Fields: map[string]any{
			"trace_id":   traceID,
			"item_count": len(memoryIDs),
			"success":    true,
		},
	})
}

func parseMemoryID(documentID string) int64 {
	documentID = strings.TrimSpace(documentID)
	if !strings.HasPrefix(documentID, "memory:") {
		return 0
	}
	raw := strings.TrimPrefix(documentID, "memory:")
	if strings.HasPrefix(raw, "uid:") {
		return 0
	}
	var value int64
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return 0
		}
		value = value*10 + int64(ch-'0')
	}
	return value
}

func strPtrOrNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	value := v
	return &value
}

func strValue(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
