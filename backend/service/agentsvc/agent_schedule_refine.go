package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/agent/scheduleplan"
	"github.com/LoveLosita/smartflow/backend/agent/schedulerefine"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino-ext/components/model/ark"
)

// runScheduleRefineFlow 执行“连续对话微调排程”分支。
//
// 职责边界：
// 1. 负责读取“上一版排程预览快照”（优先 Redis，缺失再回源 MySQL）；
// 2. 负责调用独立 schedulerefine 图链路完成本轮微调；
// 3. 负责把微调结果回写预览缓存与状态快照，供后续继续微调；
// 4. 不负责聊天消息持久化（消息持久化由 AgentChat 主链路统一处理）。
func (s *AgentService) runScheduleRefineFlow(
	ctx context.Context,
	selectedModel *ark.ChatModel,
	userMessage string,
	userID int,
	chatID string,
	traceID string,
	emitStage func(stage, detail string),
	outChan chan<- string,
	modelName string,
) (string, error) {
	_ = outChan
	_ = modelName

	// 1. 依赖预检：模型为空时无法执行任何节点，直接失败避免空指针。
	if selectedModel == nil {
		return "", errors.New("schedule refine model is nil")
	}

	emitStage("schedule_refine.context.loading", "正在加载上一版排程上下文。")

	// 2. 先查 Redis 预览快照，保证热路径低延迟。
	// 2.1 如果 Redis 未命中，再回源 MySQL 快照兜底；
	// 2.2 如果两者都没有，说明当前会话没有可微调基础，直接返回业务错误。
	preview := s.loadSchedulePreviewContext(ctx, userID, chatID)
	if preview == nil {
		return "", respond.SchedulePlanPreviewNotFound
	}

	// 3. 初始化微调状态并运行独立图。
	state := schedulerefine.NewScheduleRefineState(traceID, userID, chatID, userMessage, preview)
	finalState, runErr := schedulerefine.RunScheduleRefineGraph(ctx, schedulerefine.ScheduleRefineGraphRunInput{
		Model:     selectedModel,
		State:     state,
		EmitStage: emitStage,
	})
	if runErr != nil {
		return "", runErr
	}
	if finalState == nil {
		return "", errors.New("schedule refine graph returned nil state")
	}

	// 4. 调用目的：
	// 4.1 saveSchedulePlanPreview 目前是“预览缓存 + MySQL 快照”的统一写入口；
	// 4.2 这里把 refine state 映射为 scheduleplan state，复用已有落盘链路；
	// 4.3 但若是“独立复合分支已出站、终审仍失败”，则不覆盖上一版预览，避免外部误以为新方案已验证通过。
	if shouldPersistScheduleRefinePreview(finalState) {
		s.saveSchedulePlanPreview(ctx, userID, chatID, convertRefineStateToPlanState(finalState))
	} else {
		emitStage("schedule_refine.preview.skipped", "复合分支终审未通过，本轮结果不覆盖上一版预览。")
	}

	reply := strings.TrimSpace(finalState.FinalSummary)
	if reply == "" {
		reply = "微调已完成，但本轮未生成总结文案。"
	}
	return reply, nil
}

// loadSchedulePreviewContext 读取“可用于连续微调”的排程上下文快照。
//
// 步骤化说明：
// 1. 先查 Redis：命中则直接返回，时延最小；
// 2. Redis miss 再查 MySQL：保证缓存过期后仍可继续微调；
// 3. 若 MySQL 命中且 Redis 可用，顺便回填 Redis，提升后续命中率；
// 4. 任一步失败仅打日志，不 panic，由上层根据返回 nil 做统一处理。
func (s *AgentService) loadSchedulePreviewContext(ctx context.Context, userID int, chatID string) *model.SchedulePlanPreviewCache {
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" || userID <= 0 {
		return nil
	}

	if s.cacheDAO != nil {
		preview, err := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, normalizedChatID)
		if err != nil {
			log.Printf("读取排程预览缓存失败 chat_id=%s: %v", normalizedChatID, err)
		} else if preview != nil {
			return preview
		}
	}

	if s.repo == nil {
		return nil
	}
	snapshot, err := s.repo.GetScheduleStateSnapshot(ctx, userID, normalizedChatID)
	if err != nil {
		log.Printf("读取排程状态快照失败 chat_id=%s: %v", normalizedChatID, err)
		return nil
	}
	if snapshot == nil {
		return nil
	}

	preview := snapshotToSchedulePlanPreviewCache(snapshot)
	if preview != nil && s.cacheDAO != nil {
		if setErr := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, preview); setErr != nil {
			log.Printf("回填排程预览缓存失败 chat_id=%s: %v", normalizedChatID, setErr)
		}
	}
	return preview
}

// convertRefineStateToPlanState 把 schedulerefine 状态映射为 scheduleplan 状态。
//
// 设计意图：
// 1. 复用现有 saveSchedulePlanPreview 写入链路，减少重复落盘代码；
// 2. 仅映射“预览持久化必须字段”，避免把 refine 运行期临时字段带入存储层；
// 3. 后续如要扩展 refine 专属快照字段，可在该映射处集中演进。
func convertRefineStateToPlanState(st *schedulerefine.ScheduleRefineState) *scheduleplan.SchedulePlanState {
	if st == nil {
		return nil
	}
	adjustmentScope := "medium"
	if st.Contract.Strategy == "keep" {
		adjustmentScope = "small"
	}
	return &scheduleplan.SchedulePlanState{
		TraceID:         strings.TrimSpace(st.TraceID),
		UserID:          st.UserID,
		ConversationID:  strings.TrimSpace(st.ConversationID),
		UserIntent:      strings.TrimSpace(st.UserIntent),
		Constraints:     append([]string(nil), st.Constraints...),
		TaskClassIDs:    append([]int(nil), st.TaskClassIDs...),
		Strategy:        "steady",
		AdjustmentScope: adjustmentScope,
		IsAdjustment:    true,

		HybridEntries:  append([]model.HybridScheduleEntry(nil), st.HybridEntries...),
		AllocatedItems: cloneTaskClassItems(st.AllocatedItems),
		CandidatePlans: cloneWeekSchedules(st.CandidatePlans),

		FinalSummary: strings.TrimSpace(st.FinalSummary),
		Completed:    st.Completed,
	}
}

// shouldPersistScheduleRefinePreview 判断“本轮微调结果是否应覆盖上一版预览”。
//
// 职责边界：
// 1. 默认沿用原有 refine 持久化策略，保证普通 ReAct 微调链路不受影响；
// 2. 仅当“独立复合分支已直接出站，但终审未通过”时，拒绝覆盖上一版预览；
// 3. 这样可以避免外层把未经验证的复合结果当成新的基线继续滚动微调。
func shouldPersistScheduleRefinePreview(st *schedulerefine.ScheduleRefineState) bool {
	if st == nil {
		return false
	}
	if st.CompositeRouteSucceeded && !schedulerefine.FinalHardCheckPassed(st) {
		return false
	}
	return true
}
