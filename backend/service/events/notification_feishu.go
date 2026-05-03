package events

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/notification"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
)

// RegisterFeishuNotificationHandler 注册 `notification.feishu.requested` 消费 handler。
//
// 职责边界：
// 1. 只负责事件解析、协议校验、调用 NotificationService 和推进 outbox consumed；
// 2. 不承担 notification_records 状态机细节，状态流转全部下沉到 notification 模块；
// 3. 不在 handler 内部创建 provider/service，避免事件消费与 retry loop 使用两套不同配置。
func RegisterFeishuNotificationHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	svc *notification.NotificationService,
) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if svc == nil {
		return errors.New("notification service is nil")
	}
	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, sharedevents.NotificationFeishuRequestedEventType)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		// 1. 先校验 event_version，避免未来协议破坏性升级后旧 handler 误吃新消息。
		// 2. 当前阶段只接受 v1；版本不匹配属于不可恢复协议错误，直接标记 dead。
		eventVersion := strings.TrimSpace(envelope.EventVersion)
		if eventVersion != "" && eventVersion != sharedevents.NotificationFeishuRequestedEventVersion {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "notification.feishu.requested event_version 不匹配: "+eventVersion)
			return nil
		}

		var payload sharedevents.FeishuNotificationRequestedPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析 notification.feishu.requested 载荷失败: "+unmarshalErr.Error())
			return nil
		}
		if validateErr := payload.Validate(); validateErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "notification.feishu.requested 载荷非法: "+validateErr.Error())
			return nil
		}

		result, handleErr := svc.HandleFeishuRequested(ctx, payload)
		if handleErr != nil {
			return handleErr
		}

		if consumeErr := eventOutboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, nil); consumeErr != nil {
			return consumeErr
		}

		log.Printf(
			"notification.feishu.requested 消费完成: outbox_id=%d notification_id=%d status=%s delivered=%t reused=%t attempt_count=%d",
			envelope.OutboxID,
			result.RecordID,
			result.Status,
			result.Delivered,
			result.Reused,
			result.AttemptCount,
		)
		return nil
	}

	return bus.RegisterEventHandler(sharedevents.NotificationFeishuRequestedEventType, handler)
}

// PublishFeishuNotificationRequested 发布 `notification.feishu.requested` 事件。
//
// 职责边界：
// 1. 只负责把 shared/events payload 投递到 outbox；
// 2. 不等待 provider 结果，也不提前创建 notification_records；
// 3. 供主动调度 preview 阶段后续切入通知时直接复用。
func PublishFeishuNotificationRequested(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	payload sharedevents.FeishuNotificationRequestedPayload,
) error {
	if publisher == nil {
		return errors.New("event publisher is nil")
	}
	if err := payload.Validate(); err != nil {
		return err
	}

	return publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    sharedevents.NotificationFeishuRequestedEventType,
		EventVersion: sharedevents.NotificationFeishuRequestedEventVersion,
		MessageKey:   payload.MessageKey(),
		AggregateID:  payload.AggregateID(),
		Payload:      payload,
	})
}
