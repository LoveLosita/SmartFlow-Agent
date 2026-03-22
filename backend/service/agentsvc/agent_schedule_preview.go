package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/agent/scheduleplan"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

// saveSchedulePlanPreview 把排程结果以结构化 JSON 快照写入 Redis。
//
// 职责边界：
// 1. 负责把 finalState 中的 summary + candidate_plans 收敛为缓存 DTO；
// 2. 负责以“失败不阻断聊天主链路”的策略执行写入；
// 3. 不负责 SSE 返回协议，不负责数据库落库。
func (s *AgentService) saveSchedulePlanPreview(ctx context.Context, userID int, chatID string, finalState *scheduleplan.SchedulePlanState) {
	// 1. 基础前置校验：state 为空时直接返回，避免写入半成品快照。
	if s == nil || finalState == nil {
		return
	}
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return
	}

	// 2. 组装缓存快照：
	// 2.1 summary 优先取 final summary，空值时使用统一兜底文案；
	// 2.2 candidate_plans 做切片拷贝，避免后续引用共享导致意外覆盖；
	// 2.3 generated_at 用于前端判断“当前预览的新鲜度”。
	summary := strings.TrimSpace(finalState.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	preview := &model.SchedulePlanPreviewCache{
		UserID:         userID,
		ConversationID: normalizedChatID,
		TraceID:        strings.TrimSpace(finalState.TraceID),
		Summary:        summary,
		CandidatePlans: cloneWeekSchedules(finalState.CandidatePlans),
		TaskClassIDs:   append([]int(nil), finalState.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(finalState.HybridEntries),
		AllocatedItems: cloneTaskClassItems(finalState.AllocatedItems),
		GeneratedAt:    time.Now(),
	}

	// 3. 调用目的：先写 Redis 预览，保证前端查询接口能快速读取结构化结果。
	// 3.1 Redis 是“快路径”；失败只记录日志，不中断主链路；
	// 3.2 失败兜底由后续 MySQL 快照承接。
	if s.cacheDAO != nil {
		if err := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, preview); err != nil {
			log.Printf("写入排程预览缓存失败 chat_id=%s: %v", normalizedChatID, err)
		}
	}

	// 4. 调用目的：同步写 MySQL 状态快照，保证 Redis 失效后仍可连续微调。
	// 4.1 这里采用“同步写库”而不是 outbox：因为下一轮微调要强实时读取；
	// 4.2 快照写入失败只打日志，不阻断本轮用户回复，避免体验抖动；
	// 4.3 revision 自增由 DAO 的 upsert 冲突更新负责。
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
// 1. 负责参数归一化、缓存读取与会话归属校验；
// 2. 负责把缓存 DTO 转成 API 响应 DTO；
// 3. 不负责触发排程，不负责补算缓存。
func (s *AgentService) GetSchedulePlanPreview(ctx context.Context, userID int, chatID string) (*model.GetSchedulePlanPreviewResponse, error) {
	// 1. 参数校验：conversation_id 为空直接返回参数错误，避免无效 Redis 请求。
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return nil, respond.MissingParam
	}
	if s == nil {
		return nil, errors.New("agent service is not initialized")
	}

	// 2. 查询缓存并校验归属：
	// 2.1 缓存未命中：统一返回“预览不存在/已过期”；
	// 2.2 命中但 user_id 不一致：按未命中处理，避免泄露他人会话信息；
	// 2.3 失败兜底：缓存读异常直接上抛，由 API 层统一错误处理。
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

	// 3. Redis 未命中时回落 MySQL 快照：
	// 3.1 读取成功后直接返回，避免用户看到“预览不存在”的假阴性；
	// 3.2 若本次命中 DB 且缓存可用，则顺手回填 Redis，提升后续命中率；
	// 3.3 DB 也未命中时再返回 not found。
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

// cloneWeekSchedules 对周视图排程结果做深拷贝，避免切片引用共享。
func cloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.UserWeekSchedule, 0, len(src))
	for _, week := range src {
		eventsCopy := make([]model.WeeklyEventBrief, len(week.Events))
		copy(eventsCopy, week.Events)
		dst = append(dst, model.UserWeekSchedule{
			Week:   week.Week,
			Events: eventsCopy,
		})
	}
	return dst
}

// cloneHybridEntries 深拷贝混合条目切片，避免缓存/状态之间相互污染。
func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.HybridScheduleEntry, len(src))
	copy(dst, src)
	return dst
}

// cloneTaskClassItems 深拷贝任务块切片（包含指针字段），避免跨请求引用共享。
func cloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.TaskClassItem, 0, len(src))
	for _, item := range src {
		copied := item
		if item.CategoryID != nil {
			v := *item.CategoryID
			copied.CategoryID = &v
		}
		if item.Order != nil {
			v := *item.Order
			copied.Order = &v
		}
		if item.Content != nil {
			v := *item.Content
			copied.Content = &v
		}
		if item.Status != nil {
			v := *item.Status
			copied.Status = &v
		}
		if item.EmbeddedTime != nil {
			t := *item.EmbeddedTime
			copied.EmbeddedTime = &t
		}
		dst = append(dst, copied)
	}
	return dst
}

// buildSchedulePlanSnapshotFromState 把 graph 运行结果映射成可持久化快照 DTO。
//
// 职责边界：
// 1. 负责字段映射与深拷贝，避免跨层共享可变切片；
// 2. 负责补齐 state_version 默认值；
// 3. 不负责数据库写入（写入由 DAO 承担）。
func buildSchedulePlanSnapshotFromState(userID int, conversationID string, st *scheduleplan.SchedulePlanState) *model.SchedulePlanStateSnapshot {
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

// snapshotToSchedulePlanPreviewCache 把 MySQL 快照转换为 Redis 预览缓存结构。
func snapshotToSchedulePlanPreviewCache(snapshot *model.SchedulePlanStateSnapshot) *model.SchedulePlanPreviewCache {
	if snapshot == nil {
		return nil
	}
	summary := strings.TrimSpace(snapshot.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	generatedAt := snapshot.UpdatedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return &model.SchedulePlanPreviewCache{
		UserID:         snapshot.UserID,
		ConversationID: snapshot.ConversationID,
		TraceID:        strings.TrimSpace(snapshot.TraceID),
		Summary:        summary,
		CandidatePlans: cloneWeekSchedules(snapshot.CandidatePlans),
		TaskClassIDs:   append([]int(nil), snapshot.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(snapshot.HybridEntries),
		AllocatedItems: cloneTaskClassItems(snapshot.AllocatedItems),
		GeneratedAt:    generatedAt,
	}
}

// snapshotToSchedulePlanPreviewResponse 把 MySQL 快照转换为查询接口响应。
func snapshotToSchedulePlanPreviewResponse(snapshot *model.SchedulePlanStateSnapshot) *model.GetSchedulePlanPreviewResponse {
	if snapshot == nil {
		return nil
	}
	plans := cloneWeekSchedules(snapshot.CandidatePlans)
	if plans == nil {
		plans = make([]model.UserWeekSchedule, 0)
	}
	summary := strings.TrimSpace(snapshot.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	generatedAt := snapshot.UpdatedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return &model.GetSchedulePlanPreviewResponse{
		ConversationID: snapshot.ConversationID,
		TraceID:        strings.TrimSpace(snapshot.TraceID),
		Summary:        summary,
		CandidatePlans: plans,
		GeneratedAt:    generatedAt,
	}
}
