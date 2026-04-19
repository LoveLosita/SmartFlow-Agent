package agentsvc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	newagentconv "github.com/LoveLosita/smartflow/backend/newAgent/conv"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

// SaveScheduleState 处理前端拖拽后的“暂存排程状态”请求。
//
// 职责边界：
// 1. 负责把前端绝对坐标写回当前会话的 ScheduleState 快照；
// 2. 负责刷新 Redis 预览缓存，保证后续预览读取与最新拖拽一致；
// 3. 不负责写 MySQL 正式课表，也不负责触发新一轮 graph 执行。
func (s *AgentService) SaveScheduleState(
	ctx context.Context,
	userID int,
	conversationID string,
	items []model.SaveScheduleStatePlacedItem,
) error {
	// 1. 加载会话快照；没有快照说明当前会话不在可微调窗口内。
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

	// 2. 做会话归属校验，防止跨用户写入别人的会话快照。
	if snapshot.RuntimeState != nil {
		cs := snapshot.RuntimeState.EnsureCommonState()
		if cs.UserID != 0 && cs.UserID != userID {
			return fmt.Errorf("会话归属校验失败：快照 user_id=%d，请求 user_id=%d", cs.UserID, userID)
		}
	}

	// 3. 将前端绝对坐标应用到内存态 ScheduleState。
	// 3.1 这里只修改 source=task_item 任务；
	// 3.2 source=event 课程位保持不变；
	// 3.3 坐标非法时由 ApplyPlacedItems 返回明确错误。
	if err := newagentconv.ApplyPlacedItems(snapshot.ScheduleState, items); err != nil {
		return err
	}

	// 4. 先写回运行态快照，确保“拖拽后的状态”成为后续读链路真值。
	if err := s.agentStateStore.Save(ctx, conversationID, snapshot); err != nil {
		return fmt.Errorf("保存快照失败: %w", err)
	}

	// 5. 再刷新预览缓存，避免 GetSchedulePlanPreview 读到拖拽前旧缓存。
	if err := s.refreshSchedulePreviewAfterStateSave(ctx, userID, conversationID, snapshot); err != nil {
		return err
	}

	log.Printf("[INFO] schedule state saved chat=%s user=%d item_count=%d", conversationID, userID, len(items))
	return nil
}

// refreshSchedulePreviewAfterStateSave 按“最新快照”重建并覆盖 Redis 预览缓存。
//
// 职责边界：
// 1. 只处理 Redis 预览缓存，不负责 MySQL 快照；
// 2. 以最新 ScheduleState 为准，修复“预览读到旧拖拽结果”的回滚问题；
// 3. 尽量保留旧预览中的 trace_id/candidate_plans，避免前端字段突变。
func (s *AgentService) refreshSchedulePreviewAfterStateSave(
	ctx context.Context,
	userID int,
	conversationID string,
	snapshot *newagentmodel.AgentStateSnapshot,
) error {
	// 1. 依赖不完整时直接跳过，避免写入不完整缓存。
	if s == nil || s.cacheDAO == nil || snapshot == nil || snapshot.ScheduleState == nil {
		return nil
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return nil
	}

	// 2. 从运行态提取 task_class_ids，保证预览过滤口径与会话一致。
	taskClassIDs := make([]int, 0)
	if snapshot.RuntimeState != nil {
		flowState := snapshot.RuntimeState.EnsureCommonState()
		taskClassIDs = append(taskClassIDs, flowState.TaskClassIDs...)
	}

	// 3. 基于最新 ScheduleState 生成预览主干（hybrid_entries 为最新真值）。
	preview := newagentconv.ScheduleStateToPreview(
		snapshot.ScheduleState,
		userID,
		normalizedConversationID,
		taskClassIDs,
		"",
	)
	if preview == nil {
		return nil
	}

	// 4. 合并旧预览里需要保留的字段，避免前端依赖字段突然丢失。
	existingPreview, err := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, normalizedConversationID)
	if err != nil {
		return fmt.Errorf("读取排程预览缓存失败: %w", err)
	}
	if existingPreview != nil {
		preview.TraceID = strings.TrimSpace(existingPreview.TraceID)
		if len(existingPreview.CandidatePlans) > 0 {
			preview.CandidatePlans = cloneWeekSchedules(existingPreview.CandidatePlans)
		}
		if len(existingPreview.AllocatedItems) > 0 {
			preview.AllocatedItems = cloneTaskClassItems(existingPreview.AllocatedItems)
		}
		if len(preview.TaskClassIDs) == 0 && len(existingPreview.TaskClassIDs) > 0 {
			preview.TaskClassIDs = append([]int(nil), existingPreview.TaskClassIDs...)
		}
	}
	if preview.CandidatePlans == nil {
		preview.CandidatePlans = make([]model.UserWeekSchedule, 0)
	}
	if preview.HybridEntries == nil {
		preview.HybridEntries = make([]model.HybridScheduleEntry, 0)
	}
	if preview.TaskClassIDs == nil {
		preview.TaskClassIDs = make([]int, 0)
	}

	// 5. 回写 Redis 预览缓存；失败则返回错误，让前端可感知并重试。
	if err := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedConversationID, preview); err != nil {
		return fmt.Errorf("刷新排程预览缓存失败: %w", err)
	}
	return nil
}
