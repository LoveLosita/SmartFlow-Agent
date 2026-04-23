package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/memory/observe"
	memoryorchestrator "github.com/LoveLosita/smartflow/backend/memory/orchestrator"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	memoryutils "github.com/LoveLosita/smartflow/backend/memory/utils"
	memoryvectorsync "github.com/LoveLosita/smartflow/backend/memory/vectorsync"
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
	vectorSyncer *memoryvectorsync.Syncer
	observer     memoryobserve.Observer
	metrics      memoryobserve.MetricsRecorder

	// 决策层依赖。
	// 说明：
	// 1. cfg 提供决策层配置（是否启用、TopK、MinScore、FallbackMode）；
	// 2. decisionOrchestrator 在决策启用时负责 LLM 逐对比较，为 nil 时走旧路径。
	cfg                  memorymodel.Config
	decisionOrchestrator *memoryorchestrator.LLMDecisionOrchestrator
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
	cfg memorymodel.Config,
	decisionOrchestrator *memoryorchestrator.LLMDecisionOrchestrator,
	vectorSyncer *memoryvectorsync.Syncer,
	observer memoryobserve.Observer,
	metrics memoryobserve.MetricsRecorder,
) *Runner {
	if observer == nil {
		observer = memoryobserve.NewNopObserver()
	}
	if metrics == nil {
		metrics = memoryobserve.NewNopMetrics()
	}
	return &Runner{
		db:                   db,
		jobRepo:              jobRepo,
		itemRepo:             itemRepo,
		auditRepo:            auditRepo,
		settingsRepo:         settingsRepo,
		extractor:            extractor,
		ragRuntime:           ragRuntime,
		logger:               log.Default(),
		vectorSyncer:         vectorSyncer,
		observer:             observer,
		metrics:              metrics,
		cfg:                  cfg,
		decisionOrchestrator: decisionOrchestrator,
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
	if job.RetryCount > 0 {
		r.metrics.AddCounter(memoryobserve.MetricJobRetryTotal, 1, map[string]string{
			"job_type": strings.TrimSpace(job.JobType),
		})
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
		r.recordJobOutcome(ctx, job, nil, result.Status, false, err)
		return result, nil
	}

	// 3. 先读取用户记忆设置。总开关关闭时，任务直接成功结束，不再继续抽取和落库。
	setting, err := r.settingsRepo.GetByUserID(ctx, payload.UserID)
	if err != nil {
		r.recordJobOutcome(ctx, job, &payload, model.MemoryJobStatusFailed, false, err)
		return nil, err
	}
	effectiveSetting := memoryutils.EffectiveUserSetting(setting, payload.UserID)
	if !effectiveSetting.MemoryEnabled {
		if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
			r.recordJobOutcome(ctx, job, &payload, model.MemoryJobStatusFailed, false, err)
			return nil, err
		}
		result.Status = model.MemoryJobStatusSuccess
		r.logger.Printf("memory worker skipped by user setting: job_id=%d user_id=%d", job.ID, payload.UserID)
		r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
		return result, nil
	}

	// 4. 调用抽取器。LLM 失败时由编排器做保守 fallback，worker 只关心最终结果。
	facts, extractErr := r.extractor.ExtractFacts(ctx, payload)
	if extractErr != nil {
		failReason := fmt.Sprintf("抽取执行失败: %v", extractErr)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		r.recordJobOutcome(ctx, job, &payload, result.Status, false, extractErr)
		return result, nil
	}
	facts = memoryutils.FilterFactsBySetting(facts, effectiveSetting)
	facts = memoryutils.FilterFactsByConfidence(facts, r.cfg.WriteMinConfidence)

	if len(facts) == 0 {
		if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
			r.recordJobOutcome(ctx, job, &payload, model.MemoryJobStatusFailed, false, err)
			return nil, err
		}
		result.Status = model.MemoryJobStatusSuccess
		r.logger.Printf("memory worker run once noop: job_id=%d", job.ID)
		r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
		return result, nil
	}

	items := buildMemoryItems(job, payload, facts)
	if len(items) == 0 {
		if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
			r.recordJobOutcome(ctx, job, &payload, model.MemoryJobStatusFailed, false, err)
			return nil, err
		}
		result.Status = model.MemoryJobStatusSuccess
		r.logger.Printf("memory worker run once empty-after-normalize: job_id=%d", job.ID)
		r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
		return result, nil
	}

	// 5. 根据配置选择写入路径：决策层 or 旧路径。
	if r.cfg.DecisionEnabled && r.decisionOrchestrator != nil {
		// 5a. 决策路径：召回→比对→汇总→执行。
		outcome, decisionErr := r.executeDecisionFlow(ctx, job, payload, facts)
		if decisionErr != nil {
			// 决策流程整体失败，根据 FallbackMode 决定是否退回旧路径。
			r.logger.Printf("[WARN][去重] 决策流程整体失败: job_id=%d user_id=%d facts_count=%d fallback=%s err=%v", job.ID, payload.UserID, len(facts), r.cfg.DecisionFallbackMode, decisionErr)
			if r.cfg.DecisionFallbackMode == "legacy_add" {
				if err = r.persistMemoryWrite(ctx, job.ID, items); err != nil {
					failReason := fmt.Sprintf("决策降级后记忆落库失败: %v", err)
					_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
					result.Status = model.MemoryJobStatusFailed
					r.recordJobOutcome(ctx, job, &payload, result.Status, false, err)
					return result, nil
				}
				result.Status = model.MemoryJobStatusSuccess
				result.Facts = len(items)
				r.syncMemoryVectors(ctx, items)
				r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
				return result, nil
			}
			// FallbackMode=drop：丢弃本轮抽取结果，直接标记 job 成功。
			_ = r.jobRepo.MarkSuccess(ctx, job.ID)
			result.Status = model.MemoryJobStatusSuccess
			r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
			return result, nil
		}

		// 5b. 决策成功：同步向量（新增/更新）和删除过期向量。
		result.Status = model.MemoryJobStatusSuccess
		result.Facts = outcome.AddCount + outcome.UpdateCount + outcome.DeleteCount
		r.syncMemoryVectors(ctx, outcome.ItemsToSync)
		r.syncVectorDeletes(ctx, outcome.VectorDeletes)
		r.logger.Printf("[去重] 决策流程完成: job_id=%d user_id=%d 新增=%d 更新=%d 删除=%d 跳过=%d",
			job.ID, payload.UserID, outcome.AddCount, outcome.UpdateCount, outcome.DeleteCount, outcome.NoneCount)
		r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
		return result, nil
	}

	// 5c. 旧路径：和现在完全一样 — 先在事务里写入记忆条目和审计日志，再统一确认 job 成功。
	if err = r.persistMemoryWrite(ctx, job.ID, items); err != nil {
		failReason := fmt.Sprintf("记忆落库失败: %v", err)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		r.recordJobOutcome(ctx, job, &payload, result.Status, false, err)
		return result, nil
	}

	result.Status = model.MemoryJobStatusSuccess
	result.Facts = len(items)
	r.syncMemoryVectors(ctx, items)
	r.logger.Printf("memory worker run once success: job_id=%d extracted_facts=%d", job.ID, len(items))
	r.recordJobOutcome(ctx, job, &payload, result.Status, true, nil)
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
	if r == nil || r.vectorSyncer == nil || len(items) == 0 {
		return
	}
	r.vectorSyncer.Upsert(ctx, "", items)
}

// syncVectorDeletes 处理决策层 DELETE 动作产出的向量清理需求。
//
// 步骤：
// 1. 将 memoryID 转为 Milvus documentID（"memory:{id}" 格式）；
// 2. 调 Runtime.DeleteMemory 真正从 Milvus 删除对应向量；
// 3. 更新 MySQL vector_status 标记删除结果。
func (r *Runner) syncVectorDeletes(ctx context.Context, memoryIDs []int64) {
	if r == nil || r.vectorSyncer == nil || len(memoryIDs) == 0 {
		return
	}
	r.vectorSyncer.Delete(ctx, "", memoryIDs)
}

func resolveMemoryTTLAt(base time.Time, memoryType string) *time.Time {
	switch memoryType {
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

func (r *Runner) recordJobOutcome(
	ctx context.Context,
	job *model.MemoryJob,
	payload *memorymodel.ExtractJobPayload,
	status string,
	success bool,
	err error,
) {
	if r == nil {
		return
	}

	level := memoryobserve.LevelInfo
	if !success || err != nil {
		level = memoryobserve.LevelWarn
	}
	fields := map[string]any{
		"job_id":     jobIDValue(job),
		"status":     strings.TrimSpace(status),
		"success":    success && err == nil,
		"error":      err,
		"error_code": memoryobserve.ClassifyError(err),
	}
	if payload != nil {
		fields["trace_id"] = strings.TrimSpace(payload.TraceID)
		fields["user_id"] = payload.UserID
		fields["conversation_id"] = strings.TrimSpace(payload.ConversationID)
	}

	r.observer.Observe(ctx, memoryobserve.Event{
		Level:     level,
		Component: memoryobserve.ComponentWrite,
		Operation: "job",
		Fields:    fields,
	})
	r.metrics.AddCounter(memoryobserve.MetricJobTotal, 1, map[string]string{
		"status": strings.TrimSpace(status),
	})
}

func (r *Runner) recordDecisionObservation(
	ctx context.Context,
	job *model.MemoryJob,
	payload memorymodel.ExtractJobPayload,
	fact memorymodel.NormalizedFact,
	candidateCount int,
	finalAction string,
	fallbackMode string,
	success bool,
	err error,
) {
	if r == nil {
		return
	}

	level := memoryobserve.LevelInfo
	status := "success"
	if !success || err != nil {
		level = memoryobserve.LevelWarn
		status = "error"
	}
	fallbackMode = strings.TrimSpace(fallbackMode)
	if fallbackMode == "" {
		fallbackMode = "none"
	}

	r.observer.Observe(ctx, memoryobserve.Event{
		Level:     level,
		Component: memoryobserve.ComponentWrite,
		Operation: memoryobserve.OperationDecision,
		Fields: map[string]any{
			"trace_id":        strings.TrimSpace(payload.TraceID),
			"user_id":         payload.UserID,
			"conversation_id": strings.TrimSpace(payload.ConversationID),
			"job_id":          jobIDValue(job),
			"fact_type":       strings.TrimSpace(fact.MemoryType),
			"candidate_count": candidateCount,
			"final_action":    strings.TrimSpace(finalAction),
			"fallback_mode":   fallbackMode,
			"success":         success && err == nil,
			"error":           err,
			"error_code":      memoryobserve.ClassifyError(err),
		},
	})
	r.metrics.AddCounter(memoryobserve.MetricDecisionTotal, 1, map[string]string{
		"action": strings.TrimSpace(finalAction),
		"status": status,
	})
	if fallbackMode != "none" && fallbackMode != "hash_exact" && fallbackMode != "rag" {
		r.metrics.AddCounter(memoryobserve.MetricDecisionFallbackTotal, 1, map[string]string{
			"mode": fallbackMode,
		})
	}
}

func jobIDValue(job *model.MemoryJob) int64 {
	if job == nil {
		return 0
	}
	return job.ID
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
