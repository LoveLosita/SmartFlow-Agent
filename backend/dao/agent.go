package dao

import (
	"context"
	"errors"

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
	userChat := model.ChatHistory{
		UserID:         userID,
		MessageContent: &message,
		Role:           &role,
		ChatID:         conversationID,
	}
	if err := a.db.WithContext(ctx).Create(&userChat).Error; err != nil {
		return err
	}
	return nil
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
	err := a.db.WithContext(ctx).Where("user_id = ? AND chat_id = ?", userID, chatID).Order("created_at desc").Limit(limit).Find(&histories).Error
	if err != nil {
		return nil, err
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
