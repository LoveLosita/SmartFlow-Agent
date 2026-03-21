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
	// 1. 基础前置校验：任何关键依赖缺失都直接返回，避免产生无意义错误日志。
	if s == nil || s.agentCache == nil || finalState == nil {
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

	// 3. 尝试写入缓存：
	// 3.1 写入失败仅打日志，不上抛错误，保证聊天接口协议与可用性不受影响；
	// 3.2 兜底策略是“用户仍可收到文本摘要”，只是暂时无法通过新接口拉取结构化预览。
	if err := s.agentCache.SetSchedulePlanPreview(ctx, userID, normalizedChatID, preview); err != nil {
		log.Printf("写入排程预览缓存失败 chat_id=%s: %v", normalizedChatID, err)
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
	if s == nil || s.agentCache == nil {
		return nil, errors.New("agent cache is not initialized")
	}

	// 2. 查询缓存并校验归属：
	// 2.1 缓存未命中：统一返回“预览不存在/已过期”；
	// 2.2 命中但 user_id 不一致：按未命中处理，避免泄露他人会话信息；
	// 2.3 失败兜底：缓存读异常直接上抛，由 API 层统一错误处理。
	preview, err := s.agentCache.GetSchedulePlanPreview(ctx, userID, normalizedChatID)
	if err != nil {
		return nil, err
	}
	if preview == nil {
		return nil, respond.SchedulePlanPreviewNotFound
	}
	if preview.UserID > 0 && preview.UserID != userID {
		return nil, respond.SchedulePlanPreviewNotFound
	}

	// 3. 映射响应结构，保证输出字段稳定。
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
