package service

import (
	"context"
	"errors"

	memoryrepo "github.com/LoveLosita/smartflow/backend/services/memory/internal/repo"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
)

// EnqueueService 是 Day1 的“任务入队门面”。
//
// 职责边界：
// 1. 只负责把抽取请求入 memory_jobs；
// 2. 不负责执行抽取、不负责写 memory_items。
type EnqueueService struct {
	jobRepo *memoryrepo.JobRepo
}

func NewEnqueueService(jobRepo *memoryrepo.JobRepo) *EnqueueService {
	return &EnqueueService{jobRepo: jobRepo}
}

func (s *EnqueueService) EnqueueExtractJob(
	ctx context.Context,
	payload memorymodel.ExtractJobPayload,
	sourceEventID string,
) error {
	if s == nil || s.jobRepo == nil {
		return errors.New("memory enqueue service is nil")
	}
	return s.jobRepo.CreatePendingExtractJob(ctx, payload, sourceEventID)
}
