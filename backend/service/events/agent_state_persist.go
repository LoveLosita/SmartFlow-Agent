package events

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	// EventTypeAgentStateSnapshotPersist 是"agent 状态快照持久化"的业务事件类型。
	EventTypeAgentStateSnapshotPersist = "agent.state.snapshot.persist"
)

// AgentStateSnapshotPayload 是 outbox 事件的业务载荷。
type AgentStateSnapshotPayload struct {
	ConversationID string `json:"conversation_id"`
	UserID         int    `json:"user_id"`
	Phase          string `json:"phase"`
	SnapshotJSON   string `json:"snapshot_json"`
}

// RegisterAgentStateSnapshotHandler 注册"agent 状态快照持久化"消费者处理器。
//
// 职责边界：
// 1. 只负责快照写入 agent_state_snapshot_records 表；
// 2. 使用 upsert 语义，同一 conversation_id 只保留最新快照；
// 3. 通过 outbox 通用消费事务保证"业务写入 + consumed 推进"原子一致。
func RegisterAgentStateSnapshotHandler(
	bus OutboxBus,
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
	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, EventTypeAgentStateSnapshotPersist)
	if err != nil {
		return err
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload AgentStateSnapshotPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析快照载荷失败: "+unmarshalErr.Error())
			return nil
		}

		return eventOutboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			record := model.AgentStateSnapshotRecord{
				ConversationID: payload.ConversationID,
				UserID:         payload.UserID,
				Phase:          payload.Phase,
				SnapshotJSON:   payload.SnapshotJSON,
			}
			return tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "conversation_id"}},
				DoUpdates: clause.AssignmentColumns([]string{"user_id", "phase", "snapshot_json", "updated_at"}),
			}).Create(&record).Error
		})
	}

	return bus.RegisterEventHandler(EventTypeAgentStateSnapshotPersist, handler)
}

// PublishAgentStateSnapshot 发布"agent 状态快照持久化"事件到 outbox。
//
// 设计说明：
// 1. 将快照 JSON 序列化后通过 outbox 异步写入 MySQL；
// 2. publisher 为 nil 时静默降级（Kafka 未启用场景）；
// 3. 发布失败只记日志，不中断主流程。
func PublishAgentStateSnapshot(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	snapshot *agentmodel.AgentStateSnapshot,
	conversationID string,
	userID int,
) {
	if publisher == nil {
		return
	}
	if snapshot == nil {
		return
	}

	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		log.Printf("[WARN] 序列化 agent 状态快照失败 chat=%s: %v", conversationID, err)
		return
	}

	phase := ""
	if snapshot.RuntimeState != nil {
		cs := snapshot.RuntimeState.EnsureCommonState()
		if cs != nil {
			phase = string(cs.Phase)
		}
	}

	payload := AgentStateSnapshotPayload{
		ConversationID: conversationID,
		UserID:         userID,
		Phase:          phase,
		SnapshotJSON:   string(snapshotJSON),
	}

	if err := publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    EventTypeAgentStateSnapshotPersist,
		EventVersion: outboxinfra.DefaultEventVersion,
		MessageKey:   conversationID,
		AggregateID:  conversationID,
		Payload:      payload,
	}); err != nil {
		log.Printf("[WARN] 发布 agent 状态快照事件失败 chat=%s: %v", conversationID, err)
	}
}
