package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// RunOnceResult 描述单次手工触发执行的结果。
type RunOnceResult struct {
	Claimed bool
	JobID   int64
	Status  string
	Facts   int
}

// Runner 负责把 memory_jobs 推进成 memory_items 和审计日志。
//
// 职责边界：
// 1. 负责任务抢占、抽取、落库和状态推进；
// 2. 不负责 outbox 消费，也不负责 LLM prompt 组装；
// 3. 失败时只做可恢复的状态回写，避免把业务错误直接抛到启动层。
type Runner struct {
	db           *gorm.DB
	jobRepo      *memoryrepo.JobRepo
	itemRepo     *memoryrepo.ItemRepo
	auditRepo    *memoryrepo.AuditRepo
	settingsRepo *memoryrepo.SettingsRepo
	extractor    Extractor
	ragRuntime   infrarag.Runtime
	logger       *log.Logger
}

// NewRunner 构造记忆 worker 执行器。
func NewRunner(
	db *gorm.DB,
	jobRepo *memoryrepo.JobRepo,
	itemRepo *memoryrepo.ItemRepo,
	auditRepo *memoryrepo.AuditRepo,
	settingsRepo *memoryrepo.SettingsRepo,
	extractor Extractor,
	ragRuntime infrarag.Runtime,
) *Runner {
	return &Runner{
		db:           db,
		jobRepo:      jobRepo,
		itemRepo:     itemRepo,
		auditRepo:    auditRepo,
		settingsRepo: settingsRepo,
		extractor:    extractor,
		ragRuntime:   ragRuntime,
		logger:       log.Default(),
	}
}

// RunOnce 手工执行一轮任务处理。
//
// 返回语义：
// 1. Claimed=false 表示当前没有可执行任务；
// 2. Claimed=true 且 Status=success/failed/dead 表示本轮已经推进过一个任务；
// 3. 只有初始化缺失或数据库级错误才返回 error。
func (r *Runner) RunOnce(ctx context.Context) (*RunOnceResult, error) {
	if r == nil || r.db == nil || r.jobRepo == nil || r.itemRepo == nil || r.auditRepo == nil || r.settingsRepo == nil || r.extractor == nil {
		return nil, errors.New("memory worker runner is not initialized")
	}

	// 1. 先抢占一条可执行任务，避免多个 worker 重复处理同一条记录。
	job, err := r.jobRepo.ClaimNextRunnableExtractJob(ctx, time.Now())
	if err != nil {
		return nil, err
	}
	if job == nil {
		return &RunOnceResult{Claimed: false}, nil
	}

	result := &RunOnceResult{
		Claimed: true,
		JobID:   job.ID,
		Status:  model.MemoryJobStatusProcessing,
		Facts:   0,
	}

	// 2. 解析任务载荷。这里属于数据质量问题，解析失败就直接标记为可重试失败。
	var payload memorymodel.ExtractJobPayload
	if err = json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		failReason := fmt.Sprintf("解析任务载荷失败: %v", err)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		return result, nil
	}

	// 3. 先读取用户记忆设置。总开关关闭时，任务直接成功结束，不再继续抽取和落库。
	setting, err := r.settingsRepo.GetByUserID(ctx, payload.UserID)
	if err != nil {
		return nil, err
	}
	effectiveSetting := memoryutils.EffectiveUserSetting(setting, payload.UserID)
	if !effectiveSetting.MemoryEnabled {
		if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
			return nil, err
		}
		result.Status = model.MemoryJobStatusSuccess
		r.logger.Printf("memory worker skipped by user setting: job_id=%d user_id=%d", job.ID, payload.UserID)
		return result, nil
	}

	// 4. 调用抽取器。LLM 失败时由编排器做保守 fallback，worker 只关心最终结果。
	facts, extractErr := r.extractor.ExtractFacts(ctx, payload)
	if extractErr != nil {
		failReason := fmt.Sprintf("抽取执行失败: %v", extractErr)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		return result, nil
	}
	facts = memoryutils.FilterFactsBySetting(facts, effectiveSetting)

	if len(facts) == 0 {
		if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
			return nil, err
		}
		result.Status = model.MemoryJobStatusSuccess
		r.logger.Printf("memory worker run once noop: job_id=%d", job.ID)
		return result, nil
	}

	items := buildMemoryItems(job, payload, facts)
	if len(items) == 0 {
		if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
			return nil, err
		}
		result.Status = model.MemoryJobStatusSuccess
		r.logger.Printf("memory worker run once empty-after-normalize: job_id=%d", job.ID)
		return result, nil
	}

	// 5. 先在事务里写入记忆条目和审计日志，再统一确认 job 成功。
	if err = r.persistMemoryWrite(ctx, job.ID, items); err != nil {
		failReason := fmt.Sprintf("记忆落库失败: %v", err)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		return result, nil
	}

	result.Status = model.MemoryJobStatusSuccess
	result.Facts = len(items)
	r.syncMemoryVectors(ctx, items)
	r.logger.Printf("memory worker run once success: job_id=%d extracted_facts=%d", job.ID, len(items))
	return result, nil
}

func (r *Runner) persistMemoryWrite(ctx context.Context, jobID int64, items []model.MemoryItem) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		jobRepo := r.jobRepo.WithTx(tx)
		itemRepo := r.itemRepo.WithTx(tx)
		auditRepo := r.auditRepo.WithTx(tx)

		if err := itemRepo.UpsertItems(ctx, items); err != nil {
			return err
		}

		for i := range items {
			audit := memoryutils.BuildItemAuditLog(
				items[i].ID,
				items[i].UserID,
				memoryutils.AuditOperationCreate,
				"system",
				"LLM 提取入库",
				nil,
				&items[i],
			)
			if err := auditRepo.Create(ctx, audit); err != nil {
				return err
			}
		}

		return jobRepo.MarkSuccess(ctx, jobID)
	})
}

func buildMemoryItems(job *model.MemoryJob, payload memorymodel.ExtractJobPayload, facts []memorymodel.NormalizedFact) []model.MemoryItem {
	if job == nil || len(facts) == 0 {
		return nil
	}

	items := make([]model.MemoryItem, 0, len(facts))
	for _, fact := range facts {
		items = append(items, model.MemoryItem{
			UserID:            payload.UserID,
			ConversationID:    strPtrOrNil(payload.ConversationID),
			AssistantID:       strPtrOrNil(payload.AssistantID),
			RunID:             strPtrOrNil(payload.RunID),
			MemoryType:        fact.MemoryType,
			Title:             fact.Title,
			Content:           fact.Content,
			NormalizedContent: strPtrFromValue(fact.NormalizedContent),
			ContentHash:       strPtrFromValue(fact.ContentHash),
			Confidence:        fact.Confidence,
			Importance:        fact.Importance,
			SensitivityLevel:  fact.SensitivityLevel,
			SourceMessageID:   int64PtrOrNil(payload.SourceMessageID),
			SourceEventID:     job.SourceEventID,
			IsExplicit:        fact.IsExplicit,
			Status:            model.MemoryItemStatusActive,
			TTLAt:             resolveMemoryTTLAt(payload.OccurredAt, fact.MemoryType),
			VectorStatus:      "pending",
		})
	}
	return items
}

func (r *Runner) syncMemoryVectors(ctx context.Context, items []model.MemoryItem) {
	if r == nil || r.ragRuntime == nil || r.itemRepo == nil || len(items) == 0 {
		return
	}

	requestItems := make([]infrarag.MemoryIngestItem, 0, len(items))
	for _, item := range items {
		requestItems = append(requestItems, infrarag.MemoryIngestItem{
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

	result, err := r.ragRuntime.IngestMemory(ctx, infrarag.MemoryIngestRequest{
		Action: "add",
		Items:  requestItems,
	})
	if err != nil {
		r.logger.Printf("memory vector sync failed: err=%v", err)
		for _, item := range items {
			_ = r.itemRepo.UpdateVectorStateByID(ctx, item.ID, "failed", nil)
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
		_ = r.itemRepo.UpdateVectorStateByID(ctx, item.ID, "synced", vectorID)
	}
}

func resolveMemoryTTLAt(base time.Time, memoryType string) *time.Time {
	switch memoryType {
	case memorymodel.MemoryTypeTodoHint:
		t := base.Add(30 * 24 * time.Hour)
		return &t
	case memorymodel.MemoryTypeFact:
		t := base.Add(180 * 24 * time.Hour)
		return &t
	default:
		return nil
	}
}

func strPtrFromValue(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	value := v
	return &value
}

func strPtrOrNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	value := v
	return &value
}

func int64PtrOrNil(v int64) *int64 {
	if v <= 0 {
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

func parseMemoryID(documentID string) int64 {
	documentID = strings.TrimSpace(documentID)
	if !strings.HasPrefix(documentID, "memory:") {
		return 0
	}
	raw := strings.TrimPrefix(documentID, "memory:")
	if strings.HasPrefix(raw, "uid:") {
		return 0
	}
	memoryID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return memoryID
}
