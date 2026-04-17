package agentsvc

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/respond"
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

	// 2. 优先读取“会话历史视图缓存”：
	// 2.1 这层缓存专门服务 conversation-history，字段口径与前端展示一致；
	// 2.2 与 Agent 上下文热缓存解耦，避免为了历史多版本而拖慢首 token；
	// 2.3 若命中则直接返回，miss 再回源 DB。
	if s.cacheDAO != nil {
		items, cacheErr := s.cacheDAO.GetConversationHistoryFromCache(ctx, userID, normalizedChatID)
		if cacheErr != nil {
			log.Printf("读取会话历史视图缓存失败 chat_id=%s: %v", normalizedChatID, cacheErr)
		} else if conversationHistoryCacheCanServe(items) {
			return items, nil
		}
	}

	// 3. Redis miss 时回源 DB：
	// 3.1 复用现有 GetUserChatHistories 读取最近 N 条历史，保证“重试版本、落库主键、创建时间”口径稳定；
	// 3.2 再把 DB 结果转换成接口 DTO，作为历史视图缓存回填；
	// 3.3 失败时直接上抛，由 API 层统一处理。
	histories, err := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel("worker"), normalizedChatID)
	if err != nil {
		return nil, err
	}

	items := buildConversationHistoryItemsFromDB(histories)

	if s.cacheDAO != nil {
		if setErr := s.cacheDAO.SetConversationHistoryToCache(ctx, userID, normalizedChatID, items); setErr != nil {
			log.Printf("回填会话历史视图缓存失败 chat_id=%s: %v", normalizedChatID, setErr)
		}
	}

	return items, nil
}

// appendConversationHistoryCacheOptimistically 把“刚生成但尚未完成 DB 持久化确认”的消息追加到历史视图缓存。
//
// 职责边界：
// 1. 只服务前端会话历史展示，不参与 Agent 上下文热缓存；
// 2. 优先复用现有历史视图缓存，miss 时再用 DB 历史做一次启动兜底；
// 3. 不保证最终权威性，最终仍以 DB 落库成功后的缓存失效与回源结果为准。
func (s *AgentService) appendConversationHistoryCacheOptimistically(
	ctx context.Context,
	userID int,
	chatID string,
	newItems ...model.GetConversationHistoryItem,
) {
	if s == nil || s.cacheDAO == nil {
		return
	}
	normalizedChatID := strings.TrimSpace(chatID)
	if userID <= 0 || normalizedChatID == "" || len(newItems) == 0 {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	// 1. 优先取历史视图缓存，避免每轮乐观追加都回源 DB。
	items, err := s.cacheDAO.GetConversationHistoryFromCache(ctx, userID, normalizedChatID)
	if err != nil {
		log.Printf("读取会话历史视图缓存失败 chat_id=%s: %v", normalizedChatID, err)
		return
	}

	// 2. 缓存 miss 时，用当前 DB 已有历史做一次基线兜底。
	// 2.1 这样即便本轮是“缓存刚被 retry 补种操作删掉”，也不会只留下最新两条消息；
	// 2.2 失败策略：DB 兜底失败只记日志并跳过，不阻塞主回复流程。
	if items == nil {
		histories, hisErr := s.repo.GetUserChatHistories(ctx, userID, pkg.HistoryFetchLimitByModel("worker"), normalizedChatID)
		if hisErr != nil {
			log.Printf("乐观追加历史缓存时回源 DB 失败 chat_id=%s: %v", normalizedChatID, hisErr)
			return
		}
		items = buildConversationHistoryItemsFromDB(histories)
	}

	merged := append([]model.GetConversationHistoryItem(nil), items...)
	for _, item := range newItems {
		merged = appendConversationHistoryItemIfMissing(merged, item)
	}
	sortConversationHistoryItems(merged)

	if err = s.cacheDAO.SetConversationHistoryToCache(ctx, userID, normalizedChatID, merged); err != nil {
		log.Printf("乐观追加会话历史视图缓存失败 chat_id=%s: %v", normalizedChatID, err)
	}
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
		})
	}
	return items
}

func derefConversationHistoryText(text *string) string {
	if text == nil {
		return ""
	}
	return *text
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

func conversationHistoryCacheCanServe(items []model.GetConversationHistoryItem) bool {
	// 1. 历史接口一旦被前端用于“重试/编辑”等二次动作，消息 id 就必须稳定可追溯。
	// 2. 乐观缓存里的新消息在 DB 落库前没有自增主键，若直接返回，会让前端拿到占位 id。
	// 3. 因此只有“缓存里的每条消息都带稳定 DB id”时，才允许直接命中缓存；否则强制回源 DB。
	for _, item := range items {
		if item.ID <= 0 {
			return false
		}
	}
	return items != nil
}

func buildOptimisticConversationHistoryItem(
	role string,
	content string,
	reasoningContent string,
	reasoningDurationSeconds int,
	createdAt time.Time,
) model.GetConversationHistoryItem {
	item := model.GetConversationHistoryItem{
		Role:                     normalizeConversationHistoryRole(role),
		Content:                  strings.TrimSpace(content),
		ReasoningContent:         strings.TrimSpace(reasoningContent),
		ReasoningDurationSeconds: reasoningDurationSeconds,
	}
	if !createdAt.IsZero() {
		t := createdAt
		item.CreatedAt = &t
	}
	return item
}

func appendConversationHistoryItemIfMissing(
	items []model.GetConversationHistoryItem,
	item model.GetConversationHistoryItem,
) []model.GetConversationHistoryItem {
	targetKey := conversationHistoryItemSignature(item)
	for _, existed := range items {
		if conversationHistoryItemSignature(existed) == targetKey {
			return items
		}
	}
	return append(items, item)
}

func conversationHistoryItemSignature(item model.GetConversationHistoryItem) string {
	if item.ID > 0 {
		return fmt.Sprintf("id:%d", item.ID)
	}

	createdAt := ""
	if item.CreatedAt != nil {
		createdAt = item.CreatedAt.UTC().Format(time.RFC3339Nano)
	}

	return fmt.Sprintf(
		"%s|%s|%s|%d|%s",
		strings.TrimSpace(item.Role),
		strings.TrimSpace(item.Content),
		strings.TrimSpace(item.ReasoningContent),
		item.ReasoningDurationSeconds,
		createdAt,
	)
}

func sortConversationHistoryItems(items []model.GetConversationHistoryItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left := conversationHistoryTimestamp(items[i])
		right := conversationHistoryTimestamp(items[j])
		if left.Equal(right) {
			return conversationHistoryItemSignature(items[i]) < conversationHistoryItemSignature(items[j])
		}
		return left.Before(right)
	})
}

func conversationHistoryTimestamp(item model.GetConversationHistoryItem) time.Time {
	if item.CreatedAt == nil {
		return time.Time{}
	}
	return *item.CreatedAt
}
