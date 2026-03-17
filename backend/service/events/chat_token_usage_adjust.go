package events

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

const (
	// EventTypeChatTokenUsageAdjustRequested 是“会话 token 账本增量调整”事件类型。
	//
	// 命名约束：
	// 1. 仅表达业务语义，不泄露 outbox/kafka 实现细节；
	// 2. 作为稳定路由键长期保留，后续演进优先通过 event_version。
	EventTypeChatTokenUsageAdjustRequested = "chat.token.usage.adjust.requested"
)

// RegisterChatTokenUsageAdjustHandler 注册“会话 token 账本增量调整”消费者。
//
// 职责边界：
// 1. 只处理 token 调整事件，不处理聊天正文落库；
// 2. 通过 outbox 统一消费事务入口，保证“业务成功 + consumed 推进”原子一致；
// 3. 非法载荷直接标记 dead，避免无意义重试。
func RegisterChatTokenUsageAdjustHandler(
	bus *outboxinfra.EventBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if repoManager == nil {
		return errors.New("repo manager is nil")
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.ChatTokenUsageAdjustPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "解析会话 token 调整载荷失败: "+unmarshalErr.Error())
			return nil
		}

		if payload.UserID <= 0 || payload.TokensDelta <= 0 || payload.ConversationID == "" {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "会话 token 调整载荷无效: user_id/conversation_id/tokens_delta 非法")
			return nil
		}

		return outboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			txM := repoManager.WithTx(tx)
			return txM.Agent.AdjustTokenUsageInTx(ctx, payload.UserID, payload.ConversationID, payload.TokensDelta)
		})
	}

	return bus.RegisterEventHandler(EventTypeChatTokenUsageAdjustRequested, handler)
}

// PublishChatTokenUsageAdjustRequested 发布“会话 token 账本增量调整”事件。
//
// 说明：
// 1. 只保证“写入 outbox 成功”，不等待消费完成；
// 2. 业务层只传 DTO，不关心 outbox/kafka 协议细节。
func PublishChatTokenUsageAdjustRequested(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	payload model.ChatTokenUsageAdjustPayload,
) error {
	if publisher == nil {
		return errors.New("event publisher is nil")
	}
	if payload.UserID <= 0 {
		return errors.New("invalid user_id")
	}
	if payload.TokensDelta <= 0 {
		return errors.New("invalid tokens_delta")
	}
	if payload.ConversationID == "" {
		return errors.New("invalid conversation_id")
	}
	if payload.TriggeredAt.IsZero() {
		payload.TriggeredAt = time.Now()
	}

	return publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    EventTypeChatTokenUsageAdjustRequested,
		EventVersion: outboxinfra.DefaultEventVersion,
		MessageKey:   payload.ConversationID,
		AggregateID:  strconv.Itoa(payload.UserID) + ":" + payload.ConversationID,
		Payload:      payload,
	})
}
