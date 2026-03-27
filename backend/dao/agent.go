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

// saveChatHistoryCore 鏄€滆亰澶╂秷鎭惤搴?+ 浼氳瘽缁熻鏇存柊鈥濈殑鏍稿績瀹炵幇銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 鍙墽琛屽綋鍓?DAO 鍙ユ焺涓婄殑鏁版嵁搴撳啓鍏ュ姩浣滐紱
// 2. 涓嶄富鍔ㄥ紑鍚簨鍔★紙浜嬪姟鐢辫皟鐢ㄦ柟鍐冲畾锛夛紱
// 3. 淇濊瘉 chat_histories 涓?agent_chats.message_count 鐨勪竴鑷存€у彛寰勩€?
//
// 澶辫触澶勭悊锛?
// 1. 浠讳竴姝ラ澶辫触閮借繑鍥?error锛?
// 2. 鑻ヨ皟鐢ㄦ柟澶勪簬浜嬪姟涓紝杩斿洖 error 浼氳Е鍙戜簨鍔″洖婊氥€?
func (a *AgentDAO) saveChatHistoryCore(ctx context.Context, userID int, conversationID string, role, message, reasoningContent string, reasoningDurationSeconds int, retryGroupID *string, retryIndex *int, retryFromUserMessageID *int, retryFromAssistantMessageID *int, tokensConsumed int) error {
	// 0. token 鍏ュ簱鍓嶅厹搴曪細璐熸暟缁熶竴褰掗浂锛岄伩鍏嶅紓甯稿€兼薄鏌撶疮璁＄粺璁°€?
	if tokensConsumed < 0 {
		tokensConsumed = 0
	}
	reasoningContent = strings.TrimSpace(reasoningContent)
	if reasoningDurationSeconds < 0 {
		reasoningDurationSeconds = 0
	}

	// 1. 鍏堝啓 chat_histories 鍘熷娑堟伅銆?
	var reasoningContentPtr *string
	if reasoningContent != "" {
		reasoningContentPtr = &reasoningContent
	}
	userChat := model.ChatHistory{
		UserID:                      userID,
		MessageContent:              &message,
		ReasoningContent:            reasoningContentPtr,
		ReasoningDurationSeconds:    reasoningDurationSeconds,
		RetryGroupID:                retryGroupID,
		RetryIndex:                  retryIndex,
		RetryFromUserMessageID:      retryFromUserMessageID,
		RetryFromAssistantMessageID: retryFromAssistantMessageID,
		Role:                        &role,
		ChatID:                      conversationID,
		TokensConsumed:              tokensConsumed,
	}
	if err := a.db.WithContext(ctx).Create(&userChat).Error; err != nil {
		return err
	}

	// 2. 鍐嶆洿鏂颁細璇濈粺璁★細
	// 2.1 message_count +1锛屼繚鎸佸拰 chat_histories 琛屾暟鍙ｅ緞涓€鑷达紱
	// 2.2 tokens_total 绱姞鏈潯娑堟伅 token锛?
	// 2.3 last_message_at 鍒锋柊涓哄綋鍓嶆椂闂达紝渚涗細璇濇帓搴忎娇鐢ㄣ€?
	now := time.Now()
	updates := map[string]interface{}{
		"message_count":   gorm.Expr("message_count + ?", 1),
		"tokens_total":    gorm.Expr("tokens_total + ?", tokensConsumed),
		"last_message_at": &now,
	}
	result := a.db.WithContext(ctx).Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, conversationID).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 浼氳瘽涓嶅瓨鍦ㄦ椂鐩存帴澶辫触锛岄伩鍏嶅嚭鐜扳€滃鍎垮巻鍙叉秷鎭€濄€?
		return fmt.Errorf("conversation not found when updating stats: user_id=%d chat_id=%s", userID, conversationID)
	}

	// 3. 鏈€鍚庢洿鏂?users.token_usage锛堝悓涓€浜嬪姟鍐咃級锛?
	// 3.1 鍙湪 tokensConsumed>0 鏃舵墽琛岋紝閬垮厤鏃犳剰涔夊啓鍏ワ紱
	// 3.2 鍜?chat_histories/agent_chats 鏀惧湪鍚屼竴浜嬪姟閲岋紝淇濊瘉缁熻鍙ｅ緞鍘熷瓙涓€鑷达紱
	// 3.3 鑻ョ敤鎴疯涓嶅瓨鍦ㄥ垯杩斿洖閿欒锛岃Е鍙戜簨鍔″洖婊氾紝闃叉鍑虹幇鈥滀細璇濈粺璁℃垚鍔熶絾鐢ㄦ埛缁熻涓㈠け鈥濄€?
	if tokensConsumed > 0 {
		userUpdate := a.db.WithContext(ctx).
			Model(&model.User{}).
			Where("id = ?", userID).
			Update("token_usage", gorm.Expr("token_usage + ?", tokensConsumed))
		if userUpdate.Error != nil {
			return userUpdate.Error
		}
		if userUpdate.RowsAffected == 0 {
			return fmt.Errorf("user not found when updating token usage: user_id=%d", userID)
		}
	}
	return nil
}

// SaveChatHistoryInTx 鍦ㄨ皟鐢ㄦ柟鈥滃凡寮€鍚簨鍔♀€濈殑鍦烘櫙涓嬪啓鍏ヨ亰澶╁巻鍙层€?
//
// 璁捐鐩殑锛?
// 1. 缁欐湇鍔″眰缁勫悎澶氫釜 DAO 鎿嶄綔鏃跺鐢紝閬垮厤宓屽浜嬪姟锛?
// 2. 璁?outbox 娑堣垂澶勭悊鍣ㄥ彲浠ュ拰涓氬姟鍐欏叆鍏变韩鍚屼竴涓?tx銆?
func (a *AgentDAO) SaveChatHistoryInTx(ctx context.Context, userID int, conversationID string, role, message, reasoningContent string, reasoningDurationSeconds int, retryGroupID *string, retryIndex *int, retryFromUserMessageID *int, retryFromAssistantMessageID *int, tokensConsumed int) error {
	return a.saveChatHistoryCore(ctx, userID, conversationID, role, message, reasoningContent, reasoningDurationSeconds, retryGroupID, retryIndex, retryFromUserMessageID, retryFromAssistantMessageID, tokensConsumed)
}

// SaveChatHistory 鍦ㄥ悓姝ョ洿鍐欒矾寰勪笅鍐欏叆鑱婂ぉ鍘嗗彶銆?
//
// 璇存槑锛?
// 1. 璇ユ柟娉曚細鑷寮€鍚簨鍔★紱
// 2. 鍐呴儴澶嶇敤 saveChatHistoryCore锛岀‘淇濆拰 SaveChatHistoryInTx 鐨勪笟鍔″彛寰勫畬鍏ㄤ竴鑷淬€?
func (a *AgentDAO) SaveChatHistory(ctx context.Context, userID int, conversationID string, role, message, reasoningContent string, reasoningDurationSeconds int, retryGroupID *string, retryIndex *int, retryFromUserMessageID *int, retryFromAssistantMessageID *int, tokensConsumed int) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return a.WithTx(tx).saveChatHistoryCore(ctx, userID, conversationID, role, message, reasoningContent, reasoningDurationSeconds, retryGroupID, retryIndex, retryFromUserMessageID, retryFromAssistantMessageID, tokensConsumed)
	})
}

// adjustTokenUsageCore 鍦ㄥ悓涓€浜嬪姟璇箟涓嬪仛鈥滀細璇?鐢ㄦ埛鈥漷oken 璐︽湰澧為噺璋冩暣銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 鍙洿鏂?agent_chats.tokens_total 涓?users.token_usage锛?
// 2. 涓嶅啓 chat_histories锛堟秷鎭惤搴撶敱 SaveChatHistory* 璺緞璐熻矗锛夛紱
// 3. deltaTokens<=0 鏃惰涓烘棤鎿嶄綔锛岀洿鎺ヨ繑鍥炪€?
func (a *AgentDAO) adjustTokenUsageCore(ctx context.Context, userID int, conversationID string, deltaTokens int) error {
	if deltaTokens <= 0 {
		return nil
	}

	// 1. 鍏堟洿鏂颁細璇濈疮璁?token銆?
	chatUpdate := a.db.WithContext(ctx).
		Model(&model.AgentChat{}).
		Where("user_id = ? AND chat_id = ?", userID, conversationID).
		Update("tokens_total", gorm.Expr("tokens_total + ?", deltaTokens))
	if chatUpdate.Error != nil {
		return chatUpdate.Error
	}
	if chatUpdate.RowsAffected == 0 {
		return fmt.Errorf("conversation not found when adjusting tokens: user_id=%d chat_id=%s", userID, conversationID)
	}

	// 2. 鍐嶆洿鏂扮敤鎴风疮璁?token銆?
	userUpdate := a.db.WithContext(ctx).
		Model(&model.User{}).
		Where("id = ?", userID).
		Update("token_usage", gorm.Expr("token_usage + ?", deltaTokens))
	if userUpdate.Error != nil {
		return userUpdate.Error
	}
	if userUpdate.RowsAffected == 0 {
		return fmt.Errorf("user not found when adjusting token usage: user_id=%d", userID)
	}
	return nil
}

// AdjustTokenUsageInTx 鍦ㄨ皟鐢ㄦ柟宸插紑鍚簨鍔℃椂鎵ц token 璐︽湰澧為噺璋冩暣銆?
func (a *AgentDAO) AdjustTokenUsageInTx(ctx context.Context, userID int, conversationID string, deltaTokens int) error {
	return a.adjustTokenUsageCore(ctx, userID, conversationID, deltaTokens)
}

// AdjustTokenUsage 鍦ㄥ悓姝ヨ矾寰勪笅鎵ц token 璐︽湰澧為噺璋冩暣锛堝唴閮ㄨ嚜甯︿簨鍔★級銆?
func (a *AgentDAO) AdjustTokenUsage(ctx context.Context, userID int, conversationID string, deltaTokens int) error {
	return a.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return a.WithTx(tx).adjustTokenUsageCore(ctx, userID, conversationID, deltaTokens)
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
	// 淇濈暀鈥滄渶杩?N 鏉♀€濆悗锛屽弽杞垚鏃堕棿姝ｅ簭锛屾柟渚挎ā鍨嬫秷璐广€?
	for i, j := 0, len(histories)-1; i < j; i, j = i+1, j-1 {
		histories[i], histories[j] = histories[j], histories[i]
	}
	return histories, nil
}

func (a *AgentDAO) EnsureRetryGroupSeed(ctx context.Context, userID int, chatID, retryGroupID string, sourceUserMessageID, sourceAssistantMessageID int) error {
	normalizedGroupID := strings.TrimSpace(retryGroupID)
	if normalizedGroupID == "" {
		return nil
	}

	indexOne := 1
	ids := make([]int, 0, 2)
	if sourceUserMessageID > 0 {
		ids = append(ids, sourceUserMessageID)
	}
	if sourceAssistantMessageID > 0 {
		ids = append(ids, sourceAssistantMessageID)
	}
	if len(ids) == 0 {
		return nil
	}

	return a.db.WithContext(ctx).
		Model(&model.ChatHistory{
			UserID: userID,
			ChatID: chatID,
		}).
		Where("user_id = ? AND chat_id = ? AND id IN ?", userID, chatID, ids).
		Where("(retry_group_id IS NULL OR retry_group_id = '')").
		Updates(map[string]any{
			"retry_group_id": normalizedGroupID,
			"retry_index":    indexOne,
		}).Error
}

func (a *AgentDAO) GetRetryGroupNextIndex(ctx context.Context, userID int, chatID, retryGroupID string) (int, error) {
	normalizedGroupID := strings.TrimSpace(retryGroupID)
	if normalizedGroupID == "" {
		return 0, errors.New("retry_group_id is empty")
	}

	var maxIndex int
	if err := a.db.WithContext(ctx).
		Model(&model.ChatHistory{}).
		Where("user_id = ? AND chat_id = ? AND retry_group_id = ?", userID, chatID, normalizedGroupID).
		Select("COALESCE(MAX(retry_index), 0)").
		Scan(&maxIndex).Error; err != nil {
		return 0, err
	}
	return maxIndex + 1, nil
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

// GetConversationMeta 鏌ヨ鍗曚釜浼氳瘽鍏冧俊鎭€?
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

// GetConversationTitle 璇诲彇褰撳墠浼氳瘽鏍囬銆?
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

// UpdateConversationTitleIfEmpty 浠呭湪鏍囬涓虹┖鏃舵洿鏂颁細璇濇爣棰樸€?
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

// GetConversationList 鎸夊垎椤垫煡璇㈡寚瀹氱敤鎴风殑浼氳瘽鍒楄〃銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 鍙礋璐ｈ搴擄紝涓嶈礋璐ｇ紦瀛橈紱
// 2. 鍙礋璐?user_id 鏁版嵁闅旂锛屼笉璐熻矗鍙傛暟鍚堟硶鎬у厹搴曪紙鐢?service 璐熻矗锛夛紱
// 3. 杩斿洖鎬绘暟 total 渚涗笂灞傝绠?has_more銆?
func (a *AgentDAO) GetConversationList(ctx context.Context, userID, page, pageSize int, status string) ([]model.AgentChat, int64, error) {
	// 1. 鍏堟瀯閫犵粺涓€杩囨护鏉′欢锛屼繚璇?total 涓?list 鐨勭粺璁″彛寰勪竴鑷淬€?
	baseQuery := a.db.WithContext(ctx).Model(&model.AgentChat{}).Where("user_id = ?", userID)
	if strings.TrimSpace(status) != "" {
		baseQuery = baseQuery.Where("status = ?", status)
	}

	// 2. 鍏堟煡鎬绘潯鏁帮紝缁欏墠绔垎椤靛櫒鎻愪緵瀹屾暣鍏冧俊鎭€?
	var total int64
	if err := baseQuery.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return make([]model.AgentChat, 0), 0, nil
	}

	// 3. 鍐嶆煡褰撳墠椤垫暟鎹細
	// 3.1 鎸夋渶杩戞秷鎭椂闂村€掑簭锛屼繚璇佲€滄渶杩戞椿璺冣€濅紭鍏堝睍绀猴紱
	// 3.2 鍚屾椂闂存埑涓嬫寜 id 鍊掑簭锛岄伩鍏嶇炕椤垫椂椤哄簭鎶栧姩銆?
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
