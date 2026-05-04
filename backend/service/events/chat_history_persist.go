package events

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"gorm.io/gorm"
)

const (
	// EventTypeChatHistoryPersistRequested 是聊天消息持久化请求的业务事件类型。
	EventTypeChatHistoryPersistRequested = "chat.history.persist.requested"
)

// RegisterChatHistoryPersistHandler 注册“聊天消息持久化”消费者。
// 职责边界：
// 1. 只处理聊天历史事件，不处理其它业务事件；
// 2. 只负责注册，不负责总线启动；
// 3. 先写本地 chat 相关表，再调用 userauth 调整 token 额度；
// 4. 当前版本仅注册新路由键，不再注册旧兼容键。
func RegisterChatHistoryPersistHandler(
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

	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, EventTypeChatHistoryPersistRequested)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.ChatHistoryPersistPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析聊天持久化载荷失败: "+unmarshalErr.Error())
			return nil
		}

		eventID := strings.TrimSpace(envelope.EventID)
		if eventID == "" {
			eventID = strconv.FormatInt(envelope.OutboxID, 10)
		}

		if err := eventOutboxRepo.ConsumeInTx(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			txM := repoManager.WithTx(tx)
			return txM.Agent.SaveChatHistoryInTx(
				ctx,
				payload.UserID,
				payload.ConversationID,
				payload.Role,
				payload.Message,
				payload.ReasoningContent,
				payload.ReasoningDurationSeconds,
				payload.TokensConsumed,
				eventID,
			)
		}); err != nil {
			return err
		}

		if payload.TokensConsumed > 0 {
			if adjuster == nil {
				return errors.New("userauth token adjuster is nil")
			}
			if _, err := adjuster.AdjustTokenUsage(ctx, contracts.AdjustTokenUsageRequest{
				EventID:    eventID,
				UserID:     payload.UserID,
				TokenDelta: payload.TokensConsumed,
			}); err != nil {
				return err
			}
		}

		return eventOutboxRepo.MarkConsumed(ctx, envelope.OutboxID)
	}

	return bus.RegisterEventHandler(EventTypeChatHistoryPersistRequested, handler)
}

// PublishChatHistoryPersistRequested 发布“聊天消息持久化请求”事件。
func PublishChatHistoryPersistRequested(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	payload model.ChatHistoryPersistPayload,
) error {
	if publisher == nil {
		return errors.New("event publisher is nil")
	}
	return publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    EventTypeChatHistoryPersistRequested,
		EventVersion: outboxinfra.DefaultEventVersion,
		MessageKey:   payload.ConversationID,
		AggregateID:  payload.ConversationID,
		Payload:      payload,
	})
}
