package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
	"github.com/LoveLosita/smartflow/backend/dao"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const triggerDedupeWindow = 30 * time.Minute

// TriggerRequest 是正式主动调度触发入口的请求 DTO。
//
// 职责边界：
// 1. 负责承载 API trigger、worker due job、用户反馈归一后的触发事实；
// 2. 不承载 dry-run 结果、preview 快照或 notification provider 参数；
// 3. Payload 只保存触发来源补充信息，不能塞任意业务写库参数。
type TriggerRequest struct {
	UserID         int
	TriggerType    trigger.TriggerType
	Source         trigger.Source
	TargetType     trigger.TargetType
	TargetID       int
	FeedbackID     string
	IdempotencyKey string
	DedupeKey      string
	MockNow        *time.Time
	IsMockTime     bool
	RequestedAt    time.Time
	Payload        json.RawMessage
	JobID          *string
	TraceID        string
}

// TriggerResponse 是正式触发写入后的结果。
type TriggerResponse struct {
	TriggerID string  `json:"trigger_id"`
	Status    string  `json:"status"`
	PreviewID *string `json:"preview_id,omitempty"`
	DedupeHit bool    `json:"dedupe_hit"`
	TraceID   string  `json:"trace_id,omitempty"`
}

// TriggerService 负责写入正式 trigger 并发布 active_schedule.triggered 事件。
//
// 职责边界：
// 1. 只负责触发信号持久化、去重和事件发布；
// 2. 不执行 dry-run、不写 preview、不发飞书；
// 3. outbox 未启用时返回明确错误，避免调用方误以为正式链路已启动。
type TriggerService struct {
	activeDAO *dao.ActiveScheduleDAO
	publisher outboxinfra.EventPublisher
	clock     func() time.Time
}

func NewTriggerService(activeDAO *dao.ActiveScheduleDAO, publisher outboxinfra.EventPublisher) (*TriggerService, error) {
	if activeDAO == nil {
		return nil, errors.New("active schedule dao 不能为空")
	}
	return &TriggerService{
		activeDAO: activeDAO,
		publisher: publisher,
		clock:     time.Now,
	}, nil
}

func (s *TriggerService) SetClock(clock func() time.Time) {
	if s != nil && clock != nil {
		s.clock = clock
	}
}

// CreateAndPublish 创建正式 trigger 并发布 outbox 事件。
//
// 步骤化说明：
// 1. 先按主动调度 trigger DTO 做入口校验，确保 mock_now 不会从 worker 入口混入；
// 2. 再用 idempotency_key / dedupe_key 查询已有 trigger，命中则直接返回旧状态；
// 3. 新 trigger 先落库，再发布 outbox；发布失败会把 trigger 标记 failed，便于排障；
// 4. 返回 nil error 只表示事件已入 outbox，不表示 worker 已经生成 preview。
func (s *TriggerService) CreateAndPublish(ctx context.Context, req TriggerRequest) (*TriggerResponse, error) {
	if s == nil || s.activeDAO == nil {
		return nil, errors.New("trigger service 未初始化")
	}
	if s.publisher == nil {
		return nil, errors.New("outbox event bus 未启用，无法执行正式主动调度 trigger")
	}

	now := s.now()
	if req.RequestedAt.IsZero() {
		req.RequestedAt = now
	}
	if req.IsMockTime && req.MockNow == nil {
		return nil, errors.New("is_mock_time=true 时 mock_now 不能为空")
	}
	trig := trigger.ActiveScheduleTrigger{
		UserID:         req.UserID,
		TriggerType:    req.TriggerType,
		Source:         req.Source,
		TargetType:     req.TargetType,
		TargetID:       req.TargetID,
		FeedbackID:     req.FeedbackID,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        req.MockNow,
		IsMockTime:     req.IsMockTime,
		RequestedAt:    req.RequestedAt,
		TraceID:        firstNonEmpty(req.TraceID, fmt.Sprintf("trace_active_trigger_%d", now.UnixNano())),
	}
	if err := trig.Validate(); err != nil {
		return nil, err
	}
	if trig.Source == trigger.SourceAPIDryRun {
		return nil, errors.New("api_dry_run 不允许创建正式 trigger")
	}

	dedupeKey := strings.TrimSpace(req.DedupeKey)
	if dedupeKey == "" {
		dedupeKey = BuildTriggerDedupeKey(req.UserID, req.TriggerType, req.TargetType, req.TargetID, req.FeedbackID, req.IdempotencyKey, trig.EffectiveNow(req.RequestedAt))
	}
	if existing, ok, err := s.findExistingTrigger(ctx, req.UserID, string(req.TriggerType), req.IdempotencyKey, dedupeKey); err != nil {
		return nil, err
	} else if ok {
		return triggerResponseFromModel(existing, true), nil
	}

	payloadJSON := string(req.Payload)
	if strings.TrimSpace(payloadJSON) == "" {
		payloadJSON = "{}"
	}
	triggerID := "ast_" + uuid.NewString()
	row := &model.ActiveScheduleTrigger{
		ID:             triggerID,
		UserID:         req.UserID,
		TriggerType:    string(req.TriggerType),
		Source:         string(req.Source),
		TargetType:     string(req.TargetType),
		TargetID:       req.TargetID,
		FeedbackID:     strings.TrimSpace(req.FeedbackID),
		JobID:          req.JobID,
		IdempotencyKey: strings.TrimSpace(req.IdempotencyKey),
		DedupeKey:      dedupeKey,
		Status:         model.ActiveScheduleTriggerStatusPending,
		MockNow:        req.MockNow,
		IsMockTime:     req.IsMockTime,
		RequestedAt:    req.RequestedAt,
		PayloadJSON:    &payloadJSON,
		TraceID:        trig.TraceID,
	}
	if err := s.activeDAO.CreateTrigger(ctx, row); err != nil {
		return nil, err
	}

	eventPayload := sharedevents.ActiveScheduleTriggeredPayload{
		TriggerID:      row.ID,
		UserID:         row.UserID,
		TriggerType:    row.TriggerType,
		Source:         row.Source,
		TargetType:     row.TargetType,
		TargetID:       row.TargetID,
		FeedbackID:     row.FeedbackID,
		IdempotencyKey: row.IdempotencyKey,
		DedupeKey:      row.DedupeKey,
		MockNow:        row.MockNow,
		IsMockTime:     row.IsMockTime,
		RequestedAt:    row.RequestedAt,
		Payload:        json.RawMessage(payloadJSON),
		TraceID:        row.TraceID,
	}
	if err := eventPayload.Validate(); err != nil {
		_ = s.markTriggerFailed(ctx, row.ID, "payload_invalid", err)
		return nil, err
	}
	if err := s.publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    sharedevents.ActiveScheduleTriggeredEventType,
		EventVersion: sharedevents.ActiveScheduleTriggeredEventVersion,
		MessageKey:   eventPayload.MessageKey(),
		AggregateID:  eventPayload.AggregateID(),
		Payload:      eventPayload,
	}); err != nil {
		_ = s.markTriggerFailed(ctx, row.ID, "outbox_publish_failed", err)
		return nil, err
	}

	return triggerResponseFromModel(row, false), nil
}

func (s *TriggerService) findExistingTrigger(ctx context.Context, userID int, triggerType string, idempotencyKey string, dedupeKey string) (*model.ActiveScheduleTrigger, bool, error) {
	if strings.TrimSpace(idempotencyKey) != "" {
		existing, err := s.activeDAO.FindTriggerByIdempotencyKey(ctx, userID, triggerType, idempotencyKey)
		if err == nil {
			return existing, true, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, err
		}
	}
	statuses := []string{
		model.ActiveScheduleTriggerStatusPending,
		model.ActiveScheduleTriggerStatusProcessing,
		model.ActiveScheduleTriggerStatusPreviewGenerated,
	}
	existing, err := s.activeDAO.FindTriggerByDedupeKey(ctx, dedupeKey, statuses)
	if err == nil {
		return existing, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}
	return nil, false, nil
}

func (s *TriggerService) markTriggerFailed(ctx context.Context, triggerID string, code string, err error) error {
	message := ""
	if err != nil {
		message = err.Error()
	}
	now := s.now()
	return s.activeDAO.UpdateTriggerFields(ctx, triggerID, map[string]any{
		"status":          model.ActiveScheduleTriggerStatusFailed,
		"last_error_code": code,
		"last_error":      &message,
		"completed_at":    &now,
	})
}

func (s *TriggerService) now() time.Time {
	if s == nil || s.clock == nil {
		return time.Now()
	}
	return s.clock()
}

// BuildTriggerDedupeKey 生成正式触发去重键。
//
// 说明：
// 1. important_urgent_task 按 30 分钟窗口聚合，避免同一任务反复生成预览；
// 2. unfinished_feedback 优先使用 feedback_id/idempotency_key，不做固定时间窗强去重；
// 3. 参数非法时仍返回可读字符串，调用方会在 trigger.Validate 阶段拒绝非法输入。
func BuildTriggerDedupeKey(userID int, triggerType trigger.TriggerType, targetType trigger.TargetType, targetID int, feedbackID string, idempotencyKey string, at time.Time) string {
	if triggerType == trigger.TriggerTypeUnfinishedFeedback {
		return fmt.Sprintf("%d:%s:%s", userID, triggerType, firstNonEmpty(feedbackID, idempotencyKey, fmt.Sprintf("%s:%d", targetType, targetID)))
	}
	if at.IsZero() {
		at = time.Now()
	}
	windowStart := at.Truncate(triggerDedupeWindow)
	return fmt.Sprintf("%d:%s:%s:%d:%s", userID, triggerType, targetType, targetID, windowStart.Format(time.RFC3339))
}

func triggerResponseFromModel(row *model.ActiveScheduleTrigger, dedupeHit bool) *TriggerResponse {
	if row == nil {
		return &TriggerResponse{DedupeHit: dedupeHit}
	}
	return &TriggerResponse{
		TriggerID: row.ID,
		Status:    row.Status,
		PreviewID: row.PreviewID,
		DedupeHit: dedupeHit,
		TraceID:   row.TraceID,
	}
}
