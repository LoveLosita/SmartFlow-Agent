package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
)

// EnqueueActiveScheduleTriggeredInTx 在事务内写入 active_schedule.triggered outbox 消息。
//
// 职责边界：
// 1. 只负责把已经校验好的事件契约写入 outbox；
// 2. 不负责创建 trigger 记录，trigger 真值应由调用方先落库；
// 3. 失败时返回 error，让上层决定是否整体回滚与重试。
func EnqueueActiveScheduleTriggeredInTx(
	ctx context.Context,
	outboxRepo *outboxinfra.Repository,
	maxRetry int,
	payload sharedevents.ActiveScheduleTriggeredPayload,
) error {
	return enqueueContractEventInTx(
		ctx,
		outboxRepo,
		maxRetry,
		sharedevents.ActiveScheduleTriggeredEventType,
		sharedevents.ActiveScheduleTriggeredEventVersion,
		payload.MessageKey(),
		payload.AggregateID(),
		payload.AggregateID(),
		payload,
		payload.Validate,
	)
}

// EnqueueNotificationFeishuRequestedInTx 在事务内写入 notification.feishu.requested outbox 消息。
//
// 职责边界：
// 1. 只做事件契约序列化和 outbox 入队；
// 2. 不负责 notification_records 幂等与 provider 调用；
// 3. 失败时直接返回，让 trigger -> preview -> notification 保持同事务回滚。
func EnqueueNotificationFeishuRequestedInTx(
	ctx context.Context,
	outboxRepo *outboxinfra.Repository,
	maxRetry int,
	payload sharedevents.FeishuNotificationRequestedPayload,
) error {
	if err := ensureNotificationFeishuOutboxRoute(); err != nil {
		return err
	}
	return enqueueContractEventInTx(
		ctx,
		outboxRepo,
		maxRetry,
		sharedevents.NotificationFeishuRequestedEventType,
		sharedevents.NotificationFeishuRequestedEventVersion,
		payload.MessageKey(),
		payload.AggregateID(),
		payload.AggregateID(),
		payload,
		payload.Validate,
	)
}

// BuildTriggeredPayloadFromModel 把持久化 trigger 还原成事件载荷。
//
// 职责边界：
// 1. 只做 model -> contract DTO 映射；
// 2. 不校验 trigger 是否应该被处理，业务真值判断由 scanner / worker 完成；
// 3. 若 payload_json 不是合法 JSON，返回 error，让调用方回滚本次触发。
// ensureNotificationFeishuOutboxRoute 确保 publisher 侧能把飞书通知事件写入 notification outbox。
//
// 职责边界：
// 1. 这里只登记 event_type -> notification 服务归属，不注册 handler，也不启动单体旧消费者；
// 2. RegisterEventService 本身幂等，重复调用用于覆盖 API/worker 不同启动路径；
// 3. 若路由登记失败，直接返回给事务调用方，让 trigger 与 notification 入队一起回滚。
func ensureNotificationFeishuOutboxRoute() error {
	return outboxinfra.RegisterEventService(sharedevents.NotificationFeishuRequestedEventType, outboxinfra.ServiceNotification)
}

func BuildTriggeredPayloadFromModel(row model.ActiveScheduleTrigger) (sharedevents.ActiveScheduleTriggeredPayload, error) {
	var rawPayload json.RawMessage
	if row.PayloadJSON != nil && strings.TrimSpace(*row.PayloadJSON) != "" {
		rawPayload = json.RawMessage(strings.TrimSpace(*row.PayloadJSON))
		if !json.Valid(rawPayload) {
			return sharedevents.ActiveScheduleTriggeredPayload{}, errors.New("trigger payload_json 不是合法 JSON")
		}
	}

	payload := sharedevents.ActiveScheduleTriggeredPayload{
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
		Payload:        rawPayload,
		TraceID:        row.TraceID,
	}
	if err := payload.Validate(); err != nil {
		return sharedevents.ActiveScheduleTriggeredPayload{}, err
	}
	return payload, nil
}

// BuildFeishuRequestedPayload 生成通知事件载荷。
//
// 职责边界：
// 1. 只做 trigger/preview 快照到通知契约的拼装；
// 2. 不判断是否真的要发通知，上层应先根据 decision.ShouldNotify 决定是否调用；
// 3. fallback 文案只做兜底，不替代后续 notification handler 的 provider 级策略。
func BuildFeishuRequestedPayload(
	triggerRow model.ActiveScheduleTrigger,
	previewID string,
	notificationSummary string,
	requestedAt time.Time,
) sharedevents.FeishuNotificationRequestedPayload {
	summary := strings.TrimSpace(notificationSummary)
	targetURL := fmt.Sprintf("/assistant/%s", buildActiveScheduleConversationID(triggerRow.ID))
	return sharedevents.FeishuNotificationRequestedPayload{
		UserID:       triggerRow.UserID,
		TriggerID:    triggerRow.ID,
		PreviewID:    strings.TrimSpace(previewID),
		TriggerType:  triggerRow.TriggerType,
		TargetType:   triggerRow.TargetType,
		TargetID:     triggerRow.TargetID,
		DedupeKey:    BuildNotificationDedupeKey(triggerRow.UserID, triggerRow.TriggerType, triggerRow.RequestedAt),
		TargetURL:    targetURL,
		SummaryText:  summary,
		FallbackText: buildNotificationFallbackText(summary, targetURL),
		TraceID:      triggerRow.TraceID,
		RequestedAt:  requestedAt,
	}
}

// BuildNotificationDedupeKey 生成通知 30 分钟窗口去重键。
//
// 说明：
// 1. 第一版按 user_id + trigger_type + time_window 聚合；
// 2. 当 requested_at 缺失时回退到当前时间，避免空值直接写出脏 dedupe_key；
// 3. 不拼 preview_id，保证同一窗口内多次重试只会落到同一组通知记录。
func BuildNotificationDedupeKey(userID int, triggerType string, requestedAt time.Time) string {
	if requestedAt.IsZero() {
		requestedAt = time.Now()
	}
	return sharedevents.BuildFeishuNotificationDedupeKey(userID, triggerType, requestedAt, sharedevents.DefaultFeishuNotificationDedupeWindow)
}

func enqueueContractEventInTx(
	ctx context.Context,
	outboxRepo *outboxinfra.Repository,
	maxRetry int,
	eventType string,
	eventVersion string,
	messageKey string,
	aggregateID string,
	eventID string,
	payload any,
	validate func() error,
) error {
	if outboxRepo == nil {
		return errors.New("outbox repository 不能为空")
	}
	if validate == nil {
		return errors.New("事件校验函数不能为空")
	}
	if err := validate(); err != nil {
		return err
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if maxRetry <= 0 {
		maxRetry = 20
	}

	wrapped := outboxinfra.OutboxEventPayload{
		EventID:      strings.TrimSpace(eventID),
		EventType:    eventType,
		EventVersion: strings.TrimSpace(eventVersion),
		AggregateID:  strings.TrimSpace(aggregateID),
		Payload:      payloadJSON,
	}
	// 1. 这里只负责把已经校验过的事件契约写入 outbox；具体 service/table/topic 由仓库按 eventType 解析。
	// 2. 这样 active scheduler 侧不再显式依赖 topic，后续切服务级路由时只需要维护事件归属表。
	_, err = outboxRepo.CreateMessage(ctx, eventType, strings.TrimSpace(messageKey), wrapped, maxRetry)
	return err
}

func buildNotificationFallbackText(summary string, targetURL string) string {
	link := strings.TrimSpace(targetURL)
	if summary == "" {
		return "你有一条新的日程调整建议，请查看：" + link
	}
	return summary + "，请查看：" + link
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
