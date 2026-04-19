package dao

import (
	"context"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

// SaveConversationTimelineEvent 持久化单条会话时间线事件到 MySQL。
//
// 职责边界：
// 1. 只做单条写入，不负责 seq 分配；
// 2. 只保证字段标准化（去空格、空值置 nil），不做业务语义修正；
// 3. 返回 error 让上层决定是否中断当前链路。
func (a *AgentDAO) SaveConversationTimelineEvent(ctx context.Context, payload model.ChatTimelinePersistPayload) (int64, *time.Time, error) {
	normalizedChatID := strings.TrimSpace(payload.ConversationID)
	normalizedKind := strings.TrimSpace(payload.Kind)
	normalizedRole := strings.TrimSpace(payload.Role)
	normalizedContent := strings.TrimSpace(payload.Content)
	normalizedPayloadJSON := strings.TrimSpace(payload.PayloadJSON)

	var rolePtr *string
	if normalizedRole != "" {
		rolePtr = &normalizedRole
	}
	var contentPtr *string
	if normalizedContent != "" {
		contentPtr = &normalizedContent
	}
	var payloadPtr *string
	if normalizedPayloadJSON != "" {
		payloadPtr = &normalizedPayloadJSON
	}

	event := model.AgentTimelineEvent{
		UserID:         payload.UserID,
		ChatID:         normalizedChatID,
		Seq:            payload.Seq,
		Kind:           normalizedKind,
		Role:           rolePtr,
		Content:        contentPtr,
		Payload:        payloadPtr,
		TokensConsumed: payload.TokensConsumed,
	}
	if err := a.db.WithContext(ctx).Create(&event).Error; err != nil {
		return 0, nil, err
	}
	return event.ID, event.CreatedAt, nil
}

// ListConversationTimelineEvents 查询会话时间线，按 seq 正序返回。
func (a *AgentDAO) ListConversationTimelineEvents(ctx context.Context, userID int, chatID string) ([]model.AgentTimelineEvent, error) {
	normalizedChatID := strings.TrimSpace(chatID)
	var events []model.AgentTimelineEvent
	err := a.db.WithContext(ctx).
		Where("user_id = ? AND chat_id = ?", userID, normalizedChatID).
		Order("seq ASC").
		Order("id ASC").
		Find(&events).Error
	if err != nil {
		return nil, err
	}
	return events, nil
}

// GetConversationTimelineMaxSeq 返回会话时间线当前最大 seq。
//
// 说明：
// 1. 该方法主要用于 Redis 顺序号不可用时的 DB 兜底；
// 2. 无记录时返回 0，不视为错误；
// 3. 上层需要自行 +1 后再写入新事件。
func (a *AgentDAO) GetConversationTimelineMaxSeq(ctx context.Context, userID int, chatID string) (int64, error) {
	normalizedChatID := strings.TrimSpace(chatID)
	var maxSeq int64
	err := a.db.WithContext(ctx).
		Model(&model.AgentTimelineEvent{}).
		Where("user_id = ? AND chat_id = ?", userID, normalizedChatID).
		Select("COALESCE(MAX(seq), 0)").
		Scan(&maxSeq).Error
	if err != nil {
		return 0, err
	}
	return maxSeq, nil
}
