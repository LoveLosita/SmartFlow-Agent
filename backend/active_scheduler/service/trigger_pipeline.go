package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	activepreview "github.com/LoveLosita/smartflow/backend/active_scheduler/preview"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	triggerErrorCodePayloadMismatch = "payload_mismatch"
	triggerErrorCodeWorkerFailed    = "worker_failed"
)

// TriggerWorkflowService 负责第四阶段的 trigger -> dry-run -> preview -> notification 编排。
//
// 职责边界：
// 1. 只推进主动调度 trigger 的后台状态机，不负责启动 outbox worker；
// 2. dry-run 与 preview 复用现有 service，不再单独实现第二套候选生成逻辑；
// 3. notification 只发布 requested 事件，不直接接真实飞书 provider。
type TriggerWorkflowService struct {
	activeDAO *dao.ActiveScheduleDAO
	dryRun    *DryRunService
	outbox    *outboxinfra.Repository
	kafkaCfg  kafkabus.Config
	clock     func() time.Time
}

func NewTriggerWorkflowService(
	activeDAO *dao.ActiveScheduleDAO,
	dryRun *DryRunService,
	outboxRepo *outboxinfra.Repository,
	kafkaCfg kafkabus.Config,
) (*TriggerWorkflowService, error) {
	if activeDAO == nil {
		return nil, errors.New("active schedule dao 不能为空")
	}
	if dryRun == nil {
		return nil, errors.New("dry-run service 不能为空")
	}
	if outboxRepo == nil {
		return nil, errors.New("outbox repository 不能为空")
	}
	return &TriggerWorkflowService{
		activeDAO: activeDAO,
		dryRun:    dryRun,
		outbox:    outboxRepo,
		kafkaCfg:  kafkaCfg,
		clock:     time.Now,
	}, nil
}

func (s *TriggerWorkflowService) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

// ProcessTriggeredInTx 在 outbox 消费事务内推进 trigger 主链路。
//
// 步骤化说明：
// 1. 先锁 trigger 行，确保同一 trigger 在并发 worker 下只能由一个事务推进；
// 2. 再把状态切到 processing，避免排障时看不出消息已经被消费；
// 3. 复用 dry-run + preview service 生成预览；若发现已有 preview，则直接复用，避免重复写库；
// 4. preview 成功后回写 trigger 状态，并在同一事务里补发 notification.requested outbox；
// 5. 任一步失败都返回 error，由外层 handler 负责记录 failed 状态并触发 outbox retry。
func (s *TriggerWorkflowService) ProcessTriggeredInTx(
	ctx context.Context,
	tx *gorm.DB,
	payload sharedevents.ActiveScheduleTriggeredPayload,
) error {
	if s == nil || s.activeDAO == nil || s.dryRun == nil || s.outbox == nil {
		return errors.New("trigger workflow service 未初始化")
	}
	if tx == nil {
		return errors.New("gorm tx 不能为空")
	}
	if err := payload.Validate(); err != nil {
		return err
	}

	now := s.now()
	triggerRow, err := s.lockTrigger(ctx, tx, payload.TriggerID)
	if err != nil {
		return err
	}

	txDAO := s.activeDAO.WithTx(tx)
	if completed, err := s.tryFinishByTerminalStatus(ctx, txDAO, *triggerRow); err != nil || completed {
		return err
	}
	if handled, err := s.tryRejectMismatchedPayload(ctx, txDAO, *triggerRow, payload, now); err != nil || handled {
		return err
	}

	if err := txDAO.UpdateTriggerFields(ctx, triggerRow.ID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusProcessing,
		"processed_at":    &now,
		"last_error_code": nil,
		"last_error":      nil,
	}); err != nil {
		return err
	}

	existingPreview, err := txDAO.GetPreviewByTriggerID(ctx, triggerRow.ID)
	switch {
	case err == nil:
		return s.finishWithExistingPreview(ctx, txDAO, *triggerRow, *existingPreview, now)
	case errors.Is(err, gorm.ErrRecordNotFound):
		// 继续创建新 preview。
	default:
		return err
	}

	domainTrigger := buildDomainTriggerFromModel(*triggerRow, payload)
	dryRunResult, err := s.dryRun.DryRun(ctx, domainTrigger)
	if err != nil {
		return err
	}
	if len(dryRunResult.Candidates) == 0 {
		return s.markClosedWithoutPreview(ctx, txDAO, triggerRow.ID, now)
	}
	if !dryRunResult.Observation.Decision.ShouldNotify && !dryRunResult.Observation.Decision.ShouldWritePreview {
		return s.markClosedWithoutPreview(ctx, txDAO, triggerRow.ID, now)
	}

	previewService, err := activepreview.NewService(txDAO)
	if err != nil {
		return err
	}
	previewResp, err := previewService.CreatePreview(ctx, activepreview.CreatePreviewRequest{
		ActiveContext: dryRunResult.Context,
		Observation:   dryRunResult.Observation,
		Candidates:    dryRunResult.Candidates,
		TriggerID:     triggerRow.ID,
		GeneratedAt:   now,
	})
	if err != nil {
		return err
	}

	previewID := previewResp.Detail.PreviewID
	if err = txDAO.UpdateTriggerFields(ctx, triggerRow.ID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusPreviewGenerated,
		"preview_id":      &previewID,
		"completed_at":    &now,
		"last_error_code": nil,
		"last_error":      nil,
	}); err != nil {
		return err
	}

	if !dryRunResult.Observation.Decision.ShouldNotify {
		return nil
	}

	notificationPayload := BuildFeishuRequestedPayload(
		*triggerRow,
		previewID,
		previewResp.Detail.Notification,
		now,
	)
	return EnqueueNotificationFeishuRequestedInTx(ctx, s.outbox.WithTx(tx), s.kafkaCfg, notificationPayload)
}

// MarkTriggerFailedBestEffort 在事务外补记 trigger failed 状态，供 outbox retry 前排障。
//
// 职责边界：
// 1. 只做 best-effort 状态回写，不能影响外层对原始错误的返回；
// 2. 不负责错误分类，当前统一记为 worker_failed；
// 3. 失败时静默返回，让真正的重试仍由 outbox 状态机负责。
func (s *TriggerWorkflowService) MarkTriggerFailedBestEffort(ctx context.Context, triggerID string, err error) {
	if s == nil || s.activeDAO == nil || strings.TrimSpace(triggerID) == "" {
		return
	}
	message := ""
	if err != nil {
		message = err.Error()
	}
	_ = s.activeDAO.UpdateTriggerFields(ctx, triggerID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusFailed,
		"last_error_code": triggerErrorCodeWorkerFailed,
		"last_error":      &message,
	})
}

func (s *TriggerWorkflowService) lockTrigger(ctx context.Context, tx *gorm.DB, triggerID string) (*model.ActiveScheduleTrigger, error) {
	var row model.ActiveScheduleTrigger
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", triggerID).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *TriggerWorkflowService) tryFinishByTerminalStatus(
	ctx context.Context,
	txDAO *dao.ActiveScheduleDAO,
	row model.ActiveScheduleTrigger,
) (bool, error) {
	switch row.Status {
	case model.ActiveScheduleTriggerStatusPreviewGenerated,
		model.ActiveScheduleTriggerStatusClosed,
		model.ActiveScheduleTriggerStatusSkipped,
		model.ActiveScheduleTriggerStatusRejected:
		return true, nil
	case model.ActiveScheduleTriggerStatusPending,
		model.ActiveScheduleTriggerStatusProcessing,
		model.ActiveScheduleTriggerStatusFailed:
		return false, nil
	default:
		// 1. 遇到未知状态时，不直接报错中断，而是继续按 processing 流程推进。
		// 2. 这样可以兼容迁移期历史脏数据，避免单条异常阻塞整批消费。
		// 3. 真实状态最终会被下面的 UpdateTriggerFields 覆盖为 processing。
		return false, nil
	}
}

func (s *TriggerWorkflowService) tryRejectMismatchedPayload(
	ctx context.Context,
	txDAO *dao.ActiveScheduleDAO,
	row model.ActiveScheduleTrigger,
	payload sharedevents.ActiveScheduleTriggeredPayload,
	now time.Time,
) (bool, error) {
	mismatchReason := buildPayloadMismatchReason(row, payload)
	if mismatchReason == "" {
		return false, nil
	}
	if err := txDAO.UpdateTriggerFields(ctx, row.ID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusRejected,
		"last_error_code": triggerErrorCodePayloadMismatch,
		"last_error":      &mismatchReason,
		"completed_at":    &now,
	}); err != nil {
		return false, err
	}
	return true, nil
}

func (s *TriggerWorkflowService) finishWithExistingPreview(
	ctx context.Context,
	txDAO *dao.ActiveScheduleDAO,
	triggerRow model.ActiveScheduleTrigger,
	previewRow model.ActiveSchedulePreview,
	now time.Time,
) error {
	previewID := previewRow.ID
	return txDAO.UpdateTriggerFields(ctx, triggerRow.ID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusPreviewGenerated,
		"preview_id":      &previewID,
		"completed_at":    &now,
		"last_error_code": nil,
		"last_error":      nil,
	})
}

func (s *TriggerWorkflowService) markClosedWithoutPreview(
	ctx context.Context,
	txDAO *dao.ActiveScheduleDAO,
	triggerID string,
	now time.Time,
) error {
	return txDAO.UpdateTriggerFields(ctx, triggerID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusClosed,
		"completed_at":    &now,
		"last_error_code": nil,
		"last_error":      nil,
	})
}

func (s *TriggerWorkflowService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

func buildDomainTriggerFromModel(
	row model.ActiveScheduleTrigger,
	payload sharedevents.ActiveScheduleTriggeredPayload,
) trigger.ActiveScheduleTrigger {
	mockNow := row.MockNow
	if mockNow == nil && payload.MockNow != nil {
		mockNow = payload.MockNow
	}
	traceID := strings.TrimSpace(row.TraceID)
	if traceID == "" {
		traceID = strings.TrimSpace(payload.TraceID)
	}
	if traceID == "" {
		traceID = "trace_active_trigger_" + uuid.NewString()
	}
	return trigger.ActiveScheduleTrigger{
		TriggerID:      row.ID,
		UserID:         row.UserID,
		TriggerType:    trigger.TriggerType(row.TriggerType),
		Source:         trigger.Source(row.Source),
		TargetType:     trigger.TargetType(row.TargetType),
		TargetID:       row.TargetID,
		FeedbackID:     row.FeedbackID,
		IdempotencyKey: row.IdempotencyKey,
		MockNow:        mockNow,
		IsMockTime:     row.IsMockTime || payload.IsMockTime,
		RequestedAt:    row.RequestedAt,
		TraceID:        traceID,
	}
}

func buildPayloadMismatchReason(row model.ActiveScheduleTrigger, payload sharedevents.ActiveScheduleTriggeredPayload) string {
	switch {
	case row.UserID != payload.UserID:
		return fmt.Sprintf("trigger 事件 user_id 不一致: row=%d payload=%d", row.UserID, payload.UserID)
	case row.TriggerType != payload.TriggerType:
		return fmt.Sprintf("trigger 事件 trigger_type 不一致: row=%s payload=%s", row.TriggerType, payload.TriggerType)
	case row.TargetType != payload.TargetType:
		return fmt.Sprintf("trigger 事件 target_type 不一致: row=%s payload=%s", row.TargetType, payload.TargetType)
	case row.TargetID != payload.TargetID:
		return fmt.Sprintf("trigger 事件 target_id 不一致: row=%d payload=%d", row.TargetID, payload.TargetID)
	default:
		return ""
	}
}
