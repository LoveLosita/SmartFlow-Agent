package sv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"

	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
)

// OutboxBus 是 token-store 注册论坛奖励 handler 需要的最小总线接口。
type OutboxBus interface {
	RegisterEventHandler(eventType string, handler outboxinfra.MessageHandler) error
}

// RegisterForumRewardRoutes 只登记 token-store 负责消费的论坛奖励事件归属。
//
// 职责边界：
// 1. 只负责把事件类型路由到 token-store 的服务级 outbox；
// 2. 不注册 handler，也不启动 consumer；
// 3. 供发布方和消费方在不同进程内共享同一份事件归属映射。
func RegisterForumRewardRoutes() error {
	if err := outboxinfra.RegisterEventService(sharedevents.ForumPostLikedEventType, outboxinfra.ServiceTokenStore); err != nil {
		return err
	}
	return outboxinfra.RegisterEventService(sharedevents.ForumPostImportedEventType, outboxinfra.ServiceTokenStore)
}

// RegisterForumRewardHandlers 注册 token-store 对论坛奖励事件的消费处理器。
//
// 职责边界：
// 1. 只消费 forum.post.liked / forum.post.imported 两类事件；
// 2. 论坛奖励账本写入由 token-store Service 负责，handler 不自行计算金额；
// 3. grant 写入成功后再标记 consumed；若标记失败，可依赖 event_id 幂等安全重试。
func RegisterForumRewardHandlers(bus OutboxBus, outboxRepo *outboxinfra.Repository, svc *Service) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if svc == nil {
		return errors.New("tokenstore service is nil")
	}
	if err := RegisterForumRewardRoutes(); err != nil {
		return err
	}
	if err := registerForumRewardHandler(bus, outboxRepo, svc, sharedevents.ForumPostLikedEventType, sharedevents.ForumRewardSourceLike); err != nil {
		return err
	}
	return registerForumRewardHandler(bus, outboxRepo, svc, sharedevents.ForumPostImportedEventType, sharedevents.ForumRewardSourceImport)
}

func registerForumRewardHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	svc *Service,
	eventType string,
	source string,
) error {
	route, ok := outboxinfra.ResolveEventRoute(eventType)
	if !ok {
		return fmt.Errorf("forum reward outbox route is missing: eventType=%s", eventType)
	}
	eventOutboxRepo := outboxRepo.WithRoute(route)

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		if !isAllowedForumRewardEventVersion(envelope.EventVersion) {
			if err := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("论坛奖励事件版本不受支持: %s", envelope.EventVersion)); err != nil {
				return err
			}
			return nil
		}

		var payload sharedevents.ForumPostRewardPayload
		if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析论坛奖励载荷失败: "+err.Error()); markErr != nil {
				return markErr
			}
			return nil
		}
		if err := payload.Validate(); err != nil {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "论坛奖励载荷非法: "+err.Error()); markErr != nil {
				return markErr
			}
			return nil
		}
		if payload.EventType() != eventType {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("论坛奖励事件类型不匹配: envelope=%s payload=%s", eventType, payload.EventType())); markErr != nil {
				return markErr
			}
			return nil
		}

		eventID := strings.TrimSpace(envelope.EventID)
		if eventID == "" {
			eventID = strings.TrimSpace(payload.EventID)
		}
		if eventID == "" {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "论坛奖励 event_id 为空"); markErr != nil {
				return markErr
			}
			return nil
		}

		sourceRefID, parseErr := parseForumRewardSourceRefID(forumRewardSourceRefID(payload, source))
		if parseErr != nil {
			if markErr := eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "论坛奖励 source_ref_id 非法: "+parseErr.Error()); markErr != nil {
				return markErr
			}
			return nil
		}

		transaction, err := svc.RecordForumRewardCredit(ctx, forumRewardGrantRequest{
			EventID:        eventID,
			ReceiverUserID: payload.RewardReceiverUserID,
			Source:         forumRewardSource(payload, source),
			SourceRefID:    sourceRefID,
		})
		if err != nil {
			return err
		}
		if err := eventOutboxRepo.MarkConsumed(ctx, envelope.OutboxID); err != nil {
			return err
		}

		log.Printf(
			"forum reward event consumed by tokenstore: event_type=%s event_id=%s transaction_id=%d outbox_id=%d",
			eventType,
			eventID,
			transaction.TransactionID,
			envelope.OutboxID,
		)
		return nil
	}

	return bus.RegisterEventHandler(eventType, handler)
}

func isAllowedForumRewardEventVersion(version string) bool {
	version = strings.TrimSpace(version)
	return version == "" || version == sharedevents.ForumRewardEventVersion
}

func forumRewardSource(payload sharedevents.ForumPostRewardPayload, fallback string) string {
	source := strings.TrimSpace(payload.Source)
	if source != "" {
		return source
	}
	return fallback
}

func forumRewardSourceRefID(payload sharedevents.ForumPostRewardPayload, source string) string {
	if source == sharedevents.ForumRewardSourceImport && payload.ImportID > 0 {
		return strconv.FormatUint(payload.ImportID, 10)
	}
	return strconv.FormatUint(payload.PostID, 10)
}
