package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

const EventTypeAgentTimelinePersistRequested = "agent.timeline.persist.requested"

// RegisterAgentTimelinePersistHandler 注册“会话时间线持久化”消费者处理器。
//
// 职责边界：
// 1. 只负责 timeline 事件，不处理 chat_history 等其他业务消息；
// 2. 只负责注册 handler，不负责总线启停；
// 3. 通过 outbox 通用消费事务，把“时间线写库 + consumed 推进”放进同一事务；
// 4. 若遇到 seq 唯一键冲突，会先判定是否属于重放幂等，再决定是否补新 seq 并回填 Redis。
func RegisterAgentTimelinePersistHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	agentRepo *dao.AgentDAO,
	cacheDAO *dao.CacheDAO,
) error {
	// 1. 依赖校验：缺少任一关键依赖都无法安全消费消息。
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if agentRepo == nil {
		return errors.New("agent repo is nil")
	}
	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, EventTypeAgentTimelinePersistRequested)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.ChatTimelinePersistPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			// 1. payload 无法反序列化属于不可恢复错误，直接标 dead，避免无意义重试。
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析时间线持久化载荷失败: "+unmarshalErr.Error())
			return nil
		}

		payload = payload.Normalize()
		if !payload.HasValidIdentity() {
			// 2. 这里只校验“能否唯一定位一条 timeline 记录”的最小字段集合。
			// 3. content / payload_json 是否为空由事件类型自行决定，不在这里一刀切限制。
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "时间线持久化载荷非法: user_id/conversation_id/seq/kind 非法")
			return nil
		}

		refreshCache := false
		finalSeq := payload.Seq

		// 4. 统一走 outbox 消费事务入口，保证“业务写入成功 -> consumed”原子一致。
		err := eventOutboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			finalPayload, repaired, persistErr := persistConversationTimelineEventInTx(ctx, tx, agentRepo.WithTx(tx), payload)
			if persistErr != nil {
				return persistErr
			}
			refreshCache = repaired
			finalSeq = finalPayload.Seq
			return nil
		})
		if err != nil {
			return err
		}

		// 5. 只有发生“seq 冲突且补了新 seq”时，才需要重建 Redis timeline。
		// 5.1 原因：主链路已经先写过 Redis，常规成功无需重复回写。
		// 5.2 若发生补 seq，不重建会留下旧 seq 的缓存残影，刷新后顺序会错乱。
		// 5.3 缓存重建失败只记日志，不能反向把已 consumed 的 outbox 回滚。
		if refreshCache {
			if refreshErr := rebuildConversationTimelineCache(ctx, agentRepo, cacheDAO, payload.UserID, payload.ConversationID, finalSeq); refreshErr != nil {
				log.Printf("重建时间线缓存失败 user=%d chat=%s seq=%d err=%v", payload.UserID, payload.ConversationID, finalSeq, refreshErr)
			}
		}
		return nil
	}

	return bus.RegisterEventHandler(EventTypeAgentTimelinePersistRequested, handler)
}

// PublishAgentTimelinePersistRequested 发布“会话时间线持久化请求”事件。
//
// 设计目的：
// 1. 让业务层只传 DTO，不重复拼事件元数据；
// 2. 统一以 conversation_id 作为 MessageKey / AggregateID，尽量降低同会话乱序概率；
// 3. 发布失败显式返回 error，由调用方决定是否中断主链路。
func PublishAgentTimelinePersistRequested(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	payload model.ChatTimelinePersistPayload,
) error {
	if publisher == nil {
		return errors.New("event publisher is nil")
	}

	payload = payload.Normalize()
	if !payload.HasValidIdentity() {
		return errors.New("invalid timeline persist payload")
	}

	return publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    EventTypeAgentTimelinePersistRequested,
		EventVersion: outboxinfra.DefaultEventVersion,
		MessageKey:   payload.ConversationID,
		AggregateID:  payload.ConversationID,
		Payload:      payload,
	})
}

// persistConversationTimelineEventInTx 负责在单个事务里完成 timeline 事件写库。
//
// 步骤化说明：
// 1. 先按 payload 原始 seq 尝试写入；
// 2. 若命中 seq 唯一键冲突，先查询同 seq 记录，判断是否属于“重放同一事件”；
// 3. 若不是重放，而是 Redis seq 漂移导致的新旧事件撞 seq，则用 max(seq)+1 重新分配；
// 4. 最多修复 3 次，避免异常数据把消费者拖进无限循环。
func persistConversationTimelineEventInTx(
	ctx context.Context,
	tx *gorm.DB,
	agentRepo *dao.AgentDAO,
	payload model.ChatTimelinePersistPayload,
) (model.ChatTimelinePersistPayload, bool, error) {
	if tx == nil {
		return payload, false, errors.New("transaction is nil")
	}
	if agentRepo == nil {
		return payload, false, errors.New("agent repo is nil")
	}

	working := payload.Normalize()
	repaired := false

	for attempt := 0; attempt < 3; attempt++ {
		if _, _, err := agentRepo.SaveConversationTimelineEvent(ctx, working); err == nil {
			return working, repaired, nil
		} else if !model.IsTimelineSeqConflictError(err) {
			return working, repaired, err
		}

		// 1. 先判断是否属于“同一条事件被重复消费”。
		// 2. 若库里已有记录且字段完全一致，说明前一次其实已经成功落库，本次可视为幂等成功。
		// 3. 若字段不一致，再进入“补新 seq”分支，避免把真正的新事件吞掉。
		existing, findErr := findConversationTimelineEventBySeq(ctx, tx, working.UserID, working.ConversationID, working.Seq)
		if findErr == nil && working.MatchesStoredEvent(existing) {
			return working, repaired, nil
		}
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return working, repaired, findErr
		}

		maxSeq, maxErr := loadConversationTimelineMaxSeq(ctx, tx, working.UserID, working.ConversationID)
		if maxErr != nil {
			return working, repaired, maxErr
		}
		working.Seq = maxSeq + 1
		repaired = true
	}

	return working, repaired, fmt.Errorf("timeline seq repair exceeded limit user=%d chat=%s", working.UserID, working.ConversationID)
}

func findConversationTimelineEventBySeq(
	ctx context.Context,
	tx *gorm.DB,
	userID int,
	conversationID string,
	seq int64,
) (model.AgentTimelineEvent, error) {
	var event model.AgentTimelineEvent
	err := tx.WithContext(ctx).
		Where("user_id = ? AND chat_id = ? AND seq = ?", userID, strings.TrimSpace(conversationID), seq).
		Take(&event).Error
	return event, err
}

func loadConversationTimelineMaxSeq(
	ctx context.Context,
	tx *gorm.DB,
	userID int,
	conversationID string,
) (int64, error) {
	var maxSeq int64
	err := tx.WithContext(ctx).
		Model(&model.AgentTimelineEvent{}).
		Where("user_id = ? AND chat_id = ?", userID, strings.TrimSpace(conversationID)).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&maxSeq).Error
	if err != nil {
		return 0, err
	}
	return maxSeq, nil
}

// rebuildConversationTimelineCache 在“补新 seq”后重建 Redis timeline 缓存。
//
// 说明：
// 1. 这里只在缓存存在时执行；未接 Redis 的环境直接跳过即可；
// 2. 需要整表重建而不是只 append 一条，因为旧缓存里已经存在错误 seq 的事件；
// 3. 这里不抽到 agent/sv 复用，是因为 events 不能反向依赖 service，否则会形成循环依赖。
func rebuildConversationTimelineCache(
	ctx context.Context,
	agentRepo *dao.AgentDAO,
	cacheDAO *dao.CacheDAO,
	userID int,
	conversationID string,
	finalSeq int64,
) error {
	if cacheDAO == nil || agentRepo == nil {
		return nil
	}

	events, err := agentRepo.ListConversationTimelineEvents(ctx, userID, conversationID)
	if err != nil {
		return err
	}
	items := buildConversationTimelineCacheItems(events)
	if err = cacheDAO.SetConversationTimelineToCache(ctx, userID, conversationID, items); err != nil {
		return err
	}

	if len(items) > 0 {
		finalSeq = items[len(items)-1].Seq
	}
	return cacheDAO.SetConversationTimelineSeq(ctx, userID, conversationID, finalSeq)
}

func buildConversationTimelineCacheItems(events []model.AgentTimelineEvent) []model.GetConversationTimelineItem {
	if len(events) == 0 {
		return make([]model.GetConversationTimelineItem, 0)
	}

	items := make([]model.GetConversationTimelineItem, 0, len(events))
	for _, event := range events {
		item := model.GetConversationTimelineItem{
			ID:             event.ID,
			Seq:            event.Seq,
			Kind:           strings.TrimSpace(event.Kind),
			TokensConsumed: event.TokensConsumed,
			CreatedAt:      event.CreatedAt,
		}
		if event.Role != nil {
			item.Role = strings.TrimSpace(*event.Role)
		}
		if event.Content != nil {
			item.Content = strings.TrimSpace(*event.Content)
		}
		if event.Payload != nil {
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(*event.Payload)), &payload); err == nil && len(payload) > 0 {
				item.Payload = payload
			}
		}
		items = append(items, item)
	}
	return normalizeConversationTimelineCacheItems(items)
}

func normalizeConversationTimelineCacheItems(items []model.GetConversationTimelineItem) []model.GetConversationTimelineItem {
	if len(items) == 0 {
		return make([]model.GetConversationTimelineItem, 0)
	}

	normalized := make([]model.GetConversationTimelineItem, 0, len(items))
	for _, item := range items {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		kind := canonicalizeConversationTimelineKind(item.Kind, role)

		if kind == "" {
			switch role {
			case "user":
				kind = model.AgentTimelineKindUserText
			case "assistant":
				kind = model.AgentTimelineKindAssistantText
			}
		}
		if role == "" {
			switch kind {
			case model.AgentTimelineKindUserText:
				role = "user"
			case model.AgentTimelineKindAssistantText:
				role = "assistant"
			}
		}

		item.Kind = kind
		item.Role = role
		normalized = append(normalized, item)
	}
	return normalized
}

func canonicalizeConversationTimelineKind(kind string, role string) string {
	normalizedKind := strings.ToLower(strings.TrimSpace(kind))
	normalizedRole := strings.ToLower(strings.TrimSpace(role))

	switch normalizedKind {
	case model.AgentTimelineKindUserText,
		model.AgentTimelineKindAssistantText,
		model.AgentTimelineKindToolCall,
		model.AgentTimelineKindToolResult,
		model.AgentTimelineKindConfirmRequest,
		model.AgentTimelineKindBusinessCard,
		model.AgentTimelineKindScheduleCompleted,
		model.AgentTimelineKindThinkingSummary:
		return normalizedKind
	case "text", "message", "query":
		if normalizedRole == "user" {
			return model.AgentTimelineKindUserText
		}
		if normalizedRole == "assistant" {
			return model.AgentTimelineKindAssistantText
		}
	}
	return normalizedKind
}
