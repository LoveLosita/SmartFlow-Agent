package agentsvc

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"gorm.io/gorm"
)

// GetConversationTimeline 返回指定会话的统一时间线（正文+卡片）列表。
//
// 职责边界：
// 1. 只读，不修改会话状态；
// 2. 顺序以 seq 为准，保证刷新后可稳定重建；
// 3. 优先读 Redis 时间线缓存，未命中再回源 MySQL。
func (s *AgentService) GetConversationTimeline(ctx context.Context, userID int, chatID string) ([]model.GetConversationTimelineItem, error) {
	normalizedChatID := normalizeConversationID(chatID)
	if userID <= 0 || strings.TrimSpace(normalizedChatID) == "" {
		return nil, gorm.ErrRecordNotFound
	}

	exists, err := s.repo.IfChatExists(ctx, userID, normalizedChatID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, gorm.ErrRecordNotFound
	}

	if s.cacheDAO != nil {
		cacheItems, cacheErr := s.cacheDAO.GetConversationTimelineFromCache(ctx, userID, normalizedChatID)
		if cacheErr == nil && cacheItems != nil {
			return normalizeConversationTimelineItems(cacheItems), nil
		}
		if cacheErr != nil {
			log.Printf("读取会话时间线缓存失败 user=%d chat=%s err=%v", userID, normalizedChatID, cacheErr)
		}
	}

	events, err := s.repo.ListConversationTimelineEvents(ctx, userID, normalizedChatID)
	if err != nil {
		return nil, err
	}
	items := buildConversationTimelineItemsFromDB(events)

	if s.cacheDAO != nil {
		if err := s.cacheDAO.SetConversationTimelineToCache(ctx, userID, normalizedChatID, items); err != nil {
			log.Printf("回填会话时间线缓存失败 user=%d chat=%s err=%v", userID, normalizedChatID, err)
		}
		if len(items) > 0 {
			if err := s.cacheDAO.SetConversationTimelineSeq(ctx, userID, normalizedChatID, items[len(items)-1].Seq); err != nil {
				log.Printf("回填会话时间线 seq 失败 user=%d chat=%s err=%v", userID, normalizedChatID, err)
			}
		}
	}

	return normalizeConversationTimelineItems(items), nil
}

// appendConversationTimelineEvent 统一追加单条时间线事件到 Redis + MySQL。
//
// 步骤化说明：
// 1. 先从 Redis INCR 分配 seq，若 Redis 异常则回退 DB MAX(seq)+1；
// 2. 再写 MySQL，保证刷新时至少有权威持久化；
// 3. 最后追加 Redis 时间线列表，失败只记日志，不影响主链路返回；
// 4. 返回分配到的 seq，便于后续扩展在 SSE meta 回传顺序号。
func (s *AgentService) appendConversationTimelineEvent(
	ctx context.Context,
	userID int,
	chatID string,
	kind string,
	role string,
	content string,
	payload map[string]any,
	tokensConsumed int,
) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, errors.New("agent service is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedChatID := strings.TrimSpace(chatID)
	normalizedRole := strings.TrimSpace(role)
	normalizedKind := canonicalizeTimelineKind(kind, normalizedRole)
	normalizedContent := strings.TrimSpace(content)
	if userID <= 0 || normalizedChatID == "" || normalizedKind == "" {
		return 0, errors.New("invalid timeline event identity")
	}

	seq, err := s.nextConversationTimelineSeq(ctx, userID, normalizedChatID)
	if err != nil {
		return 0, err
	}

	payloadJSON := marshalTimelinePayloadJSON(payload)
	persistPayload := model.ChatTimelinePersistPayload{
		UserID:         userID,
		ConversationID: normalizedChatID,
		Seq:            seq,
		Kind:           normalizedKind,
		Role:           normalizedRole,
		Content:        normalizedContent,
		PayloadJSON:    payloadJSON,
		TokensConsumed: tokensConsumed,
	}
	eventID, eventCreatedAt, err := s.repo.SaveConversationTimelineEvent(ctx, persistPayload)
	if err != nil {
		// 1. 并发极端场景下（例如 Redis seq 分配失败后 DB 兜底）可能产生重复 seq；
		// 2. 这里做一次“读取最新 MAX(seq)+1”的重试，避免主链路直接失败；
		// 3. 重试仍失败则返回错误，让调用方感知真实落库失败。
		if !isTimelineSeqConflictError(err) {
			return 0, err
		}
		maxSeq, seqErr := s.repo.GetConversationTimelineMaxSeq(ctx, userID, normalizedChatID)
		if seqErr != nil {
			return 0, err
		}
		persistPayload.Seq = maxSeq + 1
		var retryErr error
		eventID, eventCreatedAt, retryErr = s.repo.SaveConversationTimelineEvent(ctx, persistPayload)
		if retryErr != nil {
			return 0, retryErr
		}
		seq = persistPayload.Seq
		if s.cacheDAO != nil {
			if setErr := s.cacheDAO.SetConversationTimelineSeq(ctx, userID, normalizedChatID, seq); setErr != nil {
				log.Printf("时间线 seq 冲突重试后回写 Redis 失败 user=%d chat=%s seq=%d err=%v", userID, normalizedChatID, seq, setErr)
			}
		}
	}

	if s.cacheDAO != nil {
		now := time.Now()
		item := model.GetConversationTimelineItem{
			ID:             eventID,
			Seq:            seq,
			Kind:           normalizedKind,
			Role:           normalizedRole,
			Content:        normalizedContent,
			Payload:        cloneTimelinePayload(payload),
			TokensConsumed: tokensConsumed,
		}
		if eventCreatedAt != nil {
			item.CreatedAt = eventCreatedAt
		} else {
			item.CreatedAt = &now
		}
		if err := s.cacheDAO.AppendConversationTimelineEventToCache(ctx, userID, normalizedChatID, item); err != nil {
			log.Printf("追加会话时间线缓存失败 user=%d chat=%s seq=%d kind=%s err=%v", userID, normalizedChatID, seq, normalizedKind, err)
		}
	}
	return seq, nil
}

func isTimelineSeqConflictError(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "duplicate") && strings.Contains(text, "uk_timeline_user_chat_seq")
}

// persistNewAgentTimelineExtraEvent 把 SSE extra 卡片事件写入时间线。
//
// 说明：
// 1. 只持久化真正需要刷新后重建的卡片事件；
// 2. status/reasoning/finish 等临时过程信号不落时间线；
// 3. 失败只记日志，不中断当前 SSE 输出。
func (s *AgentService) persistNewAgentTimelineExtraEvent(ctx context.Context, userID int, chatID string, extra *newagentstream.OpenAIChunkExtra) {
	kind, ok := mapTimelineKindFromStreamExtra(extra)
	if !ok {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}

	if _, err := s.appendConversationTimelineEvent(
		ctx,
		userID,
		chatID,
		kind,
		"",
		"",
		buildTimelinePayloadFromStreamExtra(extra),
		0,
	); err != nil {
		log.Printf("写入 newAgent 卡片时间线失败 user=%d chat=%s kind=%s err=%v", userID, chatID, kind, err)
	}
}

func (s *AgentService) nextConversationTimelineSeq(ctx context.Context, userID int, chatID string) (int64, error) {
	if s.cacheDAO != nil {
		seq, err := s.cacheDAO.IncrConversationTimelineSeq(ctx, userID, chatID)
		if err == nil {
			return seq, nil
		}
		log.Printf("会话时间线 seq Redis 分配失败，回退 DB user=%d chat=%s err=%v", userID, chatID, err)
	}

	maxSeq, err := s.repo.GetConversationTimelineMaxSeq(ctx, userID, chatID)
	if err != nil {
		return 0, err
	}
	seq := maxSeq + 1
	if s.cacheDAO != nil {
		if err := s.cacheDAO.SetConversationTimelineSeq(ctx, userID, chatID, seq); err != nil {
			log.Printf("会话时间线 seq 回填 Redis 失败 user=%d chat=%s seq=%d err=%v", userID, chatID, seq, err)
		}
	}
	return seq, nil
}

func buildConversationTimelineItemsFromDB(events []model.AgentTimelineEvent) []model.GetConversationTimelineItem {
	if len(events) == 0 {
		return make([]model.GetConversationTimelineItem, 0)
	}
	items := make([]model.GetConversationTimelineItem, 0, len(events))
	for _, event := range events {
		item := model.GetConversationTimelineItem{
			ID:             event.ID,
			Seq:            event.Seq,
			Kind:           strings.TrimSpace(event.Kind),
			TokensConsumed: event.TokensConsumed,
			CreatedAt:      event.CreatedAt,
		}
		if event.Role != nil {
			item.Role = strings.TrimSpace(*event.Role)
		}
		if event.Content != nil {
			item.Content = strings.TrimSpace(*event.Content)
		}
		if event.Payload != nil {
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(*event.Payload)), &payload); err == nil && len(payload) > 0 {
				item.Payload = payload
			}
		}
		items = append(items, item)
	}
	return normalizeConversationTimelineItems(items)
}

// normalizeConversationTimelineItems 统一收敛 timeline 的 kind/role 口径，避免前端切分失效。
func normalizeConversationTimelineItems(items []model.GetConversationTimelineItem) []model.GetConversationTimelineItem {
	if len(items) == 0 {
		return make([]model.GetConversationTimelineItem, 0)
	}
	normalized := make([]model.GetConversationTimelineItem, 0, len(items))
	for _, item := range items {
		role := strings.ToLower(strings.TrimSpace(item.Role))
		kind := canonicalizeTimelineKind(item.Kind, role)

		// kind 缺失时尝试从 role 反推文本类型，保障“用户分段锚点”可用。
		if kind == "" {
			switch role {
			case "user":
				kind = model.AgentTimelineKindUserText
			case "assistant":
				kind = model.AgentTimelineKindAssistantText
			}
		}
		// role 缺失时按文本类型补齐，减少前端额外兼容判断。
		if role == "" {
			switch kind {
			case model.AgentTimelineKindUserText:
				role = "user"
			case model.AgentTimelineKindAssistantText:
				role = "assistant"
			}
		}

		item.Kind = kind
		item.Role = role
		normalized = append(normalized, item)
	}
	return normalized
}

// canonicalizeTimelineKind 统一 kind 别名，收敛到文档定义值。
func canonicalizeTimelineKind(kind string, role string) string {
	normalizedKind := strings.ToLower(strings.TrimSpace(kind))
	normalizedRole := strings.ToLower(strings.TrimSpace(role))
	switch normalizedKind {
	case model.AgentTimelineKindUserText,
		model.AgentTimelineKindAssistantText,
		model.AgentTimelineKindToolCall,
		model.AgentTimelineKindToolResult,
		model.AgentTimelineKindConfirmRequest,
		model.AgentTimelineKindBusinessCard,
		model.AgentTimelineKindScheduleCompleted:
		return normalizedKind
	case "text", "message", "query":
		if normalizedRole == "user" {
			return model.AgentTimelineKindUserText
		}
		if normalizedRole == "assistant" {
			return model.AgentTimelineKindAssistantText
		}
		return normalizedKind
	default:
		return normalizedKind
	}
}

func marshalTimelinePayloadJSON(payload map[string]any) string {
	if len(payload) == 0 {
		return ""
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return string(data)
}

func cloneTimelinePayload(payload map[string]any) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func mapTimelineKindFromStreamExtra(extra *newagentstream.OpenAIChunkExtra) (string, bool) {
	if extra == nil {
		return "", false
	}
	switch extra.Kind {
	case newagentstream.StreamExtraKindToolCall:
		return model.AgentTimelineKindToolCall, true
	case newagentstream.StreamExtraKindToolResult:
		return model.AgentTimelineKindToolResult, true
	case newagentstream.StreamExtraKindConfirm:
		return model.AgentTimelineKindConfirmRequest, true
	case newagentstream.StreamExtraKindBusinessCard:
		return model.AgentTimelineKindBusinessCard, true
	case newagentstream.StreamExtraKindScheduleCompleted:
		return model.AgentTimelineKindScheduleCompleted, true
	default:
		return "", false
	}
}

func buildTimelinePayloadFromStreamExtra(extra *newagentstream.OpenAIChunkExtra) map[string]any {
	if extra == nil {
		return nil
	}
	payload := map[string]any{
		"stage":        strings.TrimSpace(extra.Stage),
		"block_id":     strings.TrimSpace(extra.BlockID),
		"display_mode": string(extra.DisplayMode),
	}
	if extra.Tool != nil {
		payload["tool"] = map[string]any{
			"name":              strings.TrimSpace(extra.Tool.Name),
			"status":            strings.TrimSpace(extra.Tool.Status),
			"summary":           strings.TrimSpace(extra.Tool.Summary),
			"arguments_preview": strings.TrimSpace(extra.Tool.ArgumentsPreview),
		}
	}
	if extra.Confirm != nil {
		payload["confirm"] = map[string]any{
			"interaction_id": strings.TrimSpace(extra.Confirm.InteractionID),
			"title":          strings.TrimSpace(extra.Confirm.Title),
			"summary":        strings.TrimSpace(extra.Confirm.Summary),
		}
	}
	if extra.Interrupt != nil {
		payload["interrupt"] = map[string]any{
			"interaction_id": strings.TrimSpace(extra.Interrupt.InteractionID),
			"type":           strings.TrimSpace(extra.Interrupt.Type),
			"summary":        strings.TrimSpace(extra.Interrupt.Summary),
		}
	}
	if extra.BusinessCard != nil {
		payload["business_card"] = cloneStreamBusinessCard(extra.BusinessCard)
	}
	if len(extra.Meta) > 0 {
		payload["meta"] = cloneTimelinePayload(extra.Meta)
	}
	return payload
}

func cloneStreamBusinessCard(card *newagentstream.StreamBusinessCardExtra) map[string]any {
	if card == nil {
		return nil
	}
	cloned := map[string]any{
		"card_type": strings.TrimSpace(card.CardType),
		"title":     strings.TrimSpace(card.Title),
		"summary":   strings.TrimSpace(card.Summary),
		"source":    strings.TrimSpace(card.Source),
	}
	if len(card.Data) > 0 {
		cloned["data"] = cloneTimelinePayload(card.Data)
	}
	return cloned
}
