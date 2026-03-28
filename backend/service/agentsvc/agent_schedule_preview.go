package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent/model"
	agentshared "github.com/LoveLosita/smartflow/backend/agent/shared"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

// saveSchedulePlanPreview 负责把排程结果同步写入“查询预览”所需的缓存与快照。
//
// 职责边界：
// 1. 负责把 graph 最终状态映射为统一预览 DTO，并先写 Redis、再写 MySQL 快照。
// 2. 负责执行“失败不阻断主回复”的旁路持久化策略，避免影响聊天主链路。
// 3. 不负责 SSE 输出，不负责聊天消息落库，也不负责 refine 状态到 plan 状态的转换。
func (s *AgentService) saveSchedulePlanPreview(ctx context.Context, userID int, chatID string, finalState *agentmodel.SchedulePlanState) {
	// 1. 先做最小前置校验，避免把空状态或空会话写成脏快照。
	if s == nil || finalState == nil {
		return
	}
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return
	}

	// 2. 组装统一预览缓存结构。
	// 2.1 summary 为空时使用统一兜底文案，保证查询接口始终有稳定输出。
	// 2.2 所有切片字段都做深拷贝，避免缓存与 graph state 共享底层数组。
	preview := &model.SchedulePlanPreviewCache{
		UserID:         userID,
		ConversationID: normalizedChatID,
		TraceID:        strings.TrimSpace(finalState.TraceID),
		Summary:        schedulePlanSummaryOrFallback(strings.TrimSpace(finalState.FinalSummary)),
		CandidatePlans: cloneWeekSchedules(finalState.CandidatePlans),
		TaskClassIDs:   append([]int(nil), finalState.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(finalState.HybridEntries),
		AllocatedItems: cloneTaskClassItems(finalState.AllocatedItems),
		GeneratedAt:    time.Now(),
	}

	// 3. 先写 Redis 预览，保证前端查询链路优先命中低时延缓存。
	// 3.1 Redis 写失败只记日志，不中断主流程；
	// 3.2 真正兜底由后续 MySQL 快照承担。
	if s.cacheDAO != nil {
		if err := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, preview); err != nil {
			log.Printf("写入排程预览缓存失败 chat_id=%s: %v", normalizedChatID, err)
		}
	}

	// 4. 再写 MySQL 快照，保证缓存失效后仍能恢复预览与连续微调上下文。
	// 4.1 这里继续采用“同步写快照”的策略，因为下一轮 refine 依赖强一致读取；
	// 4.2 写库失败同样只记日志，避免让用户侧回复因为旁路持久化失败而中断。
	if s.repo != nil {
		snapshot := buildSchedulePlanSnapshotFromState(userID, normalizedChatID, finalState)
		if err := s.repo.UpsertScheduleStateSnapshot(ctx, snapshot); err != nil {
			log.Printf("写入排程状态快照失败 chat_id=%s: %v", normalizedChatID, err)
		}
	}
}

// GetSchedulePlanPreview 按 conversation_id 读取结构化排程预览。
//
// 职责边界：
// 1. 负责参数归一化、缓存优先读取、会话归属校验和 DB 兜底。
// 2. 负责把缓存/快照 DTO 转成接口响应 DTO。
// 3. 不负责触发排程，不负责补算结果，也不负责消息链路落库。
func (s *AgentService) GetSchedulePlanPreview(ctx context.Context, userID int, chatID string) (*model.GetSchedulePlanPreviewResponse, error) {
	// 1. 先校验会话参数，避免无效请求打到缓存或数据库。
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return nil, respond.MissingParam
	}
	if s == nil {
		return nil, errors.New("agent service is not initialized")
	}

	// 2. 优先查 Redis。
	// 2.1 命中后立即校验 user_id，避免把别人的会话预览泄露给当前用户；
	// 2.2 缓存异常直接上抛，由接口层统一处理错误响应。
	if s.cacheDAO != nil {
		preview, err := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, normalizedChatID)
		if err != nil {
			return nil, err
		}
		if preview != nil {
			if preview.UserID > 0 && preview.UserID != userID {
				return nil, respond.SchedulePlanPreviewNotFound
			}
			plans := cloneWeekSchedules(preview.CandidatePlans)
			if plans == nil {
				plans = make([]model.UserWeekSchedule, 0)
			}
			return &model.GetSchedulePlanPreviewResponse{
				ConversationID: normalizedChatID,
				TraceID:        strings.TrimSpace(preview.TraceID),
				Summary:        strings.TrimSpace(preview.Summary),
				CandidatePlans: plans,
				GeneratedAt:    preview.GeneratedAt,
			}, nil
		}
	}

	// 3. Redis 未命中时回源 MySQL。
	// 3.1 命中快照后顺手回填 Redis，提高后续命中率；
	// 3.2 DB 未命中才真正返回 not found，避免缓存过期造成假阴性。
	if s.repo != nil {
		snapshot, err := s.repo.GetScheduleStateSnapshot(ctx, userID, normalizedChatID)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			response := snapshotToSchedulePlanPreviewResponse(snapshot)
			if s.cacheDAO != nil {
				cachePreview := snapshotToSchedulePlanPreviewCache(snapshot)
				if setErr := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, cachePreview); setErr != nil {
					log.Printf("回填排程预览缓存失败 chat_id=%s: %v", normalizedChatID, setErr)
				}
			}
			return response, nil
		}
	}

	return nil, respond.SchedulePlanPreviewNotFound
}

// cloneWeekSchedules 负责深拷贝周视图排程，避免缓存与运行态共享底层切片。
func cloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	return agentshared.CloneWeekSchedules(src)
}

// cloneHybridEntries 负责深拷贝混合排程条目，避免跨请求污染。
func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	return agentshared.CloneHybridEntries(src)
}

// cloneTaskClassItems 负责深拷贝任务项切片，包含内部指针字段的安全复制。
func cloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	return agentshared.CloneTaskClassItems(src)
}

// buildSchedulePlanSnapshotFromState 把 graph 最终状态映射成可持久化的快照 DTO。
//
// 职责边界：
// 1. 负责字段归一化、深拷贝和 state_version 补齐。
// 2. 不负责数据库写入，也不负责生成业务摘要文案。
func buildSchedulePlanSnapshotFromState(userID int, conversationID string, st *agentmodel.SchedulePlanState) *model.SchedulePlanStateSnapshot {
	if st == nil {
		return nil
	}
	return &model.SchedulePlanStateSnapshot{
		UserID:           userID,
		ConversationID:   conversationID,
		StateVersion:     model.SchedulePlanStateVersionV1,
		TaskClassIDs:     append([]int(nil), st.TaskClassIDs...),
		Constraints:      append([]string(nil), st.Constraints...),
		HybridEntries:    cloneHybridEntries(st.HybridEntries),
		AllocatedItems:   cloneTaskClassItems(st.AllocatedItems),
		CandidatePlans:   cloneWeekSchedules(st.CandidatePlans),
		UserIntent:       strings.TrimSpace(st.UserIntent),
		Strategy:         strings.TrimSpace(st.Strategy),
		AdjustmentScope:  strings.TrimSpace(st.AdjustmentScope),
		RestartRequested: st.RestartRequested,
		FinalSummary:     strings.TrimSpace(st.FinalSummary),
		Completed:        st.Completed,
		TraceID:          strings.TrimSpace(st.TraceID),
	}
}

// snapshotToSchedulePlanPreviewCache 把 MySQL 快照映射成 Redis 预览缓存结构。
func snapshotToSchedulePlanPreviewCache(snapshot *model.SchedulePlanStateSnapshot) *model.SchedulePlanPreviewCache {
	if snapshot == nil {
		return nil
	}
	generatedAt := snapshot.UpdatedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return &model.SchedulePlanPreviewCache{
		UserID:         snapshot.UserID,
		ConversationID: snapshot.ConversationID,
		TraceID:        strings.TrimSpace(snapshot.TraceID),
		Summary:        schedulePlanSummaryOrFallback(strings.TrimSpace(snapshot.FinalSummary)),
		CandidatePlans: cloneWeekSchedules(snapshot.CandidatePlans),
		TaskClassIDs:   append([]int(nil), snapshot.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(snapshot.HybridEntries),
		AllocatedItems: cloneTaskClassItems(snapshot.AllocatedItems),
		GeneratedAt:    generatedAt,
	}
}

// snapshotToSchedulePlanPreviewResponse 把 MySQL 快照映射成查询接口响应结构。
func snapshotToSchedulePlanPreviewResponse(snapshot *model.SchedulePlanStateSnapshot) *model.GetSchedulePlanPreviewResponse {
	if snapshot == nil {
		return nil
	}
	plans := cloneWeekSchedules(snapshot.CandidatePlans)
	if plans == nil {
		plans = make([]model.UserWeekSchedule, 0)
	}
	generatedAt := snapshot.UpdatedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return &model.GetSchedulePlanPreviewResponse{
		ConversationID: snapshot.ConversationID,
		TraceID:        strings.TrimSpace(snapshot.TraceID),
		Summary:        schedulePlanSummaryOrFallback(strings.TrimSpace(snapshot.FinalSummary)),
		CandidatePlans: plans,
		GeneratedAt:    generatedAt,
	}
}

// schedulePlanSummaryOrFallback 统一收口排程摘要兜底文案，避免各处重复维护默认值。
func schedulePlanSummaryOrFallback(summary string) string {
	if strings.TrimSpace(summary) == "" {
		return "排程流程已完成，但未生成结果摘要。"
	}
	return summary
}
