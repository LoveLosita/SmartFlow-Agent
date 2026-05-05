package sv

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
	eventsvc "github.com/LoveLosita/smartflow/backend/services/runtime/eventsvc"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
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

// appendConversationTimelineEvent 统一追加单条时间线事件到 Redis + outbox。
//
// 步骤化说明：
// 1. 先分配同会话内单调递增的 seq，优先走 Redis，Redis 不可用时回退 DB；
// 2. 再把事件同步追加到 Redis timeline cache，保证刷新前的用户体验连续；
// 3. 最后发布 outbox 事件异步落 MySQL，与 chat history 的可靠落库方式对齐；
// 4. 未注入 eventPublisher 时走同步 MySQL fallback，方便本地极简环境启动。
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

	normalizedContent, normalizedPayload, shouldPersist := normalizeConversationTimelinePersistMaterial(normalizedKind, normalizedContent, payload)
	if !shouldPersist {
		return 0, nil
	}

	seq, err := s.nextConversationTimelineSeq(ctx, userID, normalizedChatID)
	if err != nil {
		return 0, err
	}

	persistPayload := (model.ChatTimelinePersistPayload{
		UserID:         userID,
		ConversationID: normalizedChatID,
		Seq:            seq,
		Kind:           normalizedKind,
		Role:           normalizedRole,
		Content:        normalizedContent,
		PayloadJSON:    marshalTimelinePayloadJSON(normalizedPayload),
		TokensConsumed: tokensConsumed,
	}).Normalize()
	if s.eventPublisher != nil {
		now := time.Now()

		// 1. 先写 Redis timeline cache，让刷新前的本地态和下一轮上下文都能立即看到这条事件。
		// 2. 再发布 outbox 事件，与 chat history 保持相同的“入队成功即返回”语义。
		// 3. 若 outbox 发布失败，这里返回 error 交给上层处理，不在本方法里偷偷回退成同步写库。
		s.appendConversationTimelineCacheNonBlocking(
			ctx,
			userID,
			normalizedChatID,
			buildConversationTimelineCacheItem(0, seq, normalizedKind, normalizedRole, normalizedContent, normalizedPayload, tokensConsumed, &now),
		)
		if err := eventsvc.PublishAgentTimelinePersistRequested(ctx, s.eventPublisher, persistPayload); err != nil {
			return 0, err
		}
		return seq, nil
	}
	return s.appendConversationTimelineEventSync(ctx, userID, normalizedChatID, persistPayload, normalizedPayload)
}

// appendConversationTimelineEventSync 在未启用 outbox 时同步写 MySQL。
//
// 步骤化说明：
// 1. 本方法只作为 eventPublisher 为空时的降级路径，保证本地环境不依赖总线；
// 2. 若 seq 唯一键冲突，读取 DB 最大 seq 后补一个新序号，语义与 outbox 消费者保持一致；
// 3. MySQL 写入成功后再追加 Redis cache，让缓存拿到数据库生成的 id/created_at。
func (s *AgentService) appendConversationTimelineEventSync(
	ctx context.Context,
	userID int,
	chatID string,
	persistPayload model.ChatTimelinePersistPayload,
	payload map[string]any,
) (int64, error) {
	eventID, eventCreatedAt, err := s.repo.SaveConversationTimelineEvent(ctx, persistPayload)
	if err != nil {
		// 1. 这里的冲突通常来自 Redis seq key 过期或落后于 DB。
		// 2. 由于当前是同步写库链路，可以直接读取 DB 当前最大 seq 并补一个新序号。
		// 3. 若重试后仍失败，则把数据库错误原样抛给上层，避免悄悄吞掉真实问题。
		if !model.IsTimelineSeqConflictError(err) {
			return 0, err
		}
		maxSeq, seqErr := s.repo.GetConversationTimelineMaxSeq(ctx, userID, chatID)
		if seqErr != nil {
			return 0, seqErr
		}
		persistPayload.Seq = maxSeq + 1
		eventID, eventCreatedAt, err = s.repo.SaveConversationTimelineEvent(ctx, persistPayload)
		if err != nil {
			return 0, err
		}
		if s.cacheDAO != nil {
			if setErr := s.cacheDAO.SetConversationTimelineSeq(ctx, userID, chatID, persistPayload.Seq); setErr != nil {
				log.Printf("回填时间线 seq 到 Redis 失败 user=%d chat=%s seq=%d err=%v", userID, chatID, persistPayload.Seq, setErr)
			}
		}
	}

	s.appendConversationTimelineCacheNonBlocking(
		ctx,
		userID,
		chatID,
		buildConversationTimelineCacheItem(
			eventID,
			persistPayload.Seq,
			persistPayload.Kind,
			persistPayload.Role,
			persistPayload.Content,
			payload,
			persistPayload.TokensConsumed,
			eventCreatedAt,
		),
	)
	return persistPayload.Seq, nil
}

// appendConversationTimelineCacheNonBlocking 尽力把单条 timeline 事件追加到 Redis。
//
// 步骤化说明：
// 1. 缓存失败不能反向影响主链路，因为 MySQL/outbox 才是最终可靠写入；
// 2. 这里统一记录错误日志，方便排查 Redis 不可用或 payload 序列化问题；
// 3. item 由调用方提前标准化，本方法不再二次裁剪业务字段。
func (s *AgentService) appendConversationTimelineCacheNonBlocking(
	ctx context.Context,
	userID int,
	chatID string,
	item model.GetConversationTimelineItem,
) {
	if s.cacheDAO == nil {
		return
	}
	if err := s.cacheDAO.AppendConversationTimelineEventToCache(ctx, userID, chatID, item); err != nil {
		log.Printf("追加时间线缓存失败 user=%d chat=%s seq=%d kind=%s err=%v", userID, chatID, item.Seq, item.Kind, err)
	}
}

// nextConversationTimelineSeq 负责分配一条新的 timeline seq。
//
// 步骤化说明：
// 1. 优先走 Redis INCR，避免所有事件都串行依赖 MySQL；
// 2. 再用 DB MAX(seq) 做一次自检，尽量把“Redis key 过期/落后”在写入前提前修正；
// 3. 若 Redis 不可用，则直接回退到 DB MAX(seq)+1，并把结果尽力回填回 Redis。
func (s *AgentService) nextConversationTimelineSeq(ctx context.Context, userID int, chatID string) (int64, error) {
	if s == nil || s.repo == nil {
		return 0, errors.New("agent service is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	normalizedChatID := strings.TrimSpace(chatID)
	if userID <= 0 || normalizedChatID == "" {
		return 0, errors.New("invalid timeline seq identity")
	}

	if s.cacheDAO == nil {
		return s.nextConversationTimelineSeqFromDB(ctx, userID, normalizedChatID)
	}

	candidateSeq, err := s.cacheDAO.IncrConversationTimelineSeq(ctx, userID, normalizedChatID)
	if err != nil {
		log.Printf("分配时间线 seq 时 Redis INCR 失败，回退 DB user=%d chat=%s err=%v", userID, normalizedChatID, err)
		return s.nextConversationTimelineSeqFromDB(ctx, userID, normalizedChatID)
	}

	// 1. Redis key 缺失时，INCR 常会从 1 重新开始，容易和已有 DB 记录撞 seq。
	// 2. 这里额外对照一次 DB 最大 seq，把明显落后的顺序号提前修正，降低 outbox 消费时的补 seq 概率。
	// 3. 该自检不会看到“尚未消费到 MySQL 的新 outbox 事件”，因此真正的极端并发兜底仍由消费者承担。
	maxSeq, err := s.repo.GetConversationTimelineMaxSeq(ctx, userID, normalizedChatID)
	if err != nil {
		return 0, err
	}
	if candidateSeq > maxSeq {
		return candidateSeq, nil
	}

	repairedSeq := maxSeq + 1
	if err = s.cacheDAO.SetConversationTimelineSeq(ctx, userID, normalizedChatID, repairedSeq); err != nil {
		log.Printf("修正时间线 seq 到 Redis 失败 user=%d chat=%s seq=%d err=%v", userID, normalizedChatID, repairedSeq, err)
	}
	return repairedSeq, nil
}

func (s *AgentService) nextConversationTimelineSeqFromDB(ctx context.Context, userID int, chatID string) (int64, error) {
	maxSeq, err := s.repo.GetConversationTimelineMaxSeq(ctx, userID, chatID)
	if err != nil {
		return 0, err
	}
	nextSeq := maxSeq + 1
	if s.cacheDAO != nil {
		if setErr := s.cacheDAO.SetConversationTimelineSeq(ctx, userID, chatID, nextSeq); setErr != nil {
			log.Printf("回填时间线 seq 到 Redis 失败 user=%d chat=%s seq=%d err=%v", userID, chatID, nextSeq, setErr)
		}
	}
	return nextSeq, nil
}

// normalizeConversationTimelinePersistMaterial 负责把 timeline 原始输入收敛成“可缓存 + 可持久化”的口径。
//
// 职责边界：
// 1. 对普通事件只做浅拷贝，避免调用方后续继续改 map 影响已入队 payload；
// 2. 对 thinking_summary 只保留 detail_summary 与必要 metadata，明确剔除 short_summary；
// 3. 若 thinking_summary 最终没有 detail_summary，则返回 shouldPersist=false，仅保留实时 SSE 展示，不进入 timeline。
func normalizeConversationTimelinePersistMaterial(kind string, content string, payload map[string]any) (string, map[string]any, bool) {
	normalizedKind := strings.ToLower(strings.TrimSpace(kind))
	normalizedContent := strings.TrimSpace(content)
	if normalizedKind != model.AgentTimelineKindThinkingSummary {
		return normalizedContent, cloneTimelinePayload(payload), true
	}
	return sanitizeThinkingSummaryPersistMaterial(normalizedContent, payload)
}

func sanitizeThinkingSummaryPersistMaterial(content string, payload map[string]any) (string, map[string]any, bool) {
	detailSummary := readTimelinePayloadString(payload, "detail_summary")
	if detailSummary == "" {
		detailSummary = strings.TrimSpace(content)
	}
	if detailSummary == "" {
		return "", nil, false
	}

	sanitized := make(map[string]any)
	copyTrimmedTimelinePayloadField(payload, sanitized, "stage")
	copyTrimmedTimelinePayloadField(payload, sanitized, "block_id")
	copyTrimmedTimelinePayloadField(payload, sanitized, "display_mode")
	copyTimelinePayloadFieldIfPresent(payload, sanitized, "summary_seq")
	copyTimelinePayloadFieldIfPresent(payload, sanitized, "final")
	copyTimelinePayloadFieldIfPresent(payload, sanitized, "duration_seconds")
	sanitized["detail_summary"] = detailSummary

	return detailSummary, sanitized, true
}

func copyTrimmedTimelinePayloadField(src map[string]any, dst map[string]any, key string) {
	if len(src) == 0 || dst == nil {
		return
	}
	value, ok := src[key]
	if !ok {
		return
	}
	text, ok := value.(string)
	if !ok {
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	dst[key] = trimmed
}

func copyTimelinePayloadFieldIfPresent(src map[string]any, dst map[string]any, key string) {
	if len(src) == 0 || dst == nil {
		return
	}
	value, ok := src[key]
	if !ok || value == nil {
		return
	}
	dst[key] = value
}

// persistAgentTimelineExtraEvent 把 SSE extra 里的结构化事件写入时间线。
//
// 说明：
// 1. 只持久化刷新后仍需重建的业务事件；
// 2. short_summary 这类临时展示信息会在 appendConversationTimelineEvent 内被过滤掉；
// 3. 失败只记日志，不反向打断当前 SSE 输出。
func (s *AgentService) persistAgentTimelineExtraEvent(
	ctx context.Context,
	userID int,
	chatID string,
	extra *agentstream.OpenAIChunkExtra,
) {
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
		log.Printf("写入 agent 时间线事件失败 user=%d chat=%s kind=%s err=%v", userID, chatID, kind, err)
	}
}

func buildConversationTimelineCacheItem(
	eventID int64,
	seq int64,
	kind string,
	role string,
	content string,
	payload map[string]any,
	tokensConsumed int,
	createdAt *time.Time,
) model.GetConversationTimelineItem {
	item := model.GetConversationTimelineItem{
		ID:             eventID,
		Seq:            seq,
		Kind:           kind,
		Role:           role,
		Content:        content,
		Payload:        cloneTimelinePayload(payload),
		TokensConsumed: tokensConsumed,
	}
	if createdAt != nil {
		item.CreatedAt = createdAt
	}
	return item
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
		model.AgentTimelineKindScheduleCompleted,
		model.AgentTimelineKindThinkingSummary:
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

func mapTimelineKindFromStreamExtra(extra *agentstream.OpenAIChunkExtra) (string, bool) {
	if extra == nil {
		return "", false
	}
	if isThinkingSummaryStreamExtra(extra) {
		return model.AgentTimelineKindThinkingSummary, true
	}
	switch extra.Kind {
	case agentstream.StreamExtraKindToolCall:
		return model.AgentTimelineKindToolCall, true
	case agentstream.StreamExtraKindToolResult:
		return model.AgentTimelineKindToolResult, true
	case agentstream.StreamExtraKindConfirm:
		return model.AgentTimelineKindConfirmRequest, true
	case agentstream.StreamExtraKindBusinessCard:
		return model.AgentTimelineKindBusinessCard, true
	case agentstream.StreamExtraKindScheduleCompleted:
		return model.AgentTimelineKindScheduleCompleted, true
	default:
		return "", false
	}
}

func buildTimelinePayloadFromStreamExtra(extra *agentstream.OpenAIChunkExtra) map[string]any {
	if extra == nil {
		return nil
	}
	if isThinkingSummaryStreamExtra(extra) {
		return buildThinkingSummaryTimelinePayload(extra)
	}
	payload := map[string]any{
		"stage":        strings.TrimSpace(extra.Stage),
		"block_id":     strings.TrimSpace(extra.BlockID),
		"display_mode": string(extra.DisplayMode),
	}
	if extra.Tool != nil {
		toolPayload := map[string]any{
			"name":              strings.TrimSpace(extra.Tool.Name),
			"status":            strings.TrimSpace(extra.Tool.Status),
			"summary":           strings.TrimSpace(extra.Tool.Summary),
			"arguments_preview": strings.TrimSpace(extra.Tool.ArgumentsPreview),
		}
		if len(extra.Tool.ArgumentView) > 0 {
			toolPayload["argument_view"] = cloneTimelinePayload(extra.Tool.ArgumentView)
		}
		if len(extra.Tool.ResultView) > 0 {
			toolPayload["result_view"] = cloneTimelinePayload(extra.Tool.ResultView)
		}
		payload["tool"] = toolPayload
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

func isThinkingSummaryStreamExtra(extra *agentstream.OpenAIChunkExtra) bool {
	if extra == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(extra.Kind)), model.AgentTimelineKindThinkingSummary)
}

func buildThinkingSummaryTimelinePayload(extra *agentstream.OpenAIChunkExtra) map[string]any {
	payload := map[string]any{
		"stage":        strings.TrimSpace(extra.Stage),
		"block_id":     strings.TrimSpace(extra.BlockID),
		"display_mode": string(extra.DisplayMode),
	}

	if extra.ThinkingSummary != nil {
		summary := extra.ThinkingSummary
		payload["summary_seq"] = summary.SummarySeq
		payload["final"] = summary.Final
		payload["duration_seconds"] = summary.DurationSeconds
		if detailSummary := strings.TrimSpace(summary.DetailSummary); detailSummary != "" {
			payload["detail_summary"] = detailSummary
		}
		return payload
	}

	if detailSummary := readTimelineExtraMetaString(extra.Meta, "detail_summary"); detailSummary != "" {
		payload["detail_summary"] = detailSummary
	}
	return payload
}

func readTimelineExtraMetaString(meta map[string]any, key string) string {
	if len(meta) == 0 {
		return ""
	}
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func readTimelinePayloadString(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	raw, ok := payload[key]
	if !ok {
		return ""
	}
	text, ok := raw.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func cloneStreamBusinessCard(card *agentstream.StreamBusinessCardExtra) map[string]any {
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
