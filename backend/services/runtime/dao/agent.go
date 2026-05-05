package dao

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// saveChatHistoryCore 是"聊天消息落库 + 会话统计更新"的核心实现。
//
// 职责边界：
// 1. 只执行当前 DAO 句柄上的数据库写入动作；
// 2. 不主动开启事务（事务由调用方决定）；
// 3. 保证 chat_histories 与 agent_chats.message_count 的一致性口径。
//
// 失败处理：
// 1. 任一步骤失败都返回 error；
// 2. 若调用方处于事务中，返回 error 会触发事务回滚。
//
// 关于 retry 字段：
// 1. retry 机制已整体下线，本函数不再写入 retry_group_id / retry_index / retry_from_* 四列；
// 2. 这些列在 GORM ChatHistory 模型上暂时保留，列本身可空，历史数据不受影响；
// 3. Step B 会做 DROP COLUMN 的 migration。
func (a *AgentDAO) saveChatHistoryCore(ctx context.Context, userID int, conversationID string, role, message, reasoningContent string, reasoningDurationSeconds int, tokensConsumed int, sourceEventID string) error {
	// 0. token 入库前兜底：负数统一归零，避免异常值污染累计统计。
	if tokensConsumed < 0 {
		tokensConsumed = 0
	}
	reasoningContent = strings.TrimSpace(reasoningContent)
	if reasoningDurationSeconds < 0 {
		reasoningDurationSeconds = 0
	}
	normalizedEventID := strings.TrimSpace(sourceEventID)
	var normalizedEventIDPtr *string
	if normalizedEventID != "" {
		normalizedEventIDPtr = &normalizedEventID
		var chat model.AgentChat
		err := a.db.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("last_history_event_id").
			Where("user_id = ? AND chat_id = ?", userID, conversationID).
			First(&chat).Error
		if err != nil {
			return err
		}
		if chat.LastHistoryEventID != nil && strings.TrimSpace(*chat.LastHistoryEventID) == normalizedEventID {
			return nil
		}
	}

	// 1. 先写 chat_histories 原始消息。
	var reasoningContentPtr *string
	if reasoningContent != "" {
		reasoningContentPtr = &reasoningContent
	}
	userChat := model.ChatHistory{
		SourceEventID:            normalizedEventIDPtr,
		UserID:                   userID,
		MessageContent:           &message,
		ReasoningContent:         reasoningContentPtr,
		ReasoningDurationSeconds: reasoningDurationSeconds,
		Role:                     &role,
		ChatID:                   conversationID,
		TokensConsumed:           tokensConsumed,
	}
	if err := a.db.WithContext(ctx).Create(&userChat).Error; err != nil {
		return err
	}

	// 2. 再更新会话统计，保证 message_count / tokens_total / last_message_at 同步推进。
	now := time.Now()
	updates := map[string]interface{}{
		"message_count":   gorm.Expr("message_count + ?", 1),
		"tokens_total":    gorm.Expr("tokens_total + ?", tokensConsumed),
		"last_message_at": &now,
	}
	if normalizedEventIDPtr != nil {
		updates["last_history_event_id"] = normalizedEventIDPtr
	}
	result := a.db.WithContext(ctx).Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, conversationID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("conversation not found when updating stats: user_id=%d chat_id=%s", userID, conversationID)
	}

	return nil
}

// SaveChatHistoryInTx 在调用方"已开启事务"的场景下写入聊天历史。
//
// 设计目的：
// 1. 给服务层组合多个 DAO 操作时复用，避免嵌套事务；
// 2. 让 outbox 消费处理器可以和业务写入共享同一个 tx。
func (a *AgentDAO) SaveChatHistoryInTx(ctx context.Context, userID int, conversationID string, role, message, reasoningContent string, reasoningDurationSeconds int, tokensConsumed int, sourceEventID string) error {
	return a.saveChatHistoryCore(ctx, userID, conversationID, role, message, reasoningContent, reasoningDurationSeconds, tokensConsumed, sourceEventID)
}

// SaveChatHistory 在同步直写路径下写入聊天历史。
//
// 说明：
// 1. 该方法会自行开启事务；
// 2. 内部复用 saveChatHistoryCore，确保和 SaveChatHistoryInTx 的业务口径完全一致。
func (a *AgentDAO) SaveChatHistory(ctx context.Context, userID int, conversationID string, role, message, reasoningContent string, reasoningDurationSeconds int, tokensConsumed int, sourceEventID string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return a.WithTx(tx).saveChatHistoryCore(ctx, userID, conversationID, role, message, reasoningContent, reasoningDurationSeconds, tokensConsumed, sourceEventID)
	})
}

// adjustTokenUsageCore 在同一事务语义下做"会话"token 账本增量调整。
//
// 职责边界：
// 1. 只更新 agent_chats.tokens_total；
// 2. 不写 chat_histories（消息落库由 SaveChatHistory* 路径负责）；
// 3. deltaTokens<=0 时视为无操作，直接返回。
func (a *AgentDAO) adjustTokenUsageCore(ctx context.Context, userID int, conversationID string, deltaTokens int, eventID string) error {
	if deltaTokens <= 0 {
		return nil
	}
	normalizedEventID := strings.TrimSpace(eventID)
	var normalizedEventIDPtr *string
	if normalizedEventID != "" {
		normalizedEventIDPtr = &normalizedEventID
		var chat model.AgentChat
		err := a.db.WithContext(ctx).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("last_token_adjust_event_id").
			Where("user_id = ? AND chat_id = ?", userID, conversationID).
			First(&chat).Error
		if err != nil {
			return err
		}
		if chat.LastTokenAdjustEventID != nil && strings.TrimSpace(*chat.LastTokenAdjustEventID) == normalizedEventID {
			return nil
		}
	}

	chatUpdate := a.db.WithContext(ctx).
		Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, conversationID).
		Updates(map[string]interface{}{
			"tokens_total":               gorm.Expr("tokens_total + ?", deltaTokens),
			"last_token_adjust_event_id": normalizedEventIDPtr,
		})
	if chatUpdate.Error != nil {
		return chatUpdate.Error
	}
	if chatUpdate.RowsAffected == 0 {
		return fmt.Errorf("conversation not found when adjusting tokens: user_id=%d chat_id=%s", userID, conversationID)
	}

	return nil
}

// AdjustTokenUsageInTx 在调用方已开启事务时执行 token 账本增量调整。
func (a *AgentDAO) AdjustTokenUsageInTx(ctx context.Context, userID int, conversationID string, deltaTokens int, eventID string) error {
	return a.adjustTokenUsageCore(ctx, userID, conversationID, deltaTokens, eventID)
}

// AdjustTokenUsage 在同步路径下执行 token 账本增量调整（内部自带事务）。
func (a *AgentDAO) AdjustTokenUsage(ctx context.Context, userID int, conversationID string, deltaTokens int, eventID string) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return a.WithTx(tx).adjustTokenUsageCore(ctx, userID, conversationID, deltaTokens, eventID)
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
	// 保留"最近 N 条"后，反转成时间正序，方便模型消费。
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
	// 3.1 按最近消息时间倒序，保证"最近活跃"优先展示；
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

// ---- 压缩摘要持久化 ----
//
// 1. 旧接口 SaveCompaction / LoadCompaction 继续保留，默认只读写 execute 阶段。
// 2. 新接口按 stageKey 分桶读写，数据仍然落在 agent_chats.compaction_summary。
// 3. 为兼容历史数据，若 compaction_summary 仍是旧字符串格式，则自动回退读取。
func (a *AgentDAO) SaveCompaction(ctx context.Context, userID int, chatID string, summary string, watermark int) error {
	return a.SaveStageCompaction(ctx, userID, chatID, "execute", summary, watermark)
}

func (a *AgentDAO) LoadCompaction(ctx context.Context, userID int, chatID string) (summary string, watermark int, err error) {
	return a.LoadStageCompaction(ctx, userID, chatID, "execute")
}

// SaveContextTokenStats 保存上下文窗口 token 分布统计。
func (a *AgentDAO) SaveContextTokenStats(ctx context.Context, userID int, chatID string, statsJSON string) error {
	return a.db.WithContext(ctx).
		Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		Update("context_token_stats", statsJSON).Error
}

// LoadContextTokenStats 读取上下文窗口 token 分布统计。
func (a *AgentDAO) LoadContextTokenStats(ctx context.Context, userID int, chatID string) (string, error) {
	var chat model.AgentChat
	err := a.db.WithContext(ctx).
		Select("context_token_stats").
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		First(&chat).Error
	if err != nil {
		return "", err
	}
	if chat.ContextTokenStats != nil {
		return *chat.ContextTokenStats, nil
	}
	return "", nil
}

type stageCompactionRecord struct {
	Summary   string `json:"summary"`
	Watermark int    `json:"watermark"`
}

type stageCompactionEnvelope struct {
	Version int                              `json:"version"`
	Stages  map[string]stageCompactionRecord `json:"stages"`
}

// normalizeCompactionStageKey 统一 stageKey 的写法，避免 "Execute" 和 "execute" 被当成两个键。
func normalizeCompactionStageKey(stageKey string) string {
	key := strings.ToLower(strings.TrimSpace(stageKey))
	if key == "" {
		return "execute"
	}
	return key
}

// loadStageCompactionStages 负责把数据库里的压缩摘要统一解包成 stage -> record。
//
// 1. 先处理空值，避免后续逻辑误判。
// 2. 如果已经是 JSON envelope，就按 stage 逐项读取。
// 3. 如果还是旧版纯字符串，就把它当作 execute 阶段的兼容数据。
func loadStageCompactionStages(summary *string, watermark int) map[string]stageCompactionRecord {
	stages := map[string]stageCompactionRecord{}
	if summary == nil {
		return stages
	}

	raw := strings.TrimSpace(*summary)
	if raw == "" {
		return stages
	}

	var env stageCompactionEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err == nil && len(env.Stages) > 0 {
		for key, record := range env.Stages {
			stages[normalizeCompactionStageKey(key)] = stageCompactionRecord{
				Summary:   strings.TrimSpace(record.Summary),
				Watermark: record.Watermark,
			}
		}
		return stages
	}

	stages["execute"] = stageCompactionRecord{
		Summary:   raw,
		Watermark: watermark,
	}
	return stages
}

// marshalStageCompactionStages 负责把按阶段分桶后的摘要重新编码为 JSON envelope。
func marshalStageCompactionStages(stages map[string]stageCompactionRecord) (string, error) {
	env := stageCompactionEnvelope{
		Version: 1,
		Stages:  stages,
	}
	data, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// LoadStageCompaction 按 stageKey 读取压缩摘要和水位线。
func (a *AgentDAO) LoadStageCompaction(ctx context.Context, userID int, chatID string, stageKey string) (summary string, watermark int, err error) {
	stageKey = normalizeCompactionStageKey(stageKey)

	var chat model.AgentChat
	err = a.db.WithContext(ctx).
		Select("compaction_summary", "compaction_watermark").
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		First(&chat).Error
	if err != nil {
		return "", 0, err
	}

	stages := loadStageCompactionStages(chat.CompactionSummary, chat.CompactionWatermark)
	if record, ok := stages[stageKey]; ok {
		return record.Summary, record.Watermark, nil
	}

	return "", 0, nil
}

// SaveStageCompaction 按 stageKey 保存压缩摘要和水位线。
//
// 1. 先读取现有摘要，避免覆盖其他阶段已经写入的数据。
// 2. 再更新当前阶段对应的分桶内容。
// 3. 最后整体回写 JSON envelope，并保留 execute 阶段的 legacy watermark 兼容字段。
func (a *AgentDAO) SaveStageCompaction(ctx context.Context, userID int, chatID string, stageKey string, summary string, watermark int) error {
	stageKey = normalizeCompactionStageKey(stageKey)

	var chat model.AgentChat
	err := a.db.WithContext(ctx).
		Select("compaction_summary", "compaction_watermark").
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		First(&chat).Error
	if err != nil {
		return err
	}

	stages := loadStageCompactionStages(chat.CompactionSummary, chat.CompactionWatermark)
	stages[stageKey] = stageCompactionRecord{
		Summary:   strings.TrimSpace(summary),
		Watermark: watermark,
	}

	payload, err := marshalStageCompactionStages(stages)
	if err != nil {
		return err
	}

	legacyWatermark := watermark
	if executeRecord, ok := stages["execute"]; ok {
		legacyWatermark = executeRecord.Watermark
	}

	return a.db.WithContext(ctx).
		Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, chatID).
		Updates(map[string]any{
			"compaction_summary":   payload,
			"compaction_watermark": legacyWatermark,
		}).Error
}
