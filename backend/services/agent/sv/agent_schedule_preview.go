package sv

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	agentshared "github.com/LoveLosita/smartflow/backend/services/agent/shared"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

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
	if s.cacheDAO != nil {
		preview, err := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, normalizedChatID)
		if err != nil {
			return nil, err
		}
		if preview != nil {
			if preview.UserID > 0 && preview.UserID != userID {
				return nil, respond.SchedulePlanPreviewNotFound
			}
			plans := agentshared.CloneWeekSchedules(preview.CandidatePlans)
			if plans == nil {
				plans = make([]model.UserWeekSchedule, 0)
			}
			return &model.GetSchedulePlanPreviewResponse{
				ConversationID: normalizedChatID,
				TraceID:        strings.TrimSpace(preview.TraceID),
				Summary:        strings.TrimSpace(preview.Summary),
				CandidatePlans: plans,
				HybridEntries:  agentshared.CloneHybridEntries(preview.HybridEntries),
				TaskClassIDs:   preview.TaskClassIDs,
				GeneratedAt:    preview.GeneratedAt,
			}, nil
		}
	}

	// 3. Redis 未命中时回源 MySQL。
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
		CandidatePlans: agentshared.CloneWeekSchedules(snapshot.CandidatePlans),
		TaskClassIDs:   append([]int(nil), snapshot.TaskClassIDs...),
		HybridEntries:  agentshared.CloneHybridEntries(snapshot.HybridEntries),
		AllocatedItems: agentshared.CloneTaskClassItems(snapshot.AllocatedItems),
		GeneratedAt:    generatedAt,
	}
}

// snapshotToSchedulePlanPreviewResponse 把 MySQL 快照映射成查询接口响应结构。
func snapshotToSchedulePlanPreviewResponse(snapshot *model.SchedulePlanStateSnapshot) *model.GetSchedulePlanPreviewResponse {
	if snapshot == nil {
		return nil
	}
	plans := agentshared.CloneWeekSchedules(snapshot.CandidatePlans)
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
		HybridEntries:  agentshared.CloneHybridEntries(snapshot.HybridEntries),
		TaskClassIDs:   snapshot.TaskClassIDs,
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
