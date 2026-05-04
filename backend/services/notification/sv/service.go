package sv

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	notificationfeishu "github.com/LoveLosita/smartflow/backend/services/notification/internal/feishu"
	notificationmodel "github.com/LoveLosita/smartflow/backend/services/notification/model"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"gorm.io/gorm"
)

const (
	defaultMaxAttempts      = 5
	defaultRetryBaseDelay   = 5 * time.Minute
	defaultRetryMaxDelay    = 30 * time.Minute
	defaultSendingLease     = 10 * time.Minute
	defaultSummaryMaxRunes  = 180
	defaultRetryScanBatch   = 100
	sendingLeaseExpiredCode = "sending_lease_expired"
	defaultFallbackTemplate = "我为你生成了一份日程调整建议，请回到系统确认是否应用。"
)

// RecordStore 抽象出 notification_records 真正依赖的持久化能力。
//
// 职责边界：
// 1. 只描述 notification_records 读写所需的最小接口；
// 2. 允许生产环境直接复用 notification DAO，也允许测试时替换成内存 fake；
// 3. 不把 provider、事件总线和业务状态机耦合进存储接口。
type RecordStore interface {
	CreateNotificationRecord(ctx context.Context, record *notificationmodel.NotificationRecord) error
	UpdateNotificationRecordFields(ctx context.Context, notificationID int64, updates map[string]any) error
	GetNotificationRecordByID(ctx context.Context, notificationID int64) (*notificationmodel.NotificationRecord, error)
	FindNotificationRecordByDedupeKey(ctx context.Context, channel string, dedupeKey string) (*notificationmodel.NotificationRecord, error)
	ListRetryableNotificationRecords(ctx context.Context, now time.Time, sendingStaleBefore time.Time, limit int) ([]notificationmodel.NotificationRecord, error)
	ClaimRetryableNotificationRecord(ctx context.Context, notificationID int64, now time.Time, sendingStaleBefore time.Time) (bool, error)
}

// ChannelStore 抽象出用户通知通道配置所需的最小持久化能力。
type ChannelStore interface {
	GetUserNotificationChannel(ctx context.Context, userID int, channel string) (*notificationmodel.UserNotificationChannel, error)
	UpsertUserNotificationChannel(ctx context.Context, channel *notificationmodel.UserNotificationChannel) error
	DeleteUserNotificationChannel(ctx context.Context, userID int, channel string) error
	UpdateUserNotificationChannelTestResult(ctx context.Context, userID int, channel string, status string, testErr string, testedAt time.Time) error
}

// Service 负责 notification_records 状态机、通道配置和 provider 调用编排。
//
// 职责边界：
// 1. 负责飞书 webhook 通道配置、测试、消息投递、重试和 outbox 消费；
// 2. 不负责 active_schedule 的 dry-run / preview / trigger 状态机；
// 3. 不负责 gateway 的响应适配、路由聚合和 JWT 鉴权。
type Service struct {
	recordStore  RecordStore
	channelStore ChannelStore
	provider     notificationfeishu.Provider
	options      ServiceOptions
	locks        *keyedLocker
}

// NotificationService 是阶段四对外暴露的语义化别名。
type NotificationService = Service

// ServiceOptions 定义通知服务的可调参数。
type ServiceOptions struct {
	Now             func() time.Time
	MaxAttempts     int
	RetryBaseDelay  time.Duration
	RetryMaxDelay   time.Duration
	SendingLease    time.Duration
	SummaryMaxRunes int
	RetryScanBatch  int
}

// HandleResult 描述一次事件处理或一次 retry 尝试的结果。
type HandleResult struct {
	RecordID      int64
	Status        string
	Reused        bool
	Delivered     bool
	FallbackUsed  bool
	AttemptCount  int
	NextRetryAt   *time.Time
	ProviderError string
}

// RetryResult 汇总一次批量 retry 扫描的结果。
type RetryResult struct {
	Scanned int
	Retried int
	Sent    int
	Failed  int
	Dead    int
	Skipped int
	Errors  int
}

// NewNotificationService 创建通知服务。
func NewNotificationService(recordStore RecordStore, channelStore ChannelStore, provider notificationfeishu.Provider, opts ServiceOptions) (*Service, error) {
	if recordStore == nil {
		return nil, errors.New("notification record store is nil")
	}
	if channelStore == nil {
		return nil, errors.New("notification channel store is nil")
	}
	if provider == nil {
		return nil, errors.New("feishu provider is nil")
	}
	opts = normalizeServiceOptions(opts)
	return &Service{
		recordStore:  recordStore,
		channelStore: channelStore,
		provider:     provider,
		options:      opts,
		locks:        newKeyedLocker(),
	}, nil
}

// HandleFeishuRequested 处理一条 `notification.feishu.requested` 事件。
//
// 步骤说明：
// 1. 先校验 shared/events payload，避免脏数据进入状态机；
// 2. 再按 `channel + dedupe_key` 串行化处理，保证进程内不会并发重复发同一条飞书；
// 3. 若已有 pending/failed，则复用同一条 record 继续投递；sending/sent/dead/skipped 则直接短路。
func (s *Service) HandleFeishuRequested(ctx context.Context, payload sharedevents.FeishuNotificationRequestedPayload) (HandleResult, error) {
	if err := payload.Validate(); err != nil {
		return HandleResult{}, err
	}

	lockKey := buildNotificationLockKey(notificationfeishu.Channel, payload.DedupeKey)
	unlock := s.locks.Lock(lockKey)
	defer unlock()

	record, reused, err := s.findOrCreateRecordForPayload(ctx, payload)
	if err != nil {
		return HandleResult{}, err
	}

	result, err := s.deliverRecord(ctx, record)
	if err != nil {
		return HandleResult{}, err
	}
	result.Reused = reused
	return result, nil
}

// RetryFeishuNotifications 扫描并重试到点的 failed 记录。
//
// 步骤说明：
// 1. 先按 DAO 提供的 retry 查询口径拉取 `status=failed && next_retry_at<=now`；
// 2. 再逐条加进程内锁并复用同一条 record 重试，避免 scanner 和事件 handler 打架；
// 3. 单条失败不会中断整批扫描，但会在返回值中累计 Errors，并把首个错误回传给调用方。
func (s *Service) RetryFeishuNotifications(ctx context.Context, now time.Time, limit int) (RetryResult, error) {
	if now.IsZero() {
		now = s.options.Now()
	}
	if limit <= 0 {
		limit = s.options.RetryScanBatch
	}

	records, err := s.recordStore.ListRetryableNotificationRecords(ctx, now, s.sendingStaleBefore(now), limit)
	if err != nil {
		return RetryResult{}, err
	}

	result := RetryResult{Scanned: len(records)}
	var firstErr error

	for _, record := range records {
		if record.Channel != notificationfeishu.Channel {
			result.Skipped++
			continue
		}

		handleResult, retryErr := s.retryOneRecord(ctx, record.ID)
		if retryErr != nil {
			result.Errors++
			if firstErr == nil {
				firstErr = retryErr
			}
			continue
		}

		if handleResult.Delivered {
			result.Retried++
		}
		switch handleResult.Status {
		case notificationmodel.RecordStatusSent:
			if handleResult.Delivered {
				result.Sent++
			} else {
				result.Skipped++
			}
		case notificationmodel.RecordStatusFailed:
			result.Failed++
		case notificationmodel.RecordStatusDead:
			result.Dead++
		default:
			result.Skipped++
		}
	}

	return result, firstErr
}

func (s *Service) RetryDue(ctx context.Context, now time.Time, limit int) (int, error) {
	result, err := s.RetryFeishuNotifications(ctx, now, limit)
	if err != nil {
		return result.Retried, err
	}
	return result.Retried, nil
}

func (s *Service) retryOneRecord(ctx context.Context, notificationID int64) (HandleResult, error) {
	record, err := s.recordStore.GetNotificationRecordByID(ctx, notificationID)
	if err != nil {
		return HandleResult{}, err
	}

	lockKey := buildNotificationLockKey(record.Channel, record.DedupeKey)
	unlock := s.locks.Lock(lockKey)
	defer unlock()

	// 1. retry scanner 可能在滚动发布或多实例场景下并行运行，进程内锁只能保护当前进程。
	// 2. 这里先用条件 UPDATE 把 failed 且到期的记录 claim 成 sending；只有抢到 claim 的实例才能调用 provider。
	// 3. 未抢到说明记录已被其它实例处理或状态已变化，直接回读当前状态用于统计，不再重复发送。
	now := s.options.Now()
	claimed, err := s.recordStore.ClaimRetryableNotificationRecord(ctx, notificationID, now, s.sendingStaleBefore(now))
	if err != nil {
		return HandleResult{}, err
	}

	current, err := s.recordStore.GetNotificationRecordByID(ctx, notificationID)
	if err != nil {
		return HandleResult{}, err
	}
	if !claimed {
		return HandleResult{
			RecordID:     current.ID,
			Status:       current.Status,
			FallbackUsed: current.FallbackUsed,
			AttemptCount: current.AttemptCount,
			NextRetryAt:  current.NextRetryAt,
		}, nil
	}
	return s.sendRecordNow(ctx, current)
}

func (s *Service) findOrCreateRecordForPayload(ctx context.Context, payload sharedevents.FeishuNotificationRequestedPayload) (*notificationmodel.NotificationRecord, bool, error) {
	// 1. 若 payload 已携带 notification_id，先尝试命中现有记录，便于后续扩展“指定 record 重放”场景。
	// 2. 若 id 未命中或字段不一致，再退回到 channel + dedupe_key 这一版稳定幂等口径。
	if payload.NotificationID > 0 {
		record, err := s.recordStore.GetNotificationRecordByID(ctx, payload.NotificationID)
		if err == nil && record != nil && record.Channel == notificationfeishu.Channel && record.DedupeKey == strings.TrimSpace(payload.DedupeKey) {
			return record, true, nil
		}
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, false, err
		}
	}

	record, err := s.recordStore.FindNotificationRecordByDedupeKey(ctx, notificationfeishu.Channel, strings.TrimSpace(payload.DedupeKey))
	if err == nil {
		return record, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, err
	}

	summaryText, fallbackText, fallbackUsed := s.normalizeMessageTemplate(payload.SummaryText, payload.FallbackText)
	record = &notificationmodel.NotificationRecord{
		Channel:      notificationfeishu.Channel,
		UserID:       payload.UserID,
		TriggerID:    strings.TrimSpace(payload.TriggerID),
		PreviewID:    strings.TrimSpace(payload.PreviewID),
		TriggerType:  strings.TrimSpace(payload.TriggerType),
		TargetType:   strings.TrimSpace(payload.TargetType),
		TargetID:     payload.TargetID,
		DedupeKey:    strings.TrimSpace(payload.DedupeKey),
		TargetURL:    strings.TrimSpace(payload.TargetURL),
		SummaryText:  summaryText,
		FallbackText: fallbackText,
		FallbackUsed: fallbackUsed,
		Status:       notificationmodel.RecordStatusPending,
		MaxAttempts:  s.options.MaxAttempts,
		TraceID:      strings.TrimSpace(payload.TraceID),
	}

	if err = s.recordStore.CreateNotificationRecord(ctx, record); err != nil {
		// 1. 并发场景下若唯一索引已被别的协程抢先创建，这里回查 dedupe 记录即可；
		// 2. 若回查仍失败，说明不是幂等竞争而是真正落库异常，应交给上层重试。
		existing, findErr := s.recordStore.FindNotificationRecordByDedupeKey(ctx, notificationfeishu.Channel, record.DedupeKey)
		if findErr == nil {
			return existing, true, nil
		}
		return nil, false, err
	}
	return record, false, nil
}

func (s *Service) deliverRecord(ctx context.Context, record *notificationmodel.NotificationRecord) (HandleResult, error) {
	if record == nil {
		return HandleResult{}, errors.New("notification record is nil")
	}

	switch record.Status {
	case notificationmodel.RecordStatusSending:
		if !s.isSendingLeaseExpired(record) {
			return HandleResult{}, errors.New("notification record 正在发送中，等待租约过期后再重试")
		}
		if err := s.claimStaleSendingRecord(ctx, record); err != nil {
			return HandleResult{}, err
		}
	case notificationmodel.RecordStatusSent,
		notificationmodel.RecordStatusDead,
		notificationmodel.RecordStatusSkipped:
		return HandleResult{
			RecordID:     record.ID,
			Status:       record.Status,
			FallbackUsed: record.FallbackUsed,
			AttemptCount: record.AttemptCount,
			NextRetryAt:  record.NextRetryAt,
		}, nil
	case notificationmodel.RecordStatusPending, notificationmodel.RecordStatusFailed:
		// 继续向下走真正投递流程。
	default:
		// 1. 未识别状态先保守短路，避免把未知脏数据继续推进到 provider。
		// 2. 后续若新增新状态，应显式扩展这里的状态机分支。
		return HandleResult{
			RecordID:     record.ID,
			Status:       record.Status,
			FallbackUsed: record.FallbackUsed,
			AttemptCount: record.AttemptCount,
			NextRetryAt:  record.NextRetryAt,
		}, nil
	}

	return s.sendRecordNow(ctx, record)
}

func (s *Service) sendRecordNow(ctx context.Context, record *notificationmodel.NotificationRecord) (HandleResult, error) {
	requestPayload := s.buildSendRequest(record)
	requestJSON, err := marshalJSONPointer(requestPayload)
	if err != nil {
		return HandleResult{}, err
	}

	nextAttemptCount := record.AttemptCount + 1
	updates := map[string]any{
		"status":                notificationmodel.RecordStatusSending,
		"attempt_count":         nextAttemptCount,
		"next_retry_at":         nil,
		"last_error_code":       nil,
		"last_error":            nil,
		"provider_request_json": requestJSON,
	}
	if record.MaxAttempts <= 0 {
		updates["max_attempts"] = s.options.MaxAttempts
		record.MaxAttempts = s.options.MaxAttempts
	}
	if err = s.recordStore.UpdateNotificationRecordFields(ctx, record.ID, updates); err != nil {
		return HandleResult{}, err
	}

	record.Status = notificationmodel.RecordStatusSending
	record.AttemptCount = nextAttemptCount
	record.NextRetryAt = nil
	record.ProviderRequestJSON = requestJSON

	sendResult, sendErr := s.provider.Send(ctx, requestPayload)
	if sendErr != nil && sendResult.Outcome == "" {
		sendResult = notificationfeishu.SendResult{
			Outcome:      notificationfeishu.SendOutcomeTemporaryFail,
			ErrorCode:    notificationfeishu.ErrorCodeNetworkError,
			ErrorMessage: sendErr.Error(),
		}
	}
	if sendResult.Outcome == "" {
		sendResult.Outcome = notificationfeishu.SendOutcomeTemporaryFail
		if sendResult.ErrorCode == "" {
			sendResult.ErrorCode = notificationfeishu.ErrorCodeNetworkError
		}
		if sendResult.ErrorMessage == "" && sendErr != nil {
			sendResult.ErrorMessage = sendErr.Error()
		}
	}

	return s.applySendResult(ctx, record, sendResult)
}

func (s *Service) claimStaleSendingRecord(ctx context.Context, record *notificationmodel.NotificationRecord) error {
	now := s.options.Now()
	// 1. sending 只在超过租约后回收，避免多实例把仍在执行的 provider 调用重复发送。
	// 2. claim 使用条件 UPDATE，抢不到说明状态已被其它实例推进，本次交给 outbox/retry 下轮重试。
	// 3. 抢到后复用 sendRecordNow 重新进入统一投递状态机，不额外分叉 provider 调用路径。
	claimed, err := s.recordStore.ClaimRetryableNotificationRecord(ctx, record.ID, now, s.sendingStaleBefore(now))
	if err != nil {
		return err
	}
	if !claimed {
		return errors.New("notification record sending 租约已被其它实例处理")
	}
	record.Status = notificationmodel.RecordStatusFailed
	record.NextRetryAt = &now
	record.LastErrorCode = stringPtrOrNil(sendingLeaseExpiredCode)
	record.LastError = stringPtrOrNil("上一次发送停留在 sending，租约过期后自动恢复重试")
	return nil
}

func (s *Service) isSendingLeaseExpired(record *notificationmodel.NotificationRecord) bool {
	if record == nil || record.Status != notificationmodel.RecordStatusSending {
		return false
	}
	if record.UpdatedAt.IsZero() {
		return true
	}
	return !record.UpdatedAt.After(s.sendingStaleBefore(s.options.Now()))
}

func (s *Service) sendingStaleBefore(now time.Time) time.Time {
	if now.IsZero() {
		now = time.Now()
	}
	lease := s.options.SendingLease
	if lease <= 0 {
		lease = defaultSendingLease
	}
	return now.Add(-lease)
}

func (s *Service) applySendResult(ctx context.Context, record *notificationmodel.NotificationRecord, sendResult notificationfeishu.SendResult) (HandleResult, error) {
	now := s.options.Now()
	responseJSON, err := marshalJSONPointer(sendResult.ResponsePayload)
	if err != nil {
		return HandleResult{}, err
	}
	requestJSON, err := marshalJSONPointer(sendResult.RequestPayload)
	if err != nil {
		return HandleResult{}, err
	}
	if requestJSON == nil {
		requestJSON = record.ProviderRequestJSON
	}

	errorCode := stringPtrOrNil(sendResult.ErrorCode)
	errorMessage := stringPtrOrNil(truncateText(sendResult.ErrorMessage, 2000))
	providerMessageID := stringPtrOrNil(sendResult.ProviderMessageID)

	switch sendResult.Outcome {
	case notificationfeishu.SendOutcomeSuccess:
		sentAt := now
		updates := map[string]any{
			"status":                 notificationmodel.RecordStatusSent,
			"provider_message_id":    providerMessageID,
			"provider_request_json":  requestJSON,
			"provider_response_json": responseJSON,
			"last_error_code":        nil,
			"last_error":             nil,
			"next_retry_at":          nil,
			"sent_at":                &sentAt,
		}
		if err = s.recordStore.UpdateNotificationRecordFields(ctx, record.ID, updates); err != nil {
			return HandleResult{}, err
		}
		return HandleResult{
			RecordID:     record.ID,
			Status:       notificationmodel.RecordStatusSent,
			Delivered:    true,
			FallbackUsed: record.FallbackUsed,
			AttemptCount: record.AttemptCount,
		}, nil
	case notificationfeishu.SendOutcomeSkipped:
		updates := map[string]any{
			"status":                 notificationmodel.RecordStatusSkipped,
			"provider_message_id":    providerMessageID,
			"provider_request_json":  requestJSON,
			"provider_response_json": responseJSON,
			"last_error_code":        errorCode,
			"last_error":             errorMessage,
			"next_retry_at":          nil,
		}
		if err = s.recordStore.UpdateNotificationRecordFields(ctx, record.ID, updates); err != nil {
			return HandleResult{}, err
		}
		return HandleResult{
			RecordID:      record.ID,
			Status:        notificationmodel.RecordStatusSkipped,
			Delivered:     true,
			FallbackUsed:  record.FallbackUsed,
			AttemptCount:  record.AttemptCount,
			ProviderError: strings.TrimSpace(sendResult.ErrorCode),
		}, nil
	case notificationfeishu.SendOutcomePermanentFail:
		updates := map[string]any{
			"status":                 notificationmodel.RecordStatusDead,
			"provider_message_id":    providerMessageID,
			"provider_request_json":  requestJSON,
			"provider_response_json": responseJSON,
			"last_error_code":        errorCode,
			"last_error":             errorMessage,
			"next_retry_at":          nil,
		}
		if err = s.recordStore.UpdateNotificationRecordFields(ctx, record.ID, updates); err != nil {
			return HandleResult{}, err
		}
		return HandleResult{
			RecordID:      record.ID,
			Status:        notificationmodel.RecordStatusDead,
			Delivered:     true,
			FallbackUsed:  record.FallbackUsed,
			AttemptCount:  record.AttemptCount,
			ProviderError: strings.TrimSpace(sendResult.ErrorCode),
		}, nil
	default:
		if record.AttemptCount >= s.effectiveMaxAttempts(record) {
			updates := map[string]any{
				"status":                 notificationmodel.RecordStatusDead,
				"provider_message_id":    providerMessageID,
				"provider_request_json":  requestJSON,
				"provider_response_json": responseJSON,
				"last_error_code":        errorCode,
				"last_error":             errorMessage,
				"next_retry_at":          nil,
			}
			if err = s.recordStore.UpdateNotificationRecordFields(ctx, record.ID, updates); err != nil {
				return HandleResult{}, err
			}
			return HandleResult{
				RecordID:      record.ID,
				Status:        notificationmodel.RecordStatusDead,
				Delivered:     true,
				FallbackUsed:  record.FallbackUsed,
				AttemptCount:  record.AttemptCount,
				ProviderError: strings.TrimSpace(sendResult.ErrorCode),
			}, nil
		}

		nextRetryAt := s.calcNextRetryAt(now, record.AttemptCount)
		updates := map[string]any{
			"status":                 notificationmodel.RecordStatusFailed,
			"provider_message_id":    providerMessageID,
			"provider_request_json":  requestJSON,
			"provider_response_json": responseJSON,
			"last_error_code":        errorCode,
			"last_error":             errorMessage,
			"next_retry_at":          &nextRetryAt,
		}
		if err = s.recordStore.UpdateNotificationRecordFields(ctx, record.ID, updates); err != nil {
			return HandleResult{}, err
		}
		return HandleResult{
			RecordID:      record.ID,
			Status:        notificationmodel.RecordStatusFailed,
			Delivered:     true,
			FallbackUsed:  record.FallbackUsed,
			AttemptCount:  record.AttemptCount,
			NextRetryAt:   &nextRetryAt,
			ProviderError: strings.TrimSpace(sendResult.ErrorCode),
		}, nil
	}
}

func (s *Service) buildSendRequest(record *notificationmodel.NotificationRecord) notificationfeishu.SendRequest {
	messageText := strings.TrimSpace(record.SummaryText)
	if record.FallbackUsed || messageText == "" {
		messageText = strings.TrimSpace(record.FallbackText)
	}
	if messageText == "" {
		messageText = defaultFallbackTemplate
	}
	if !strings.Contains(messageText, strings.TrimSpace(record.TargetURL)) {
		messageText = strings.TrimSpace(messageText) + "\n" + strings.TrimSpace(record.TargetURL)
	}

	return notificationfeishu.SendRequest{
		NotificationID: record.ID,
		UserID:         record.UserID,
		TriggerID:      record.TriggerID,
		PreviewID:      record.PreviewID,
		TriggerType:    record.TriggerType,
		TargetType:     record.TargetType,
		TargetID:       record.TargetID,
		TargetURL:      record.TargetURL,
		MessageText:    strings.TrimSpace(messageText),
		FallbackUsed:   record.FallbackUsed,
		TraceID:        record.TraceID,
		AttemptCount:   record.AttemptCount + 1,
	}
}

func (s *Service) normalizeMessageTemplate(summaryText, fallbackText string) (string, string, bool) {
	normalizedFallback := strings.TrimSpace(fallbackText)
	if normalizedFallback == "" {
		normalizedFallback = defaultFallbackTemplate
	}

	normalizedSummary := strings.TrimSpace(summaryText)
	if normalizedSummary == "" {
		return "", normalizedFallback, true
	}
	if containsExternalLink(normalizedSummary) {
		return "", normalizedFallback, true
	}

	runes := []rune(normalizedSummary)
	if len(runes) > s.options.SummaryMaxRunes {
		normalizedSummary = string(runes[:s.options.SummaryMaxRunes])
	}
	return strings.TrimSpace(normalizedSummary), normalizedFallback, false
}

func (s *Service) calcNextRetryAt(now time.Time, attemptCount int) time.Time {
	if attemptCount <= 0 {
		attemptCount = 1
	}

	delay := s.options.RetryBaseDelay
	for idx := 1; idx < attemptCount; idx++ {
		delay *= 2
		if delay >= s.options.RetryMaxDelay {
			delay = s.options.RetryMaxDelay
			break
		}
	}
	if delay > s.options.RetryMaxDelay {
		delay = s.options.RetryMaxDelay
	}
	return now.Add(delay)
}

func (s *Service) effectiveMaxAttempts(record *notificationmodel.NotificationRecord) int {
	if record != nil && record.MaxAttempts > 0 {
		return record.MaxAttempts
	}
	return s.options.MaxAttempts
}

func normalizeServiceOptions(opts ServiceOptions) ServiceOptions {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	if opts.RetryBaseDelay <= 0 {
		opts.RetryBaseDelay = defaultRetryBaseDelay
	}
	if opts.RetryMaxDelay <= 0 {
		opts.RetryMaxDelay = defaultRetryMaxDelay
	}
	if opts.RetryMaxDelay < opts.RetryBaseDelay {
		opts.RetryMaxDelay = opts.RetryBaseDelay
	}
	if opts.SendingLease <= 0 {
		opts.SendingLease = defaultSendingLease
	}
	if opts.SummaryMaxRunes <= 0 {
		opts.SummaryMaxRunes = defaultSummaryMaxRunes
	}
	if opts.RetryScanBatch <= 0 {
		opts.RetryScanBatch = defaultRetryScanBatch
	}
	return opts
}

func buildNotificationLockKey(channel, dedupeKey string) string {
	return strings.TrimSpace(channel) + "|" + strings.TrimSpace(dedupeKey)
}

func marshalJSONPointer(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	text := string(raw)
	return &text, nil
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func truncateText(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit])
}

func containsExternalLink(text string) bool {
	lowered := strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(lowered, "://") || strings.Contains(lowered, "www.")
}

type keyedLocker struct {
	mu    sync.Mutex
	locks map[string]*keyedLockEntry
}

type keyedLockEntry struct {
	mu   sync.Mutex
	refs int
}

func newKeyedLocker() *keyedLocker {
	return &keyedLocker{
		locks: make(map[string]*keyedLockEntry),
	}
}

func (l *keyedLocker) Lock(key string) func() {
	l.mu.Lock()
	entry := l.locks[key]
	if entry == nil {
		entry = &keyedLockEntry{}
		l.locks[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	entry.mu.Lock()

	return func() {
		entry.mu.Unlock()
		l.mu.Lock()
		entry.refs--
		if entry.refs == 0 {
			delete(l.locks, key)
		}
		l.mu.Unlock()
	}
}
