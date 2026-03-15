package dao

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

type AgentDAO struct {
	db *gorm.DB
}

func NewAgentDAO(db *gorm.DB) *AgentDAO {
	return &AgentDAO{db: db}
}

func (a *AgentDAO) SaveChatHistory(ctx context.Context, userID int, conversationID string, role, message string) error {
	// 1. 同步落库路径也要保证“消息写入”和“会话计数更新”原子一致。
	//    因此这里使用事务，避免出现“有消息但 message_count 没加”或反过来的不一致状态。
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1.1 先写 chat_histories。
		userChat := model.ChatHistory{
			UserID:         userID,
			MessageContent: &message,
			Role:           &role,
			ChatID:         conversationID,
		}
		if err := tx.Create(&userChat).Error; err != nil {
			return err
		}

		// 1.2 再原子更新 agent_chats 的统计字段：
		//     - message_count: +1
		//     - last_message_at: 当前时间
		// 这样 message_count 语义就稳定等于“已成功落库的消息条数”。
		now := time.Now()
		updates := map[string]interface{}{
			"message_count":   gorm.Expr("message_count + ?", 1),
			"last_message_at": &now,
		}
		result := tx.Model(&model.AgentChat{}).
			Where("user_id = ? AND chat_id = ?", userID, conversationID).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			// 会话不存在视为数据不一致，回滚事务，防止产生“孤儿历史记录”。
			return fmt.Errorf("conversation not found when updating stats: user_id=%d chat_id=%s", userID, conversationID)
		}
		return nil
	})
}

func (a *AgentDAO) CreateNewChat(userID int, chatID string) (int64, error) {
	chat := model.AgentChat{
		ChatID:        chatID,
		UserID:        userID,
		MessageCount:  0,
		LastMessageAt: nil,
	}
	if err := a.db.Create(&chat).Error; err != nil {
		return 0, err
	}
	return chat.ID, nil
}

func (a *AgentDAO) GetUserChatHistories(ctx context.Context, userID, limit int, chatID string) ([]model.ChatHistory, error) {
	var histories []model.ChatHistory
	err := a.db.WithContext(ctx).
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		Order("created_at desc").
		Limit(limit).
		Find(&histories).Error
	if err != nil {
		return nil, err
	}
	// 保留“最近 N 条”的前提下，反转为时间正序，便于模型消费
	for i, j := 0, len(histories)-1; i < j; i, j = i+1, j-1 {
		histories[i], histories[j] = histories[j], histories[i]
	}
	return histories, nil
}

func (a *AgentDAO) IfChatExists(ctx context.Context, userID int, chatID string) (bool, error) {
	var chat model.AgentChat
	err := a.db.WithContext(ctx).Where("user_id = ? AND chat_id = ?", userID, chatID).First(&chat).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil // 没有找到记录，表示会话不存在
		}
		return false, err
	}
	return true, nil
}

// GetConversationMeta 查询单个会话的元信息。
// 用途：
// 1) 给前端提供“当前会话标题/消息数/最近消息时间”等展示字段；
// 2) 与流式聊天接口解耦，避免在 SSE 头部里塞动态标题。
func (a *AgentDAO) GetConversationMeta(ctx context.Context, userID int, chatID string) (*model.AgentChat, error) {
	var chat model.AgentChat
	err := a.db.WithContext(ctx).
		Select("chat_id", "title", "message_count", "last_message_at", "status").
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		First(&chat).Error
	if err != nil {
		return nil, err
	}
	return &chat, nil
}

// GetConversationTitle 读取当前会话标题。
// 返回值说明：
// 1) title：标题内容（若为空表示尚未生成）；
// 2) exists：会话是否存在；
// 3) err：数据库错误。
func (a *AgentDAO) GetConversationTitle(ctx context.Context, userID int, chatID string) (title string, exists bool, err error) {
	var chat model.AgentChat
	queryErr := a.db.WithContext(ctx).
		Select("title").
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		First(&chat).Error
	if queryErr != nil {
		if errors.Is(queryErr, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, queryErr
	}
	if chat.Title == nil {
		return "", true, nil
	}
	return strings.TrimSpace(*chat.Title), true, nil
}

// UpdateConversationTitleIfEmpty 仅在标题为空时写入会话标题。
// 设计目的：
// 1) 避免每轮对话都覆盖已有标题；
// 2) 并发下保持幂等：多个 goroutine 同时尝试写标题，最终只会成功一次。
func (a *AgentDAO) UpdateConversationTitleIfEmpty(ctx context.Context, userID int, chatID, title string) error {
	normalized := strings.TrimSpace(title)
	if normalized == "" {
		return nil
	}
	return a.db.WithContext(ctx).
		Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ? AND (title IS NULL OR title = '')", userID, chatID).
		Update("title", normalized).Error
}
