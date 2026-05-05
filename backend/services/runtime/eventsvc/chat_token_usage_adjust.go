package eventsvc

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"gorm.io/gorm"
)

const (
	// EventTypeChatTokenUsageAdjustRequested 是“会话 token 额度调整”事件类型。
	// 命名约束：
	// 1. 只表达业务语义，不泄露 outbox/kafka 实现细节；
	// 2. 作为稳定路由键长期保留，后续演进优先通过 event_version。
	EventTypeChatTokenUsageAdjustRequested = "chat.token.usage.adjust.requested"
)

// RegisterChatTokenUsageAdjustHandler 注册“会话 token 额度调整”消费者。
// 职责边界：
// 1. 只处理 token 调整事件，不处理聊天正文落库；
// 2. 先写本地账本，再调用 userauth 侧做额度同步；
// 3. 非法载荷直接标记 dead，避免无意义重试。
func RegisterChatTokenUsageAdjustHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
	adjuster ports.TokenUsageAdjuster,
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

	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, EventTypeChatTokenUsageAdjustRequested)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.ChatTokenUsageAdjustPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析会话 token 调整载荷失败: "+unmarshalErr.Error())
			return nil
		}

		if payload.UserID <= 0 || payload.TokensDelta <= 0 || payload.ConversationID == "" {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "会话 token 调整载荷无效: user_id/conversation_id/tokens_delta 非法")
			return nil
		}

		eventID := strings.TrimSpace(envelope.EventID)
		if eventID == "" {
			eventID = strconv.FormatInt(envelope.OutboxID, 10)
		}

		if err := eventOutboxRepo.ConsumeInTx(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			txM := repoManager.WithTx(tx)
			return txM.Agent.AdjustTokenUsageInTx(ctx, payload.UserID, payload.ConversationID, payload.TokensDelta, eventID)
		}); err != nil {
			return err
		}

		if adjuster == nil {
			return errors.New("userauth token adjuster is nil")
		}
		if _, err := adjuster.AdjustTokenUsage(ctx, contracts.AdjustTokenUsageRequest{
			EventID:    eventID,
			UserID:     payload.UserID,
			TokenDelta: payload.TokensDelta,
		}); err != nil {
			return err
		}

		return eventOutboxRepo.MarkConsumed(ctx, envelope.OutboxID)
	}

	return bus.RegisterEventHandler(EventTypeChatTokenUsageAdjustRequested, handler)
}

// PublishChatTokenUsageAdjustRequested 发布“会话 token 额度调整”事件。
// 1. 这里只保证 outbox 写入成功，不等待消费结果；
// 2. 业务层只关心 DTO，不关心 outbox/Kafka 细节。
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
