package agentsvc

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/LoveLosita/smartflow/backend/model"
	newagentconv "github.com/LoveLosita/smartflow/backend/newAgent/conv"
	"github.com/LoveLosita/smartflow/backend/respond"
)

// SaveScheduleState 前端暂存日程调整到 Redis 快照。
//
// 职责边界：
// 1. 只负责更新 Redis 中的 ScheduleState 中 source=task_item 的任务；
// 2. 接受绝对时间格式（与 apply-batch 统一），由 conv 层转换为内部相对坐标；
// 3. source=event 的课程保持快照原值不变；
// 4. 不负责写 MySQL、不负责刷新预览缓存；
// 5. 不负责触发 graph 执行（由 confirm_action=accept 驱动）。
func (s *AgentService) SaveScheduleState(
	ctx context.Context,
	userID int,
	conversationID string,
	items []model.SaveScheduleStatePlacedItem,
) error {
	// 1. 加载快照。
	if s.agentStateStore == nil {
		return errors.New("agent state store 未初始化")
	}
	snapshot, ok, err := s.agentStateStore.Load(ctx, conversationID)
	if err != nil {
		return fmt.Errorf("加载快照失败: %w", err)
	}

	if !ok || snapshot == nil || snapshot.ScheduleState == nil {
		return respond.ScheduleStateSnapshotNotFound
	}

	// 2. 校验归属。
	if snapshot.RuntimeState != nil {
		cs := snapshot.RuntimeState.EnsureCommonState()
		if cs.UserID != 0 && cs.UserID != userID {
			return fmt.Errorf("会话归属校验失败：快照 user_id=%d，请求 user_id=%d", cs.UserID, userID)
		}
	}

	// 3. 调用 conv 层将绝对时间放置项应用到 ScheduleState。
	if err := newagentconv.ApplyPlacedItems(snapshot.ScheduleState, items); err != nil {
		return err
	}

	// 4. 写回 Redis。
	if err := s.agentStateStore.Save(ctx, conversationID, snapshot); err != nil {
		return fmt.Errorf("保存快照失败: %w", err)
	}

	log.Printf("[INFO] schedule state saved chat=%s user=%d item_count=%d",
		conversationID, userID, len(items))
	return nil
}
