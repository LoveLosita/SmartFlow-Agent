package events

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

const (
	// EventTypeChatHistoryPersistRequested 是“聊天消息持久化请求”的业务事件类型。
	//
	// 命名策略：
	// 1. 只描述业务语义，不包含 outbox/kafka 等实现词；
	// 2. 作为新路由键长期保留，后续协议变化优先走 event_version；
	// 3. 旧路由键仅作兼容，不再作为新发布默认值。
	EventTypeChatHistoryPersistRequested = "chat.history.persist.requested"
)

// RegisterChatHistoryPersistHandler 注册“聊天消息持久化”消费者处理器。
//
// 职责边界：
// 1. 只负责聊天事件，不处理其他业务事件；
// 2. 只负责注册，不负责总线启停；
// 3. 通过 outbox 通用事务入口把“业务写入 + consumed 推进”合并为一个事务；
// 4. 当前版本仅注册新路由键（chat.history.persist.requested），不再注册旧兼容键。
func RegisterChatHistoryPersistHandler(
	bus *outboxinfra.EventBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
) error {
	// 1. 依赖校验：任何一个关键依赖为空都无法安全处理消息。
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if repoManager == nil {
		return errors.New("repo manager is nil")
	}
	kafkaCfg := kafkabus.LoadConfig()

	// 2. 定义统一处理器：
	// 2.1 解析 payload；
	// 2.2 调用 outbox 通用消费事务；
	// 2.3 在事务回调中复用 RepoManager.WithTx 执行业务 DAO 写入。
	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.ChatHistoryPersistPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			// 2.1 payload 非法属于不可恢复错误，直接标 dead，避免无意义重试。
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "解析聊天持久化载荷失败: "+unmarshalErr.Error())
			return nil
		}

		// 2.2 使用 outbox 通用消费事务，保证“业务写入 + consumed 状态推进”原子一致。
		return outboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			// 2.2.1 基于同一个 tx 构造 RepoManager，复用你现有跨包事务模型。
			txM := repoManager.WithTx(tx)
			// 2.2.2 在同事务内写入聊天历史与会话计数。
			if err := txM.Agent.SaveChatHistoryInTx(
				ctx,
				payload.UserID,
				payload.ConversationID,
				payload.Role,
				payload.Message,
				payload.ReasoningContent,
				payload.ReasoningDurationSeconds,
				payload.TokensConsumed,
			); err != nil {
				return err
			}

			// 2.2.3 Day1 追加“记忆抽取请求”事件入队：
			// 1) 仅对 user 消息投递，避免把助手回复重复喂给抽取链路；
			// 2) 与聊天落库放在同一事务，保证“消息存在 -> 事件一定可追踪”；
			// 3) 若入队失败，整体回滚并触发 outbox 重试，不留半成功状态。
			return EnqueueMemoryExtractRequestedInTx(
				ctx,
				outboxRepo.WithTx(tx),
				kafkaCfg,
				payload,
			)
		})
	}

	// 3. 注册新路由键（主路由）。
	if err := bus.RegisterEventHandler(EventTypeChatHistoryPersistRequested, handler); err != nil {
		return err
	}

	return nil
}

// PublishChatHistoryPersistRequested 发布“聊天消息持久化请求”事件。
//
// 设计目的：
// 1. 让业务层只传 DTO，不重复拼事件元数据；
// 2. 统一消息键策略（conversation_id 作为 MessageKey/AggregateID）；
// 3. 发布失败时显式返回 error，由调用方决定是否降级到同步写库。
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
