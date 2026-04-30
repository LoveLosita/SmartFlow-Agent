package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	"gorm.io/gorm"
)

// ActiveScheduleTriggeredProcessor 描述 active_schedule.triggered worker 真正执行业务所需的最小能力。
//
// 职责边界：
// 1. ProcessTriggeredInTx 负责事务内的 trigger -> preview -> notification 编排；
// 2. MarkTriggerFailedBestEffort 负责事务外的失败回写，避免 outbox retry 前完全没有业务态可查；
// 3. 接口本身不限定具体实现，便于迁移期由 active_scheduler 模块独立演进。
type ActiveScheduleTriggeredProcessor interface {
	ProcessTriggeredInTx(ctx context.Context, tx *gorm.DB, payload sharedevents.ActiveScheduleTriggeredPayload) error
	MarkTriggerFailedBestEffort(ctx context.Context, triggerID string, err error)
}

// RegisterActiveScheduleTriggeredHandler 注册 active_schedule.triggered outbox handler。
//
// 步骤化说明：
// 1. 先做 envelope -> contract DTO 解析与版本校验，明显坏消息直接标记 dead；
// 2. 再通过 ConsumeAndMarkConsumed 把“业务落库 + consumed 推进”收敛在同一事务里；
// 3. 若事务返回 error，则 best-effort 回写 trigger failed，并把错误交给 outbox 做 retry；
// 4. 这里不直接 import active_scheduler 的具体实现，避免 service/events 和业务编排层互相反向耦合。
func RegisterActiveScheduleTriggeredHandler(
	bus *outboxinfra.EventBus,
	outboxRepo *outboxinfra.Repository,
	processor ActiveScheduleTriggeredProcessor,
) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if processor == nil {
		return errors.New("active schedule triggered processor is nil")
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		if !isAllowedTriggeredEventVersion(envelope.EventVersion) {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, fmt.Sprintf("active_schedule.triggered 版本不受支持: %s", envelope.EventVersion))
			return nil
		}

		var payload sharedevents.ActiveScheduleTriggeredPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "解析 active_schedule.triggered 载荷失败: "+unmarshalErr.Error())
			return nil
		}
		if validateErr := payload.Validate(); validateErr != nil {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "active_schedule.triggered 载荷非法: "+validateErr.Error())
			return nil
		}

		err := outboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			return processor.ProcessTriggeredInTx(ctx, tx, payload)
		})
		if err != nil {
			processor.MarkTriggerFailedBestEffort(ctx, payload.TriggerID, err)
			return err
		}
		return nil
	}

	return bus.RegisterEventHandler(sharedevents.ActiveScheduleTriggeredEventType, handler)
}

func isAllowedTriggeredEventVersion(version string) bool {
	version = strings.TrimSpace(version)
	return version == "" || version == sharedevents.ActiveScheduleTriggeredEventVersion
}
