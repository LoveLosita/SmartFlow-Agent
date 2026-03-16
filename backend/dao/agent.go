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

func (r *AgentDAO) WithTx(tx *gorm.DB) *AgentDAO {
	return &AgentDAO{db: tx}
}

// saveChatHistoryCore 是“聊天消息落库 + 会话统计更新”的核心实现。
//
// 职责边界：
// 1. 只执行当前 DAO 句柄上的数据库写入动作；
// 2. 不主动开启事务（事务由调用方决定）；
// 3. 保证 chat_histories 与 agent_chats.message_count 的一致性口径。
//
// 失败处理：
// 1. 任一步骤失败都返回 error；
// 2. 若调用方处于事务中，返回 error 会触发事务回滚。
func (a *AgentDAO) saveChatHistoryCore(ctx context.Context, userID int, conversationID string, role, message string) error {
	// 1. 先写 chat_histories 原始消息。
	userChat := model.ChatHistory{
		UserID:         userID,
		MessageContent: &message,
		Role:           &role,
		ChatID:         conversationID,
	}
	if err := a.db.WithContext(ctx).Create(&userChat).Error; err != nil {
		return err
	}

	// 2. 再更新会话统计（message_count +1, last_message_at=now）。
	now := time.Now()
	updates := map[string]interface{}{
		"message_count":   gorm.Expr("message_count + ?", 1),
		"last_message_at": &now,
	}
	result := a.db.WithContext(ctx).Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, conversationID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 会话不存在时直接失败，避免出现“孤儿历史消息”。
		return fmt.Errorf("conversation not found when updating stats: user_id=%d chat_id=%s", userID, conversationID)
	}
	return nil
}

// SaveChatHistoryInTx 在调用方“已开启事务”的场景下写入聊天历史。
//
// 设计目的：
// 1. 给服务层组合多个 DAO 操作时复用，避免嵌套事务；
// 2. 让 outbox 消费处理器可以和业务写入共享同一个 tx。
func (a *AgentDAO) SaveChatHistoryInTx(ctx context.Context, userID int, conversationID string, role, message string) error {
	return a.saveChatHistoryCore(ctx, userID, conversationID, role, message)
}

// SaveChatHistory 在同步直写路径下写入聊天历史。
//
// 说明：
// 1. 该方法会自行开启事务；
// 2. 内部复用 saveChatHistoryCore，确保和 SaveChatHistoryInTx 的业务口径完全一致。
func (a *AgentDAO) SaveChatHistory(ctx context.Context, userID int, conversationID string, role, message string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return a.WithTx(tx).saveChatHistoryCore(ctx, userID, conversationID, role, message)
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
	// 保留“最近 N 条”后，反转成时间正序，方便模型消费。
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
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetConversationMeta 查询单个会话元信息。
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

// UpdateConversationTitleIfEmpty 仅在标题为空时更新会话标题。
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

// GetConversationList 按分页查询指定用户的会话列表。
//
// 职责边界：
// 1. 只负责读库，不负责缓存；
// 2. 只负责 user_id 数据隔离，不负责参数合法性兜底（由 service 负责）；
// 3. 返回总数 total 供上层计算 has_more。
func (a *AgentDAO) GetConversationList(ctx context.Context, userID, page, pageSize int, status string) ([]model.AgentChat, int64, error) {
	// 1. 先构造统一过滤条件，保证 total 与 list 的统计口径一致。
	baseQuery := a.db.WithContext(ctx).Model(&model.AgentChat{}).Where("user_id = ?", userID)
	if strings.TrimSpace(status) != "" {
		baseQuery = baseQuery.Where("status = ?", status)
	}

	// 2. 先查总条数，给前端分页器提供完整元信息。
	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return make([]model.AgentChat, 0), 0, nil
	}

	// 3. 再查当前页数据：
	// 3.1 按最近消息时间倒序，保证“最近活跃”优先展示；
	// 3.2 同时间戳下按 id 倒序，避免翻页时顺序抖动。
	offset := (page - 1) * pageSize
	var chats []model.AgentChat
	query := a.db.WithContext(ctx).
		Model(&model.AgentChat{}).
		Select("id", "chat_id", "title", "message_count", "last_message_at", "status", "created_at").
		Where("user_id = ?", userID)
	if strings.TrimSpace(status) != "" {
		query = query.Where("status = ?", status)
	}

	if err := query.Order("last_message_at DESC").
		Order("id DESC").
		Offset(offset).
		Limit(pageSize).
		Find(&chats).Error; err != nil {
		return nil, 0, err
	}
	return chats, total, nil
}
