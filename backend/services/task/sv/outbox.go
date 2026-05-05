package sv

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strconv"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	taskdao "github.com/LoveLosita/smartflow/backend/services/task/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"gorm.io/gorm"
)

const (
	// EventTypeTaskUrgencyPromoteRequested 是“任务紧急性平移请求”事件类型。
	EventTypeTaskUrgencyPromoteRequested = "task.urgency.promote.requested"
)

// OutboxBus 是 task 服务注册消费 handler 需要的最小总线接口。
type OutboxBus interface {
	RegisterEventHandler(eventType string, handler outboxinfra.MessageHandler) error
}

// RegisterTaskUrgencyPromoteRoute 只登记 task 事件归属，不注册消费 handler。
//
// 职责边界：
// 1. 供迁移期其它进程发布 task 事件时解析到 task_outbox_messages；
// 2. 不创建 Kafka consumer，也不启动 task handler；
// 3. 真正消费仍由 cmd/task 调用 RegisterTaskUrgencyPromoteHandler 承担。
func RegisterTaskUrgencyPromoteRoute() error {
	return outboxinfra.RegisterEventService(EventTypeTaskUrgencyPromoteRequested, outboxinfra.ServiceTask)
}

// RegisterTaskUrgencyPromoteHandler 注册 task 服务自己的“紧急性平移”消费者。
//
// 职责边界：
// 1. 只处理 task.urgency.promote.requested，不处理 agent/memory 等其它事件；
// 2. 业务更新和 outbox consumed 推进放在同一事务内；
// 3. handler 不创建 DAO 或 event bus，避免消费链路隐藏启动依赖。
func RegisterTaskUrgencyPromoteHandler(bus OutboxBus, outboxRepo *outboxinfra.Repository, taskDAO *taskdao.TaskDAO) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if taskDAO == nil {
		return errors.New("task dao is nil")
	}
	if err := RegisterTaskUrgencyPromoteRoute(); err != nil {
		return err
	}
	route, ok := outboxinfra.ResolveEventRoute(EventTypeTaskUrgencyPromoteRequested)
	if !ok {
		return errors.New("task.urgency.promote.requested route is missing")
	}
	eventOutboxRepo := outboxRepo.WithRoute(route)

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.TaskUrgencyPromoteRequestedPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "解析任务紧急性平移载荷失败: "+unmarshalErr.Error())
			return nil
		}

		payload.TaskIDs = sanitizePositiveUniqueIntIDs(payload.TaskIDs)
		if payload.UserID <= 0 || len(payload.TaskIDs) == 0 {
			_ = eventOutboxRepo.MarkDead(ctx, envelope.OutboxID, "任务紧急性平移载荷无效: user_id 或 task_ids 非法")
			return nil
		}

		return eventOutboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			updated, err := taskDAO.WithTx(tx).PromoteTaskUrgencyByIDs(ctx, payload.UserID, payload.TaskIDs, time.Now())
			if err != nil {
				return err
			}
			log.Printf("任务紧急性平移消费完成: user_id=%d task_count=%d affected=%d outbox_id=%d", payload.UserID, len(payload.TaskIDs), updated, envelope.OutboxID)
			return nil
		})
	}

	return bus.RegisterEventHandler(EventTypeTaskUrgencyPromoteRequested, handler)
}

// PublishTaskUrgencyPromoteRequested 发布“任务紧急性平移请求”事件。
func PublishTaskUrgencyPromoteRequested(ctx context.Context, publisher outboxinfra.EventPublisher, payload model.TaskUrgencyPromoteRequestedPayload) error {
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
		MessageKey:   strconv.Itoa(payload.UserID),
		AggregateID:  strconv.Itoa(payload.UserID),
		Payload:      payload,
	})
}

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
