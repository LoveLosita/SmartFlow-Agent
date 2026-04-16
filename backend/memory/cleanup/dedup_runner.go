package cleanup

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	memoryobserve "github.com/LoveLosita/smartflow/backend/memory/observe"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	memoryvectorsync "github.com/LoveLosita/smartflow/backend/memory/vectorsync"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// DedupRunner 负责执行一次离线记忆去重治理。
//
// 职责边界：
// 1. 只处理“active + content_hash 非空”的重复组；
// 2. 只负责 archive + audit + 向量删除桥接，不负责自动定时调度；
// 3. 支持 dry-run，便于上线初期先观察治理结果再正式落库。
type DedupRunner struct {
	db           *gorm.DB
	itemRepo     *memoryrepo.ItemRepo
	auditRepo    *memoryrepo.AuditRepo
	vectorSyncer *memoryvectorsync.Syncer
	observer     memoryobserve.Observer
	metrics      memoryobserve.MetricsRecorder
}

func NewDedupRunner(
	db *gorm.DB,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	vectorSyncer *memoryvectorsync.Syncer,
	observer memoryobserve.Observer,
	metrics memoryobserve.MetricsRecorder,
) *DedupRunner {
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}
	return &DedupRunner{
		db:           db,
		itemRepo:     itemRepo,
		auditRepo:    auditRepo,
		vectorSyncer: vectorSyncer,
		observer:     observer,
		metrics:      metrics,
	}
}

// Run 执行一次离线去重治理。
func (r *DedupRunner) Run(ctx context.Context, req model.MemoryDedupCleanupRequest) (model.MemoryDedupCleanupResult, error) {
	result := model.MemoryDedupCleanupResult{
		DryRun: req.DryRun,
	}
	if r == nil || r.db == nil || r.itemRepo == nil || r.auditRepo == nil {
		return result, errors.New("memory dedup runner is not initialized")
	}

	items, err := r.itemRepo.ListActiveItemsForDedup(ctx, req.UserID, req.Limit)
	if err != nil {
		r.recordDedupObserve(ctx, req, result, false, err)
		return result, err
	}

	groups := groupDuplicateItems(items)
	result.ScannedGroupCount = len(groups)
	if len(groups) == 0 {
		r.recordDedupObserve(ctx, req, result, true, nil)
		return result, nil
	}

	for _, group := range groups {
		decision := DecideDedupGroup(group)
		if decision.Keep.ID > 0 {
			result.KeptCount++
		}
		if len(decision.Archive) == 0 {
			continue
		}

		result.DedupedGroupCount++
		archiveIDs := collectDedupIDs(decision.Archive)
		result.ArchivedCount += len(archiveIDs)
		result.ArchivedIDs = append(result.ArchivedIDs, archiveIDs...)
		if req.DryRun {
			continue
		}

		now := time.Now()
		txErr := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			itemRepo := r.itemRepo.WithTx(tx)
			auditRepo := r.auditRepo.WithTx(tx)

			if archiveErr := itemRepo.ArchiveByIDsAt(ctx, archiveIDs, now); archiveErr != nil {
				return archiveErr
			}

			for _, item := range decision.Archive {
				after := item
				after.Status = model.MemoryItemStatusArchived
				after.UpdatedAt = &now
				after.VectorStatus = "pending"

				audit := memoryutils.BuildItemAuditLog(
					item.ID,
					item.UserID,
					memoryutils.AuditOperationArchive,
					normalizeCleanupOperator(req.OperatorType),
					normalizeCleanupReason(req.Reason),
					&item,
					&after,
				)
				if createErr := auditRepo.Create(ctx, audit); createErr != nil {
					return createErr
				}
			}
			return nil
		})
		if txErr != nil {
			r.recordDedupObserve(ctx, req, result, false, txErr)
			return result, txErr
		}

		r.vectorSyncer.Delete(ctx, "", archiveIDs)
		r.metrics.AddCounter(memoryobserve.MetricCleanupArchivedTotal, int64(len(archiveIDs)), map[string]string{
			"dry_run": "false",
		})
	}

	r.recordDedupObserve(ctx, req, result, true, nil)
	return result, nil
}

func groupDuplicateItems(items []model.MemoryItem) [][]model.MemoryItem {
	if len(items) == 0 {
		return nil
	}

	result := make([][]model.MemoryItem, 0)
	currentGroup := make([]model.MemoryItem, 0, 2)
	currentKey := ""
	for _, item := range items {
		key := dedupGroupKey(item)
		if key == "" {
			continue
		}
		if currentKey == "" || currentKey != key {
			if len(currentGroup) > 1 {
				copied := make([]model.MemoryItem, len(currentGroup))
				copy(copied, currentGroup)
				result = append(result, copied)
			}
			currentKey = key
			currentGroup = currentGroup[:0]
		}
		currentGroup = append(currentGroup, item)
	}
	if len(currentGroup) > 1 {
		copied := make([]model.MemoryItem, len(currentGroup))
		copy(copied, currentGroup)
		result = append(result, copied)
	}
	return result
}

func dedupGroupKey(item model.MemoryItem) string {
	contentHash := strings.TrimSpace(derefString(item.ContentHash))
	if item.UserID <= 0 || strings.TrimSpace(item.MemoryType) == "" || contentHash == "" {
		return ""
	}
	return strings.Join([]string{
		strconv.Itoa(item.UserID),
		item.MemoryType,
		contentHash,
	}, "::")
}

func collectDedupIDs(items []model.MemoryItem) []int64 {
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.ID <= 0 {
			continue
		}
		ids = append(ids, item.ID)
	}
	return ids
}

func normalizeCleanupOperator(operatorType string) string {
	operatorType = strings.TrimSpace(operatorType)
	if operatorType == "" {
		return "system"
	}
	return memoryutils.NormalizeOperatorType(operatorType)
}

func normalizeCleanupReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "离线 dedup 治理归档重复记忆"
	}
	return reason
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (r *DedupRunner) recordDedupObserve(
	ctx context.Context,
	req model.MemoryDedupCleanupRequest,
	result model.MemoryDedupCleanupResult,
	success bool,
	err error,
) {
	if r == nil {
		return
	}

	status := "success"
	level := memoryobserve.LevelInfo
	if !success || err != nil {
		status = "error"
		level = memoryobserve.LevelWarn
	}

	r.observer.Observe(ctx, memoryobserve.Event{
		Level:     level,
		Component: memoryobserve.ComponentCleanup,
		Operation: memoryobserve.OperationDedup,
		Fields: map[string]any{
			"user_id":             req.UserID,
			"limit":               req.Limit,
			"dry_run":             req.DryRun,
			"scanned_group_count": result.ScannedGroupCount,
			"deduped_group_count": result.DedupedGroupCount,
			"archived_count":      result.ArchivedCount,
			"success":             success && err == nil,
			"error":               err,
			"error_code":          memoryobserve.ClassifyError(err),
		},
	})
	r.metrics.AddCounter(memoryobserve.MetricCleanupRunTotal, 1, map[string]string{
		"operation": "dedup",
		"status":    status,
	})
}
