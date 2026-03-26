package agentsvc

import (
	"context"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/conv"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// GetConversationHistory 返回指定会话的聊天历史。
//
// 职责边界：
// 1. 负责会话 ID 归一化、会话归属校验，以及“先 Redis、后 DB”的读取编排；
// 2. 负责把缓存消息 / DB 记录统一转换为 API 响应 DTO；
// 3. 不负责补写会话标题，也不负责修改聊天主链路的缓存写入策略。
func (s *AgentService) GetConversationHistory(ctx context.Context, userID int, chatID string) ([]model.GetConversationHistoryItem, error) {
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return nil, respond.MissingParam
	}

	// 1. 先做归属校验：
	// 1.1 Redis 历史缓存只按 chat_id 分桶，不能单靠缓存判断用户归属；
	// 1.2 因此先查会话是否属于当前用户，避免命中别人会话缓存时产生越权读取；
	// 1.3 若会话不存在，统一返回 gorm.ErrRecordNotFound，交由 API 层映射为参数错误。
	exists, err := s.repo.IfChatExists(ctx, userID, normalizedChatID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}

	// 2. 优先读 Redis：
	// 2.1 命中时直接返回，复用当前聊天主链路维护的最近消息窗口；
	// 2.2 失败策略：缓存读取异常只记日志并继续回源 DB，避免缓存抖动导致接口不可用；
	// 2.3 注意：缓存消息不包含稳定的 DB 主键与创建时间，因此这些字段允许为空。
	if s.agentCache != nil {
		history, cacheErr := s.agentCache.GetHistory(ctx, normalizedChatID)
		if cacheErr != nil {
			log.Printf("读取会话历史缓存失败 chat_id=%s: %v", normalizedChatID, cacheErr)
		} else if history != nil && !cacheConversationHistoryHasRetryMetadata(history) {
			return buildConversationHistoryItemsFromCache(history), nil
		}
	}

	// 3. Redis 未命中时回源 DB：
	// 3.1 复用现有 GetUserChatHistories 读取最近 N 条历史，保证查询链路和主聊天链路口径一致；
	// 3.2 失败时直接上抛，由 API 层统一处理；
	// 3.3 成功后若缓存可用，则顺手回填 Redis，降低后续冷启动成本。
	histories, err := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel("worker"), normalizedChatID)
	if err != nil {
		return nil, err
	}

	if s.agentCache != nil {
		if setErr := s.agentCache.BackfillHistory(ctx, normalizedChatID, conv.ToEinoMessages(histories)); setErr != nil {
			log.Printf("回填会话历史缓存失败 chat_id=%s: %v", normalizedChatID, setErr)
		}
	}

	return buildConversationHistoryItemsFromDB(histories), nil
}

// buildConversationHistoryItemsFromCache 把 Redis 中的 Eino 消息转换为接口响应。
//
// 职责边界：
// 1. 只做字段映射，不做权限校验或排序调整；
// 2. 不补 created_at/id，因为当前缓存模型不承载这两个字段；
// 3. role 统一输出为 user / assistant / system，避免前端再感知 schema.RoleType。
func buildConversationHistoryItemsFromCache(messages []*schema.Message) []model.GetConversationHistoryItem {
	items := make([]model.GetConversationHistoryItem, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		items = append(items, model.GetConversationHistoryItem{
			Role:                     normalizeConversationHistoryRole(string(msg.Role)),
			Content:                  strings.TrimSpace(msg.Content),
			ReasoningContent:         strings.TrimSpace(msg.ReasoningContent),
			ReasoningDurationSeconds: extractConversationReasoningDurationSeconds(msg),
			RetryGroupID:             extractConversationRetryGroupID(msg),
			RetryIndex:               extractConversationRetryIndex(msg),
		})
	}
	return attachConversationRetryTotals(items)
}

// buildConversationHistoryItemsFromDB 把数据库聊天记录转换为接口响应。
//
// 职责边界：
// 1. 只透传 DB 已有字段，不尝试补算 reasoning_content；
// 2. message_content / role 为空时兜底为空串与 system，避免空指针影响接口；
// 3. 保持 DAO 返回的时间正序，前端可直接渲染。
func buildConversationHistoryItemsFromDB(histories []model.ChatHistory) []model.GetConversationHistoryItem {
	items := make([]model.GetConversationHistoryItem, 0, len(histories))
	for _, history := range histories {
		content := ""
		if history.MessageContent != nil {
			content = strings.TrimSpace(*history.MessageContent)
		}

		role := "system"
		if history.Role != nil {
			role = normalizeConversationHistoryRole(*history.Role)
		}

		items = append(items, model.GetConversationHistoryItem{
			ID:                       history.ID,
			Role:                     role,
			Content:                  content,
			CreatedAt:                history.CreatedAt,
			ReasoningContent:         strings.TrimSpace(derefConversationHistoryText(history.ReasoningContent)),
			ReasoningDurationSeconds: history.ReasoningDurationSeconds,
			RetryGroupID:             cloneConversationStringPointer(history.RetryGroupID),
			RetryIndex:               cloneConversationIntPointer(history.RetryIndex),
		})
	}
	return attachConversationRetryTotals(items)
}

func derefConversationHistoryText(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

func extractConversationReasoningDurationSeconds(msg *schema.Message) int {
	if msg == nil || msg.Extra == nil {
		return 0
	}
	raw, ok := msg.Extra["reasoning_duration_seconds"]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

func extractConversationRetryGroupID(msg *schema.Message) *string {
	if msg == nil || msg.Extra == nil {
		return nil
	}
	raw, ok := msg.Extra["retry_group_id"]
	if !ok {
		return nil
	}
	text, ok := raw.(string)
	if !ok {
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return &text
}

func extractConversationRetryIndex(msg *schema.Message) *int {
	if msg == nil || msg.Extra == nil {
		return nil
	}
	raw, ok := msg.Extra["retry_index"]
	if !ok {
		return nil
	}
	switch v := raw.(type) {
	case int:
		if v <= 0 {
			return nil
		}
		return &v
	case int32:
		value := int(v)
		if value <= 0 {
			return nil
		}
		return &value
	case int64:
		value := int(v)
		if value <= 0 {
			return nil
		}
		return &value
	case float64:
		value := int(v)
		if value <= 0 {
			return nil
		}
		return &value
	default:
		return nil
	}
}

func attachConversationRetryTotals(items []model.GetConversationHistoryItem) []model.GetConversationHistoryItem {
	if len(items) == 0 {
		return items
	}
	groupTotals := make(map[string]int)
	for _, item := range items {
		if item.RetryGroupID == nil || item.RetryIndex == nil {
			continue
		}
		groupID := strings.TrimSpace(*item.RetryGroupID)
		if groupID == "" {
			continue
		}
		if *item.RetryIndex > groupTotals[groupID] {
			groupTotals[groupID] = *item.RetryIndex
		}
	}
	for idx := range items {
		groupIDPtr := items[idx].RetryGroupID
		if groupIDPtr == nil {
			continue
		}
		groupID := strings.TrimSpace(*groupIDPtr)
		total := groupTotals[groupID]
		if total <= 0 {
			continue
		}
		totalCopy := total
		items[idx].RetryTotal = &totalCopy
	}
	return items
}

func cloneConversationStringPointer(src *string) *string {
	if src == nil {
		return nil
	}
	text := strings.TrimSpace(*src)
	if text == "" {
		return nil
	}
	return &text
}

func cloneConversationIntPointer(src *int) *int {
	if src == nil || *src <= 0 {
		return nil
	}
	value := *src
	return &value
}

func normalizeConversationHistoryRole(role string) string {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return "system"
	}
}

func cacheConversationHistoryHasRetryMetadata(messages []*schema.Message) bool {
	for _, msg := range messages {
		if extractConversationRetryGroupID(msg) != nil {
			return true
		}
	}
	return false
}
