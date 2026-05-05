package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
)

// ForumRewardGrantRecorder 描述论坛奖励事件消费后写入 token-store 账本所需的最小能力。
//
// 职责边界：
// 1. 只暴露论坛奖励入账能力，不暴露商品、订单和用户可见查询接口；
// 2. 由 token-store 自己解析奖励额度和幂等规则，handler 不计算 Token 数量；
// 3. 接口用于隔离 service/events 与 token-store 具体 RPC client 实现。
type ForumRewardGrantRecorder interface {
	RecordForumRewardGrant(ctx context.Context, req tokencontracts.RecordForumRewardGrantRequest) (*tokencontracts.TokenGrantView, error)
}

// RegisterForumPostLikedRewardHandler 注册计划被点赞奖励消费者。
func RegisterForumPostLikedRewardHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	recorder ForumRewardGrantRecorder,
) error {
	return registerForumRewardHandler(bus, outboxRepo, recorder, sharedevents.ForumPostLikedEventType, sharedevents.ForumRewardSourceLike)
}

// RegisterForumPostImportedRewardHandler 注册计划被导入奖励消费者。
func RegisterForumPostImportedRewardHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	recorder ForumRewardGrantRecorder,
) error {
	return registerForumRewardHandler(bus, outboxRepo, recorder, sharedevents.ForumPostImportedEventType, sharedevents.ForumRewardSourceImport)
}

// registerForumRewardHandler 收敛论坛奖励事件的通用解析、校验和入账流程。
//
// 步骤说明：
// 1. 先校验 outbox 与 token-store 依赖，避免启动期注册半截 handler；
// 2. 消费时先检查版本和 payload，明显不可修复的坏消息直接标记 dead；
// 3. 再调用 token-store 内部 RPC 幂等写 token_grants，RPC 临时失败返回 error 交给 outbox 重试；
// 4. 入账成功后标记 consumed，确保重复消费不会重复发放。
func registerForumRewardHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	recorder ForumRewardGrantRecorder,
	eventType string,
	source string,
) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if recorder == nil {
		return errors.New("forum reward grant recorder is nil")
	}
	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, eventType)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		if !isAllowedForumRewardEventVersion(envelope.EventVersion) {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("论坛奖励事件版本不受支持: %s", envelope.EventVersion))
			return nil
		}

		var payload sharedevents.ForumPostRewardPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析论坛奖励载荷失败: "+unmarshalErr.Error())
			return nil
		}
		if validateErr := payload.Validate(); validateErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "论坛奖励载荷非法: "+validateErr.Error())
			return nil
		}
		if payload.EventType() != eventType {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("论坛奖励事件类型不匹配: envelope=%s payload=%s", eventType, payload.EventType()))
			return nil
		}

		eventID := strings.TrimSpace(envelope.EventID)
		if eventID == "" {
			eventID = strings.TrimSpace(payload.EventID)
		}
		if eventID == "" {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "论坛奖励 event_id 为空")
			return nil
		}

		_, err := recorder.RecordForumRewardGrant(ctx, tokencontracts.RecordForumRewardGrantRequest{
			EventID:        eventID,
			ReceiverUserID: payload.RewardReceiverUserID,
			Source:         forumRewardSource(payload, source),
			SourceRefID:    forumRewardSourceRefID(payload, source),
		})
		if err != nil {
			return err
		}
		return eventOutboxRepo.MarkConsumed(ctx, envelope.OutboxID)
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
