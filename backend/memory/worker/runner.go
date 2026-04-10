package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryrepo "github.com/LoveLosita/smartflow/backend/memory/repo"
	"github.com/LoveLosita/smartflow/backend/model"
)

// RunOnceResult 描述一次手工触发执行结果。
type RunOnceResult struct {
	Claimed bool
	JobID   int64
	Status  string
	Facts   int
}

// Runner 是 Day1 首版任务执行器。
//
// 职责边界：
// 1. 只负责推进 memory_jobs 状态机；
// 2. Day1 不做 memory_items 真正落库，仅做 mock 抽取与状态推进。
type Runner struct {
	jobRepo   *memoryrepo.JobRepo
	extractor Extractor
	logger    *log.Logger
}

func NewRunner(jobRepo *memoryrepo.JobRepo, extractor Extractor) *Runner {
	return &Runner{
		jobRepo:   jobRepo,
		extractor: extractor,
		logger:    log.Default(),
	}
}

// RunOnce 手工执行一次任务抢占与处理。
//
// 返回语义：
// 1. Claimed=false 表示当前无可执行任务；
// 2. Claimed=true 且 Status=success/failed/dead 表示状态已推进完成。
func (r *Runner) RunOnce(ctx context.Context) (*RunOnceResult, error) {
	if r == nil || r.jobRepo == nil || r.extractor == nil {
		return nil, errors.New("memory worker runner is not initialized")
	}

	// 1. 抢占一条可执行任务，避免并发 worker 重复处理同一记录。
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

	// 2. 解析 payload_json。解析失败属于数据质量问题，走失败重试并打日志。
	var payload memorymodel.ExtractJobPayload
	if err = json.Unmarshal([]byte(job.PayloadJSON), &payload); err != nil {
		failReason := fmt.Sprintf("解析任务载荷失败: %v", err)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		return result, nil
	}

	// 3. 调用抽取器执行 mock 抽取。Day1 先保证“能推进状态”，不引入重计算。
	facts, extractErr := r.extractor.ExtractFacts(ctx, payload)
	if extractErr != nil {
		failReason := fmt.Sprintf("抽取执行失败: %v", extractErr)
		_ = r.jobRepo.MarkFailed(ctx, job.ID, failReason)
		result.Status = model.MemoryJobStatusFailed
		return result, nil
	}

	// 4. 抽取成功后把任务置为 success。
	if err = r.jobRepo.MarkSuccess(ctx, job.ID); err != nil {
		return nil, err
	}
	result.Status = model.MemoryJobStatusSuccess
	result.Facts = len(facts)
	r.logger.Printf("memory worker run once success: job_id=%d extracted_facts=%d", job.ID, len(facts))
	return result, nil
}
