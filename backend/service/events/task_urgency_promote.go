package events

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

const (
	// EventTypeTaskUrgencyPromoteRequested 是“任务紧急性平移请求”事件类型。
	//
	// 命名约束：
	// 1. 只表达业务语义，不泄露 Kafka/outbox 技术细节；
	// 2. 作为稳定路由键长期保留，后续协议演进优先走 event_version。
	EventTypeTaskUrgencyPromoteRequested = "task.urgency.promote.requested"
)

// RegisterTaskUrgencyPromoteRoute 只登记 task 事件归属，不注册消费 handler。
//
// 职责边界：
// 1. 供单体残留路径在迁移期继续把 task 事件写入 task_outbox_messages；
// 2. 不创建 consumer，也不启动 handler，真正消费已迁到 cmd/task；
// 3. 重复登记同一归属是幂等操作。
func RegisterTaskUrgencyPromoteRoute() error {
	return outboxinfra.RegisterEventService(EventTypeTaskUrgencyPromoteRequested, string(outboxHandlerServiceTask))
}

// RegisterTaskUrgencyPromoteHandler 注册“任务紧急性平移”消费者处理器。
//
// 职责边界：
// 1. 只负责注册 handler，不负责启动/关闭事件总线；
// 2. 只处理 `task.urgency.promote.requested` 事件，不处理其他业务事件；
// 3. 通过 `ConsumeAndMarkConsumed` 把“业务更新 + outbox consumed 推进”放进同一事务。
func RegisterTaskUrgencyPromoteHandler(
	bus OutboxBus,
	outboxRepo *outboxinfra.Repository,
	repoManager *dao.RepoManager,
) error {
	// 1. 依赖校验：缺少任意关键依赖都不能安全消费消息。
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if repoManager == nil {
		return errors.New("repo manager is nil")
	}
	eventOutboxRepo, err := scopedOutboxRepoForEvent(outboxRepo, EventTypeTaskUrgencyPromoteRequested)
	if err != nil {
		return err
	}

	// 2. 定义统一处理函数。
	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		// 2.1 先解析 payload；解析失败属于不可恢复错误，直接标记 dead。
		var payload model.TaskUrgencyPromoteRequestedPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析任务紧急性平移载荷失败: "+unmarshalErr.Error())
			return nil
		}

		// 2.2 做轻量参数净化，避免脏数据进入 DAO。
		payload.TaskIDs = sanitizePositiveUniqueIntIDs(payload.TaskIDs)
		if payload.UserID <= 0 || len(payload.TaskIDs) == 0 {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "任务紧急性平移载荷无效: user_id 或 task_ids 非法")
			return nil
		}

		// 2.3 统一走 outbox 消费事务入口，保证“业务成功 -> consumed”原子一致。
		return eventOutboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			// 2.3.1 基于同一 tx 构造 RepoManager，复用现有跨 DAO 事务模式。
			txM := repoManager.WithTx(tx)
			// 2.3.2 以消费时刻为准做条件更新，确保“到线”判定与真实落库时刻一致。
			updated, err := txM.Task.PromoteTaskUrgencyByIDs(ctx, payload.UserID, payload.TaskIDs, time.Now())
			if err != nil {
				return err
			}
			log.Printf("任务紧急性平移消费完成: user_id=%d task_count=%d affected=%d outbox_id=%d", payload.UserID, len(payload.TaskIDs), updated, envelope.OutboxID)
			return nil
		})
	}

	// 3. 注册事件处理器。
	return bus.RegisterEventHandler(EventTypeTaskUrgencyPromoteRequested, handler)
}

// PublishTaskUrgencyPromoteRequested 发布“任务紧急性平移请求”事件。
//
// 职责边界：
// 1. 只负责把业务 DTO 发布到 outbox，不负责等待消费结果；
// 2. 若发布失败，返回 error 交给调用方决定是否降级或重试。
func PublishTaskUrgencyPromoteRequested(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	payload model.TaskUrgencyPromoteRequestedPayload,
) error {
	if publisher == nil {
		return errors.New("event publisher is nil")
	}
	if payload.UserID <= 0 {
		return errors.New("invalid user_id")
	}
	payload.TaskIDs = sanitizePositiveUniqueIntIDs(payload.TaskIDs)
	if len(payload.TaskIDs) == 0 {
		return errors.New("task_ids is empty")
	}
	if payload.TriggeredAt.IsZero() {
		payload.TriggeredAt = time.Now()
	}

	return publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    EventTypeTaskUrgencyPromoteRequested,
		EventVersion: outboxinfra.DefaultEventVersion,
		// 这里使用 user_id 作为消息键，确保同一用户相关平移事件尽量落到同一分区，降低乱序概率。
		MessageKey:  strconv.Itoa(payload.UserID),
		AggregateID: strconv.Itoa(payload.UserID),
		Payload:     payload,
	})
}

// sanitizePositiveUniqueIntIDs 过滤非正数并去重。
//
// 说明：
// 1. 该函数只做参数净化，不承载业务判定；
// 2. 不保证顺序稳定，对当前 SQL where in 语义无影响。
func sanitizePositiveUniqueIntIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
