package sv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
)

// RegisterCreditChargeRoutes 只登记 token-store 负责消费的 Credit 扣费事件归属。
func RegisterCreditChargeRoutes() error {
	return outboxinfra.RegisterEventService(sharedevents.CreditChargeRequestedEventType, outboxinfra.ServiceTokenStore)
}

// RegisterCreditChargeHandlers 注册 token-store 对 Credit 扣费事件的消费处理器。
func RegisterCreditChargeHandlers(bus OutboxBus, outboxRepo *outboxinfra.Repository, svc *Service) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if svc == nil {
		return errors.New("tokenstore service is nil")
	}
	if err := RegisterCreditChargeRoutes(); err != nil {
		return err
	}

	route, ok := outboxinfra.ResolveEventRoute(sharedevents.CreditChargeRequestedEventType)
	if !ok {
		return fmt.Errorf("credit charge outbox route is missing: eventType=%s", sharedevents.CreditChargeRequestedEventType)
	}
	eventOutboxRepo := outboxRepo.WithRoute(route)

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		if !isAllowedCreditChargeEventVersion(envelope.EventVersion) {
			if err := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("Credit 扣费事件版本不受支持: %s", envelope.EventVersion)); err != nil {
				return err
			}
			return nil
		}

		var payload sharedevents.CreditChargeRequestedPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析 Credit 扣费载荷失败: "+err.Error()); markErr != nil {
				return markErr
			}
			return nil
		}
		if strings.TrimSpace(payload.EventID) == "" {
			payload.EventID = strings.TrimSpace(envelope.EventID)
		}
		if err := payload.Validate(); err != nil {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "Credit 扣费载荷非法: "+err.Error()); markErr != nil {
				return markErr
			}
			return nil
		}
		if payload.EventType() != sharedevents.CreditChargeRequestedEventType {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("Credit 扣费事件类型不匹配: envelope=%s payload=%s", sharedevents.CreditChargeRequestedEventType, payload.EventType())); markErr != nil {
				return markErr
			}
			return nil
		}

		if !payload.SkipCharge && payload.CreditCost <= 0 && payload.RMBCostMicros <= 0 {
			if err := eventOutboxRepo.MarkConsumed(ctx, envelope.OutboxID); err != nil {
				return err
			}
			log.Printf("credit charge event skipped with zero cost: event_id=%s outbox_id=%d", payload.EventID, envelope.OutboxID)
			return nil
		}

		tx, err := svc.RecordCreditCharge(ctx, payload)
		if err != nil {
			return err
		}
		if err := eventOutboxRepo.MarkConsumed(ctx, envelope.OutboxID); err != nil {
			return err
		}

		log.Printf(
			"credit charge event consumed by tokenstore: event_id=%s transaction_id=%d outbox_id=%d",
			payload.EventID,
			tx.TransactionID,
			envelope.OutboxID,
		)
		return nil
	}

	return bus.RegisterEventHandler(sharedevents.CreditChargeRequestedEventType, handler)
}

func isAllowedCreditChargeEventVersion(version string) bool {
	version = strings.TrimSpace(version)
	return version == "" || version == sharedevents.CreditChargeEventVersion
}
