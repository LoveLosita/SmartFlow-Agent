package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/model"
	activepreview "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/preview"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	activeScheduleConversationNamespace = uuid.NewSHA1(uuid.NameSpaceURL, []byte("smartflow:active_schedule:conversation"))
	activeScheduleSessionNamespace      = uuid.NewSHA1(uuid.NameSpaceURL, []byte("smartflow:active_schedule:session"))
)

// WithActiveScheduleSessionBridge 注入主动调度 session 预创建所需的 DAO。
//
// 职责边界：
// 1. 只把 trigger -> notification 前的会话桥接能力接入 workflow；
// 2. 不改变 dry-run / preview / notification 的主状态机；
// 3. 为空时保留旧能力，便于局部测试与迁移期回退。
func WithActiveScheduleSessionBridge(agentDAO *dao.AgentDAO, sessionDAO *dao.ActiveScheduleSessionDAO) TriggerWorkflowOption {
	return func(s *TriggerWorkflowService) {
		if s == nil {
			return
		}
		s.agentDAO = agentDAO
		s.sessionDAO = sessionDAO
	}
}

// bootstrapActiveScheduleConversationInTx 负责在 notification 发出前预建会话与首屏内容。
//
// 步骤化说明：
// 1. 先生成确定性的 conversation/session ID，保证 trigger 重试时不会拆成多条会话；
// 2. 再在同一事务里创建或复用 agent_chats 和 active_schedule_sessions；
// 3. 如果是首次落库，则顺手补一条 assistant_text，必要时再补一张主动调度卡片；
// 4. 任一步失败都直接返回 error，让上层事务整体回滚，避免“通知已发但会话底稿没落”。
func (s *TriggerWorkflowService) bootstrapActiveScheduleConversationInTx(
	ctx context.Context,
	tx *gorm.DB,
	triggerRow model.ActiveScheduleTrigger,
	previewDetail activepreview.ActiveSchedulePreviewDetail,
	selectionResult selection.Result,
	now time.Time,
) error {
	if s == nil {
		return errors.New("主动调度会话桥未初始化")
	}
	if s.agentDAO == nil || s.sessionDAO == nil {
		return nil
	}
	if tx == nil {
		return errors.New("gorm tx 不能为空")
	}
	if triggerRow.ID == "" {
		return errors.New("trigger_id 不能为空")
	}

	conversationID := buildActiveScheduleConversationID(triggerRow.ID)
	sessionID := buildActiveScheduleSessionID(triggerRow.ID)
	txAgentDAO := s.agentDAO.WithTx(tx)
	txSessionDAO := s.sessionDAO.WithTx(tx)

	if err := ensureAgentConversationExists(ctx, txAgentDAO, triggerRow.UserID, conversationID); err != nil {
		return err
	}

	baseSeq, err := txAgentDAO.GetConversationTimelineMaxSeq(ctx, triggerRow.UserID, conversationID)
	if err != nil {
		return err
	}

	// 1. 只有首次创建会话时才写首屏消息，避免同一 trigger 的重试把时间线重复刷一遍。
	// 2. 若 timeline 已存在，说明这段主动调度会话已经被成功预热过，直接复用现成内容即可。
	if baseSeq == 0 {
		assistantText := resolveInitialActiveScheduleAssistantText(selectionResult, previewDetail)
		if assistantText != "" {
			if err := txAgentDAO.SaveChatHistoryInTx(ctx, triggerRow.UserID, conversationID, "assistant", assistantText, "", 0, 0, ""); err != nil {
				return err
			}
			if err := saveActiveScheduleTimelineEvent(ctx, txAgentDAO, triggerRow.UserID, conversationID, baseSeq+1, model.AgentTimelineKindAssistantText, "assistant", assistantText, nil); err != nil {
				return err
			}
			baseSeq++
		}

		if shouldSeedActiveSchedulePreviewCard(selectionResult) {
			cardPayload, err := buildActiveScheduleBusinessCardPayload(previewDetail)
			if err != nil {
				return err
			}
			if err := saveActiveScheduleTimelineEvent(ctx, txAgentDAO, triggerRow.UserID, conversationID, baseSeq+1, model.AgentTimelineKindBusinessCard, "assistant", assistantText, cardPayload); err != nil {
				return err
			}
		}
	}

	sessionSnapshot := &model.ActiveScheduleSessionSnapshot{
		SessionID:        sessionID,
		UserID:           triggerRow.UserID,
		ConversationID:   conversationID,
		TriggerID:        triggerRow.ID,
		CurrentPreviewID: strings.TrimSpace(previewDetail.PreviewID),
		Status:           resolveInitialActiveScheduleSessionStatus(selectionResult),
		State:            buildInitialActiveScheduleSessionState(selectionResult, previewDetail),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	return txSessionDAO.UpsertActiveScheduleSession(ctx, sessionSnapshot)
}

func buildActiveScheduleConversationID(triggerID string) string {
	normalized := strings.TrimSpace(triggerID)
	if normalized == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(activeScheduleConversationNamespace, []byte(normalized)).String()
}

func buildActiveScheduleSessionID(triggerID string) string {
	normalized := strings.TrimSpace(triggerID)
	if normalized == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(activeScheduleSessionNamespace, []byte(normalized)).String()
}

func ensureAgentConversationExists(ctx context.Context, agentDAO *dao.AgentDAO, userID int, conversationID string) error {
	if agentDAO == nil {
		return errors.New("agent dao 不能为空")
	}
	if userID <= 0 {
		return fmt.Errorf("invalid user_id: %d", userID)
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if normalizedConversationID == "" {
		return errors.New("conversation_id 不能为空")
	}

	exists, err := agentDAO.IfChatExists(ctx, userID, normalizedConversationID)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	_, err = agentDAO.CreateNewChat(userID, normalizedConversationID)
	return err
}

func resolveInitialActiveScheduleAssistantText(selectionResult selection.Result, previewDetail activepreview.ActiveSchedulePreviewDetail) string {
	switch selectionResult.Action {
	case selection.ActionAskUser:
		return firstNonEmptyString(
			selectionResult.AskUserQuestion,
			selectionResult.ExplanationText,
			previewDetail.Explanation,
			previewDetail.Notification,
			"请先补充主动调度需要的关键信息。",
		)
	default:
		return firstNonEmptyString(
			selectionResult.ExplanationText,
			selectionResult.NotificationSummary,
			previewDetail.Notification,
			previewDetail.Explanation,
			"主动调度建议已更新。",
		)
	}
}

func shouldSeedActiveSchedulePreviewCard(selectionResult selection.Result) bool {
	return selectionResult.Action == selection.ActionSelectCandidate
}

func resolveInitialActiveScheduleSessionStatus(selectionResult selection.Result) string {
	switch selectionResult.Action {
	case selection.ActionAskUser:
		return model.ActiveScheduleSessionStatusWaitingUserReply
	case selection.ActionSelectCandidate:
		return model.ActiveScheduleSessionStatusReadyPreview
	default:
		return model.ActiveScheduleSessionStatusIgnored
	}
}

func buildInitialActiveScheduleSessionState(
	selectionResult selection.Result,
	previewDetail activepreview.ActiveSchedulePreviewDetail,
) model.ActiveScheduleSessionState {
	state := model.ActiveScheduleSessionState{
		LastCandidateID: strings.TrimSpace(selectionResult.SelectedCandidateID),
		MissingInfo:     cloneStringSlice(previewDetail.ContextSummary.MissingInfo),
	}
	if !previewDetail.ExpiresAt.IsZero() {
		expiresAt := previewDetail.ExpiresAt
		state.ExpiresAt = &expiresAt
	}
	switch selectionResult.Action {
	case selection.ActionAskUser:
		state.PendingQuestion = firstNonEmptyString(
			selectionResult.AskUserQuestion,
			selectionResult.ExplanationText,
		)
	case selection.ActionSelectCandidate:
		state.PendingQuestion = ""
		state.MissingInfo = nil
		state.FailedReason = ""
	default:
		state.PendingQuestion = ""
		state.MissingInfo = nil
		state.ExpiresAt = nil
	}
	return state
}

func buildActiveScheduleBusinessCardPayload(detail activepreview.ActiveSchedulePreviewDetail) (map[string]any, error) {
	raw, err := json.Marshal(map[string]any{
		"business_card": map[string]any{
			"card_type": "active_schedule_preview",
			"title":     "SmartFlow 日程调整建议",
			"summary":   firstNonEmptyString(detail.Notification, detail.Explanation, detail.SelectedCandidate.Summary),
			"data":      detail,
		},
	})
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

func saveActiveScheduleTimelineEvent(
	ctx context.Context,
	agentDAO *dao.AgentDAO,
	userID int,
	conversationID string,
	seq int64,
	kind string,
	role string,
	content string,
	payload map[string]any,
) error {
	if agentDAO == nil {
		return errors.New("agent dao 不能为空")
	}
	normalizedConversationID := strings.TrimSpace(conversationID)
	if userID <= 0 || normalizedConversationID == "" {
		return errors.New("时间线事件主键不合法")
	}

	payloadJSON := ""
	if len(payload) > 0 {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		payloadJSON = string(raw)
	}

	_, _, err := agentDAO.SaveConversationTimelineEvent(ctx, model.ChatTimelinePersistPayload{
		UserID:         userID,
		ConversationID: normalizedConversationID,
		Seq:            seq,
		Kind:           kind,
		Role:           role,
		Content:        content,
		PayloadJSON:    payloadJSON,
		TokensConsumed: 0,
	})
	return err
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}
