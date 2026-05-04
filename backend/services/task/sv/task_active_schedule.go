package sv

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// syncActiveScheduleJobBestEffort 在任务变更后同步主动调度 due job。
//
// 职责边界：
// 1. 只维护 important_urgent_task 的 job，不直接触发主动调度主链路；
// 2. 任务未完成且存在 urgency_threshold_at 时 upsert pending job；
// 3. 任务已完成或阈值为空时取消当前 pending job；
// 4. 当前任务接口尚未整体事务化，job 同步失败只记日志，避免任务主写入出现“已落库但接口失败”的更差体验。
func (ts *TaskService) syncActiveScheduleJobBestEffort(ctx context.Context, task *model.Task) {
	if ts == nil || ts.activeScheduleDAO == nil || task == nil {
		return
	}
	if task.IsCompleted || task.UrgencyThresholdAt == nil {
		ts.cancelActiveScheduleJobBestEffort(ctx, task.UserID, task.ID, "task_not_schedulable")
		return
	}

	job := &model.ActiveScheduleJob{
		ID:          activeScheduleJobID(task.UserID, task.ID),
		UserID:      task.UserID,
		TaskID:      task.ID,
		TriggerType: model.ActiveScheduleTriggerTypeImportantUrgentTask,
		Status:      model.ActiveScheduleJobStatusPending,
		TriggerAt:   *task.UrgencyThresholdAt,
		DedupeKey:   activeScheduleTriggerDedupeKey(task.UserID, task.ID, *task.UrgencyThresholdAt),
		TraceID:     activeScheduleTraceID(task.UserID, task.ID),
	}
	if err := ts.activeScheduleDAO.CreateOrUpdateJob(ctx, job); err != nil {
		log.Printf("主动调度 job upsert 失败: user_id=%d task_id=%d err=%v", task.UserID, task.ID, err)
	}
}

// cancelActiveScheduleJobBestEffort 取消任务当前待触发 job。
//
// 职责边界：
// 1. 只取消 pending job，历史 triggered/skipped/failed 记录保留审计；
// 2. 找不到 pending job 属于正常幂等场景；
// 3. reason 只进入 last_error_code，方便后续排障知道取消来源。
func (ts *TaskService) cancelActiveScheduleJobBestEffort(ctx context.Context, userID int, taskID int, reason string) {
	if ts == nil || ts.activeScheduleDAO == nil || userID <= 0 || taskID <= 0 {
		return
	}
	job, err := ts.activeScheduleDAO.FindPendingJobByTask(ctx, userID, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		log.Printf("主动调度 pending job 查询失败: user_id=%d task_id=%d err=%v", userID, taskID, err)
		return
	}
	now := time.Now()
	updates := map[string]any{
		"status":          model.ActiveScheduleJobStatusCanceled,
		"last_error_code": reason,
		"last_scanned_at": &now,
	}
	if err = ts.activeScheduleDAO.UpdateJobFields(ctx, job.ID, updates); err != nil {
		log.Printf("主动调度 pending job 取消失败: user_id=%d task_id=%d job_id=%s err=%v", userID, taskID, job.ID, err)
	}
}

func activeScheduleJobID(userID int, taskID int) string {
	return fmt.Sprintf("asj_task_%d_%d", userID, taskID)
}

func activeScheduleTraceID(userID int, taskID int) string {
	return fmt.Sprintf("trace_active_task_%d_%d", userID, taskID)
}

func activeScheduleTriggerDedupeKey(userID int, taskID int, triggerAt time.Time) string {
	windowStart := triggerAt.Truncate(30 * time.Minute)
	return fmt.Sprintf("%d:%s:%s:%d:%s",
		userID,
		model.ActiveScheduleTriggerTypeImportantUrgentTask,
		model.ActiveScheduleTargetTypeTaskPool,
		taskID,
		windowStart.Format(time.RFC3339),
	)
}
