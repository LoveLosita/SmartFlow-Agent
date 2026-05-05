package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// JobRepo 封装 memory_jobs 的数据访问。
type JobRepo struct {
	db *gorm.DB
}

func NewJobRepo(db *gorm.DB) *JobRepo {
	return &JobRepo{db: db}
}

func (r *JobRepo) WithTx(tx *gorm.DB) *JobRepo {
	return &JobRepo{db: tx}
}

// CreatePendingExtractJob 创建“待抽取”任务（幂等写入）。
//
// 失败语义：
// 1. 参数非法直接返回 error，由上游决定 dead 或重试；
// 2. 同幂等键重复写入采用 DoNothing，保证无副作用。
func (r *JobRepo) CreatePendingExtractJob(
	ctx context.Context,
	payload memorymodel.ExtractJobPayload,
	sourceEventID string,
) error {
	if r == nil || r.db == nil {
		return errors.New("memory job repo is nil")
	}
	if payload.UserID <= 0 {
		return errors.New("invalid user_id")
	}
	if payload.IdempotencyKey == "" {
		return errors.New("idempotency_key is empty")
	}

	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	now := time.Now()
	job := model.MemoryJob{
		UserID:          payload.UserID,
		ConversationID:  strPtrOrNil(payload.ConversationID),
		SourceMessageID: int64PtrOrNil(payload.SourceMessageID),
		SourceEventID:   strPtrOrNil(sourceEventID),
		JobType:         model.MemoryJobTypeExtract,
		IdempotencyKey:  payload.IdempotencyKey,
		PayloadJSON:     string(rawPayload),
		Status:          model.MemoryJobStatusPending,
		RetryCount:      0,
		MaxRetry:        6,
		NextRetryAt:     &now,
	}

	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "idempotency_key"}},
			DoNothing: true,
		}).
		Create(&job).Error
}

// ClaimNextRunnableExtractJob 抢占一个可执行的 extract 任务。
//
// 抢占规则：
// 1. 只从 pending/failed 中挑 next_retry_at 已到期任务；
// 2. 用行锁避免多个 worker 抢到同一条任务；
// 3. 抢占成功后立即置为 processing，防止重复执行。
func (r *JobRepo) ClaimNextRunnableExtractJob(ctx context.Context, now time.Time) (*model.MemoryJob, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("memory job repo is nil")
	}

	var claimed *model.MemoryJob
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.MemoryJob
		query := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("job_type = ?", model.MemoryJobTypeExtract).
			Where("status IN ?", []string{model.MemoryJobStatusPending, model.MemoryJobStatusFailed}).
			Where("(next_retry_at IS NULL OR next_retry_at <= ?)", now).
			Order("id ASC").
			Limit(1).
			Find(&job)
		if query.Error != nil {
			return query.Error
		}
		if query.RowsAffected == 0 {
			return nil
		}

		updates := map[string]any{
			"status":     model.MemoryJobStatusProcessing,
			"updated_at": now,
			"last_error": nil,
		}
		if updateErr := tx.Model(&model.MemoryJob{}).Where("id = ?", job.ID).Updates(updates).Error; updateErr != nil {
			return updateErr
		}

		job.Status = model.MemoryJobStatusProcessing
		job.UpdatedAt = &now
		claimed = &job
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// MarkSuccess 把任务推进为 success 最终态。
func (r *JobRepo) MarkSuccess(ctx context.Context, jobID int64) error {
	if r == nil || r.db == nil {
		return errors.New("memory job repo is nil")
	}
	now := time.Now()
	updates := map[string]any{
		"status":        model.MemoryJobStatusSuccess,
		"last_error":    nil,
		"next_retry_at": nil,
		"updated_at":    now,
	}
	return r.db.WithContext(ctx).Model(&model.MemoryJob{}).Where("id = ?", jobID).Updates(updates).Error
}

// MarkFailed 按重试策略推进任务到 failed/dead。
//
// 规则：
// 1. retry_count +1 后若超上限，直接 dead；
// 2. 未超上限则写 failed 并设置 next_retry_at。
func (r *JobRepo) MarkFailed(ctx context.Context, jobID int64, reason string) error {
	if r == nil || r.db == nil {
		return errors.New("memory job repo is nil")
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job model.MemoryJob
		queryErr := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", jobID).
			First(&job).Error
		if queryErr != nil {
			return queryErr
		}
		if job.Status == model.MemoryJobStatusSuccess || job.Status == model.MemoryJobStatusDead {
			return nil
		}

		maxRetry := job.MaxRetry
		if maxRetry <= 0 {
			maxRetry = 6
		}
		nextRetryCount := job.RetryCount + 1
		now := time.Now()
		status := model.MemoryJobStatusFailed
		var nextRetryAt *time.Time
		if nextRetryCount >= maxRetry {
			status = model.MemoryJobStatusDead
			nextRetryAt = nil
		} else {
			t := now.Add(calcRetryBackoff(nextRetryCount))
			nextRetryAt = &t
		}

		lastErr := truncateError(reason)
		updates := map[string]any{
			"status":        status,
			"retry_count":   nextRetryCount,
			"last_error":    &lastErr,
			"next_retry_at": nextRetryAt,
			"updated_at":    now,
		}
		return tx.Model(&model.MemoryJob{}).Where("id = ?", jobID).Updates(updates).Error
	})
}

func calcRetryBackoff(retryCount int) time.Duration {
	if retryCount <= 0 {
		return time.Second
	}
	if retryCount > 6 {
		retryCount = 6
	}
	return time.Second * time.Duration(1<<(retryCount-1))
}

func truncateError(reason string) string {
	if len(reason) <= 2000 {
		return reason
	}
	return reason[:2000]
}

func strPtrOrNil(v string) *string {
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
