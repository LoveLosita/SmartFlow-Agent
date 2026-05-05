package sv

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
)

// OutboxBus 是 notification 服务注册消费 handler 需要的最小总线接口。
//
// 职责边界：只要求具备 handler 注册能力，启动、关闭和发布由进程入口自己编排。
type OutboxBus interface {
	RegisterEventHandler(eventType string, handler outboxinfra.MessageHandler) error
}

// RegisterFeishuRequestedHandler 注册 `notification.feishu.requested` 消费 handler。
//
// 职责边界：
// 1. 只负责事件解析、协议校验、调用 NotificationService 和推进 outbox consumed；
// 2. 不承担 notification_records 状态机细节，状态流转全部下沉到 notification 服务；
// 3. 不在 handler 内部创建 provider/service，避免事件消费与 retry loop 使用两套不同配置。
func RegisterFeishuRequestedHandler(bus OutboxBus, outboxRepo *outboxinfra.Repository, svc *Service) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if svc == nil {
		return errors.New("notification service is nil")
	}
	if err := outboxinfra.RegisterEventService(sharedevents.NotificationFeishuRequestedEventType, outboxinfra.ServiceNotification); err != nil {
		return err
	}
	route, ok := outboxinfra.ResolveEventRoute(sharedevents.NotificationFeishuRequestedEventType)
	if !ok {
		return errors.New("notification.feishu.requested route is missing")
	}
	eventOutboxRepo := outboxRepo.WithRoute(route)

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		// 1. 先校验 event_version，避免未来协议破坏性升级后旧 handler 误吃新消息。
		// 2. 当前阶段只接受 v1；版本不匹配属于不可恢复协议错误，直接标记 dead。
		eventVersion := strings.TrimSpace(envelope.EventVersion)
		if eventVersion != "" && eventVersion != sharedevents.NotificationFeishuRequestedEventVersion {
			if err := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "notification.feishu.requested event_version 不匹配: "+eventVersion); err != nil {
				return err
			}
			return nil
		}

		var payload sharedevents.FeishuNotificationRequestedPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			if err := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析 notification.feishu.requested 载荷失败: "+unmarshalErr.Error()); err != nil {
				return err
			}
			return nil
		}
		if validateErr := payload.Validate(); validateErr != nil {
			if err := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "notification.feishu.requested 载荷非法: "+validateErr.Error()); err != nil {
				return err
			}
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
