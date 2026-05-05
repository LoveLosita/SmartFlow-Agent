package job

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/ports"
	activesvc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/service"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

const (
	defaultScanLimit = 50
)

// Scanner 扫描到期 active_schedule_jobs 并生成正式 trigger。
//
// 职责边界：
// 1. 只负责 due job -> trigger，不执行 dry-run、不写 preview、不发 notification；
// 2. 扫描时必须重读 task 与 schedule 真值，避免过期 job 误触发；
// 3. 对已完成、已排入日程或不再符合条件的 job，只更新 job 状态，不物理删除。
type Scanner struct {
	activeDAO      *dao.ActiveScheduleDAO
	taskReader     ports.TaskReader
	scheduleReader ports.ScheduleReader
	triggerService *activesvc.TriggerService
	clock          func() time.Time
	limit          int
	scanEvery      time.Duration
}

type ScannerOptions struct {
	Limit     int
	ScanEvery time.Duration
	Clock     func() time.Time
}

type ScanResult struct {
	Scanned   int
	Triggered int
	Skipped   int
	Failed    int
}

func NewScanner(activeDAO *dao.ActiveScheduleDAO, readers ports.Readers, triggerService *activesvc.TriggerService, options ScannerOptions) (*Scanner, error) {
	if activeDAO == nil {
		return nil, errors.New("active schedule dao 不能为空")
	}
	if readers.TaskReader == nil {
		return nil, errors.New("TaskReader 不能为空")
	}
	if readers.ScheduleReader == nil {
		return nil, errors.New("ScheduleReader 不能为空")
	}
	if triggerService == nil {
		return nil, errors.New("trigger service 不能为空")
	}
	limit := options.Limit
	if limit <= 0 {
		limit = defaultScanLimit
	}
	scanEvery := options.ScanEvery
	if scanEvery <= 0 {
		scanEvery = time.Minute
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	return &Scanner{
		activeDAO:      activeDAO,
		taskReader:     readers.TaskReader,
		scheduleReader: readers.ScheduleReader,
		triggerService: triggerService,
		clock:          clock,
		limit:          limit,
		scanEvery:      scanEvery,
	}, nil
}

// Start 启动 due job 周期扫描。
//
// 说明：
// 1. worker/all 模式调用；api 模式不启动，避免 API 进程承担后台职责；
// 2. 每轮扫描失败只记录日志，下一轮继续；
// 3. ctx 取消后 goroutine 自然退出。
func (s *Scanner) Start(ctx context.Context) {
	if s == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(s.scanEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				result, err := s.ScanDue(ctx, s.now())
				if err != nil {
					log.Printf("主动调度 due job 扫描失败: err=%v", err)
					continue
				}
				if result.Scanned > 0 {
					log.Printf("主动调度 due job 扫描完成: scanned=%d triggered=%d skipped=%d failed=%d", result.Scanned, result.Triggered, result.Skipped, result.Failed)
				}
			}
		}
	}()
}

// ScanDue 扫描并处理一批到期 job。
func (s *Scanner) ScanDue(ctx context.Context, now time.Time) (ScanResult, error) {
	if s == nil || s.activeDAO == nil {
		return ScanResult{}, errors.New("scanner 未初始化")
	}
	jobs, err := s.activeDAO.ListDueJobs(ctx, now, s.limit)
	if err != nil {
		return ScanResult{}, err
	}
	result := ScanResult{Scanned: len(jobs)}
	for _, item := range jobs {
		handled, handleErr := s.processJob(ctx, item, now)
		switch {
		case handleErr != nil:
			result.Failed++
			log.Printf("主动调度 due job 处理失败: job_id=%s err=%v", item.ID, handleErr)
		case handled == model.ActiveScheduleJobStatusTriggered:
			result.Triggered++
		default:
			result.Skipped++
		}
	}
	return result, nil
}

func (s *Scanner) processJob(ctx context.Context, item model.ActiveScheduleJob, now time.Time) (string, error) {
	task, found, err := s.taskReader.GetTaskForActiveSchedule(ctx, ports.TaskRequest{
		UserID: item.UserID,
		TaskID: item.TaskID,
		Now:    now,
	})
	if err != nil {
		_ = s.markJobFailed(ctx, item.ID, "task_read_failed", err, now)
		return "", err
	}
	if !found {
		return model.ActiveScheduleJobStatusSkipped, s.markJobSkipped(ctx, item.ID, model.ActiveScheduleJobStatusSkipped, "task_not_found", now)
	}
	if task.IsCompleted {
		return model.ActiveScheduleJobStatusCanceled, s.markJobSkipped(ctx, item.ID, model.ActiveScheduleJobStatusCanceled, "task_completed", now)
	}
	if task.UrgencyThresholdAt == nil {
		// 1. 到期扫描必须重读 task 真值。
		// 2. 若上游已经移除了 urgency_threshold_at，说明这条 due job 已经不再具备触发前提。
		// 3. 这里直接收敛为 canceled，避免继续错误地产生 trigger。
		return model.ActiveScheduleJobStatusCanceled, s.markJobSkipped(ctx, item.ID, model.ActiveScheduleJobStatusCanceled, "task_not_schedulable", now)
	}
	if task.UrgencyThresholdAt != nil && task.UrgencyThresholdAt.After(now) {
		return model.ActiveScheduleJobStatusPending, s.activeDAO.UpdateJobFields(ctx, item.ID, map[string]any{
			"trigger_at":      *task.UrgencyThresholdAt,
			"last_error_code": "threshold_moved_future",
			"last_scanned_at": &now,
		})
	}
	if task.Priority != 1 && task.Priority != 2 {
		return model.ActiveScheduleJobStatusSkipped, s.markJobSkipped(ctx, item.ID, model.ActiveScheduleJobStatusSkipped, "task_not_important", now)
	}
	alreadyScheduled, err := s.isTaskAlreadyScheduled(ctx, item.UserID, item.TaskID, now)
	if err != nil {
		_ = s.markJobFailed(ctx, item.ID, "schedule_read_failed", err, now)
		return "", err
	}
	if alreadyScheduled {
		return model.ActiveScheduleJobStatusSkipped, s.markJobSkipped(ctx, item.ID, model.ActiveScheduleJobStatusSkipped, "task_already_scheduled", now)
	}

	payload := struct {
		JobID              string    `json:"job_id"`
		UrgencyThresholdAt time.Time `json:"urgency_threshold_at"`
	}{
		JobID:              item.ID,
		UrgencyThresholdAt: item.TriggerAt,
	}
	rawPayload, _ := json.Marshal(payload)
	jobID := item.ID
	resp, err := s.triggerService.CreateAndPublish(ctx, activesvc.TriggerRequest{
		UserID:      item.UserID,
		TriggerType: trigger.TriggerTypeImportantUrgentTask,
		Source:      trigger.SourceWorkerDueJob,
		TargetType:  trigger.TargetTypeTaskPool,
		TargetID:    item.TaskID,
		DedupeKey:   item.DedupeKey,
		RequestedAt: now,
		Payload:     rawPayload,
		JobID:       &jobID,
		TraceID:     firstNonEmpty(item.TraceID, fmt.Sprintf("trace_active_job_%s", item.ID)),
	})
	if err != nil {
		_ = s.markJobFailed(ctx, item.ID, "trigger_publish_failed", err, now)
		return "", err
	}
	return model.ActiveScheduleJobStatusTriggered, s.activeDAO.UpdateJobFields(ctx, item.ID, map[string]any{
		"status":          model.ActiveScheduleJobStatusTriggered,
		"last_trigger_id": &resp.TriggerID,
		"last_error_code": nil,
		"last_error":      nil,
		"last_scanned_at": &now,
	})
}

func (s *Scanner) isTaskAlreadyScheduled(ctx context.Context, userID int, taskID int, now time.Time) (bool, error) {
	facts, err := s.scheduleReader.GetScheduleFactsByWindow(ctx, ports.ScheduleWindowRequest{
		UserID:      userID,
		TargetType:  string(trigger.TargetTypeTaskPool),
		TargetID:    taskID,
		WindowStart: now,
		WindowEnd:   now.Add(24 * time.Hour),
		Now:         now,
	})
	if err != nil {
		return false, err
	}
	return facts.TargetAlreadyScheduled, nil
}

func (s *Scanner) markJobSkipped(ctx context.Context, jobID string, status string, code string, now time.Time) error {
	return s.activeDAO.UpdateJobFields(ctx, jobID, map[string]any{
		"status":          status,
		"last_error_code": code,
		"last_error":      nil,
		"last_scanned_at": &now,
	})
}

func (s *Scanner) markJobFailed(ctx context.Context, jobID string, code string, err error, now time.Time) error {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return s.activeDAO.UpdateJobFields(ctx, jobID, map[string]any{
		"status":          model.ActiveScheduleJobStatusFailed,
		"last_error_code": code,
		"last_error":      &message,
		"last_scanned_at": &now,
	})
}

func (s *Scanner) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
