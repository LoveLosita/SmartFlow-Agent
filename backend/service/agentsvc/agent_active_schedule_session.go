package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/cloudwego/eino/schema"
)

// ActiveScheduleSessionRerunFunc 表示主动调度 session 被聊天入口接管后，如何同步推进 rerun。
//
// 职责边界：
// 1. 只负责把“当前 session + 用户回复”推进为新的主动调度结果；
// 2. 不负责决定 session 何时创建，也不负责通知投递；
// 3. 返回的结果只面向聊天入口的可见消息和 session 状态回写。
type ActiveScheduleSessionRerunFunc func(
	ctx context.Context,
	session *model.ActiveScheduleSessionSnapshot,
	userMessage string,
	traceID string,
	requestStart time.Time,
) (*ActiveScheduleSessionRerunResult, error)

// ActiveScheduleSessionRerunResult 是主动调度 rerun 的最小返回结果。
//
// 职责边界：
// 1. 只承载聊天入口需要回写的可见消息、业务卡片和 session 状态；
// 2. 不直接暴露 DAO 行，也不承载 worker / notification 的副作用；
// 3. AssistantText 为空时，调用方可降级为使用卡片摘要。
type ActiveScheduleSessionRerunResult struct {
	AssistantText string
	BusinessCard  *newagentstream.StreamBusinessCardExtra
	SessionState  model.ActiveScheduleSessionState
	SessionStatus string
	PreviewID     string
}

// SetActiveScheduleSessionRerunFunc 注入主动调度 rerun 入口。
func (s *AgentService) SetActiveScheduleSessionRerunFunc(fn ActiveScheduleSessionRerunFunc) {
	s.activeRerunFunc = fn
}

// loadActiveScheduleSessionByConversation 尽量从缓存 + 数据库读取当前会话的主动调度 session。
//
// 步骤化说明：
// 1. 先读 Redis 热缓存，命中则直接返回；
// 2. 缓存未命中再回源数据库，避免把 session 状态逻辑绑死在缓存上；
// 3. 回源成功后尽力回填缓存，减少下一轮聊天入口的 DB 压力。
func (s *AgentService) loadActiveScheduleSessionByConversation(ctx context.Context, userID int, chatID string) (*model.ActiveScheduleSessionSnapshot, error) {
	if s == nil || s.activeScheduleSessionDAO == nil {
		return nil, nil
	}
	normalizedChatID := strings.TrimSpace(chatID)
	if userID <= 0 || normalizedChatID == "" {
		return nil, nil
	}

	if s.cacheDAO != nil {
		cached, err := s.cacheDAO.GetActiveScheduleSessionFromConversationCache(ctx, userID, normalizedChatID)
		if err != nil {
			log.Printf("读取主动调度 session 缓存失败 user=%d chat=%s err=%v", userID, normalizedChatID, err)
		} else if cached != nil {
			return cached, nil
		}
	}

	row, err := s.activeScheduleSessionDAO.GetActiveScheduleSessionByConversationID(ctx, userID, normalizedChatID)
	if err != nil || row == nil {
		return nil, err
	}
	if s.cacheDAO != nil {
		if cacheErr := s.cacheDAO.SetActiveScheduleSessionToCache(ctx, row); cacheErr != nil {
			log.Printf("回填主动调度 session 缓存失败 user=%d chat=%s err=%v", userID, normalizedChatID, cacheErr)
		}
	}
	return row, nil
}

// persistActiveScheduleSessionBestEffort 负责把主动调度 session 的最新状态同步回 MySQL 和 Redis。
//
// 职责边界：
// 1. MySQL 是最终真相，先写表再回填缓存；
// 2. 缓存失败只记日志，不影响主流程；
// 3. 调用方需要先把 snapshot 改成最终状态，再交给这里落盘。
func (s *AgentService) persistActiveScheduleSessionBestEffort(ctx context.Context, snapshot *model.ActiveScheduleSessionSnapshot) error {
	if s == nil || s.activeScheduleSessionDAO == nil || snapshot == nil {
		return nil
	}
	if strings.TrimSpace(snapshot.SessionID) == "" {
		return errors.New("active schedule session_id 不能为空")
	}

	if err := s.activeScheduleSessionDAO.UpsertActiveScheduleSession(ctx, snapshot); err != nil {
		return err
	}

	// 1. 重新读取一遍，拿到数据库侧最终落表后的标准快照，减少缓存和 DB 的口径漂移。
	// 2. 如果重读失败，也不影响主链路返回，只要主表已成功写入即可。
	normalized, err := s.activeScheduleSessionDAO.GetActiveScheduleSessionBySessionID(ctx, snapshot.SessionID)
	if err == nil && normalized != nil {
		snapshot = normalized
	}

	if s.cacheDAO != nil {
		if cacheErr := s.cacheDAO.SetActiveScheduleSessionToCache(ctx, snapshot); cacheErr != nil {
			log.Printf("回填主动调度 session 缓存失败 session=%s err=%v", snapshot.SessionID, cacheErr)
		}
	}
	return nil
}

// persistActiveScheduleTriggerPreviewBestEffort 负责把 rerun 产生的新 preview_id 同步回 trigger。
//
// 职责边界：
// 1. 只维护 trigger -> preview 的审计指针，不修改 preview 内容，也不推进 confirm/apply 状态；
// 2. trigger_id 或 preview_id 为空时直接跳过，避免把不完整 rerun 结果写入触发记录；
// 3. DAO 未注入时保持迁移期兼容，调用方仍以 session 写回作为主流程。
func (s *AgentService) persistActiveScheduleTriggerPreviewBestEffort(ctx context.Context, triggerID string, previewID string) error {
	if s == nil || s.activeScheduleDAO == nil {
		return nil
	}
	normalizedTriggerID := strings.TrimSpace(triggerID)
	normalizedPreviewID := strings.TrimSpace(previewID)
	if normalizedTriggerID == "" || normalizedPreviewID == "" {
		return nil
	}
	return s.activeScheduleDAO.UpdateTriggerFields(ctx, normalizedTriggerID, map[string]any{
		"preview_id": &normalizedPreviewID,
		"updated_at": time.Now(),
	})
}

// handleActiveScheduleSessionChat 处理被主动调度 session 占管的聊天入口。
//
// 步骤化说明：
// 1. 先读 session，判断当前 conversation 是否仍在 waiting_user_reply / rerunning 占管期；
// 2. 占管期间先把用户消息写入历史和时间线，保证会话内容不丢失；
// 3. waiting_user_reply 进入 rerunning，并同步调用主动调度 rerun；
// 4. rerunning 则只提示“正在重跑”，避免同一 conversation 被并发重复推进；
// 5. 终态或非占管态直接放行普通 newAgent。
func (s *AgentService) handleActiveScheduleSessionChat(
	ctx context.Context,
	userMessage string,
	traceID string,
	requestStart time.Time,
	userID int,
	chatID string,
	resolvedModelName string,
	outChan chan<- string,
	errChan chan error,
) (bool, error) {
	session, err := s.loadActiveScheduleSessionByConversation(ctx, userID, chatID)
	if err != nil {
		return false, err
	}
	if session == nil || !isActiveScheduleSessionBlockingStatus(session.Status) {
		return false, nil
	}

	trimmedMessage := strings.TrimSpace(userMessage)
	if trimmedMessage != "" {
		// 1. 主动调度占管期间，用户每次回复仍然要进入正常会话历史。
		// 2. 这样后续刷新聊天页时，用户可见消息、时间线和 session 状态不会彼此脱节。
		if err := s.persistNewAgentConversationMessage(ctx, userID, chatID, schema.UserMessage(trimmedMessage), 0); err != nil {
			return true, err
		}
	}

	switch session.Status {
	case model.ActiveScheduleSessionStatusWaitingUserReply:
		if trimmedMessage == "" {
			assistantText := strings.TrimSpace(session.State.PendingQuestion)
			if assistantText == "" {
				assistantText = "请先补充主动调度需要的关键信息。"
			}
			if err := s.persistNewAgentConversationMessage(ctx, userID, chatID, schema.AssistantMessage(assistantText, nil), 0); err != nil {
				return true, err
			}
			emitActiveScheduleAssistantChunk(outChan, traceID, resolvedModelName, requestStart, assistantText, nil)
			return true, nil
		}
		// 1. 收到用户补充信息后，先把 session 切成 rerunning，避免并发请求继续按旧状态走普通聊天。
		// 2. 这个阶段只是状态切换，不代表 graph 已经完成。
		// 3. 这里必须使用 DB CAS 抢占 rerun 权限，避免两条补充消息同时读到 waiting_user_reply 后重复生成 preview。
		switched, err := s.activeScheduleSessionDAO.TryTransitionActiveScheduleSessionStatusBySessionID(
			ctx,
			session.SessionID,
			model.ActiveScheduleSessionStatusWaitingUserReply,
			model.ActiveScheduleSessionStatusRerunning,
		)
		if err != nil {
			return true, err
		}
		if !switched {
			if err := s.respondActiveScheduleRerunning(ctx, userID, chatID, traceID, resolvedModelName, requestStart, outChan); err != nil {
				return true, err
			}
			return true, nil
		}
		session.Status = model.ActiveScheduleSessionStatusRerunning
		if s.cacheDAO != nil {
			if cacheErr := s.cacheDAO.SetActiveScheduleSessionToCache(ctx, session); cacheErr != nil {
				log.Printf("回填主动调度 rerunning session 缓存失败 session=%s err=%v", session.SessionID, cacheErr)
			}
		}
		return true, s.runActiveScheduleSessionRerun(ctx, session, trimmedMessage, traceID, requestStart, resolvedModelName, outChan, errChan)
	case model.ActiveScheduleSessionStatusRerunning:
		// 1. rerunning 是占管中的过渡态，说明当前会话已经在重跑或刚开始重跑。
		// 2. 这里不再触发第二次 rerun，只给用户一个可见的等待提示。
		if trimmedMessage != "" {
			if err := s.respondActiveScheduleRerunning(ctx, userID, chatID, traceID, resolvedModelName, requestStart, outChan); err != nil {
				return true, err
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

// respondActiveScheduleRerunning 负责在重复补充命中并发保护时写入可见提示。
//
// 职责边界：
// 1. 只写聊天历史和 SSE 文本，不推进 session、trigger、preview 状态；
// 2. 用于 rerunning 状态或 CAS 抢占失败后的兜底提示，避免再次触发 graph；
// 3. 写入失败时返回 error，让上层按聊天入口的错误通道处理。
func (s *AgentService) respondActiveScheduleRerunning(
	ctx context.Context,
	userID int,
	chatID string,
	traceID string,
	resolvedModelName string,
	requestStart time.Time,
	outChan chan<- string,
) error {
	assistantText := "主动调度正在重新生成建议，请稍后再试。"
	if err := s.persistNewAgentConversationMessage(ctx, userID, chatID, schema.AssistantMessage(assistantText, nil), 0); err != nil {
		return err
	}
	emitActiveScheduleAssistantChunk(outChan, traceID, resolvedModelName, requestStart, assistantText, nil)
	return nil
}

// runActiveScheduleSessionRerun 负责把 waiting_user_reply 的用户补充同步推进成新的主动调度结果。
//
// 职责边界：
// 1. 只负责聊天入口的最小编排，不复制 worker / notification 链路；
// 2. 成功时把新 preview / ask_user / close 的结果写回 session + timeline；
// 3. 失败时把 session 标成 failed，方便后续排障。
func (s *AgentService) runActiveScheduleSessionRerun(
	ctx context.Context,
	session *model.ActiveScheduleSessionSnapshot,
	userMessage string,
	traceID string,
	requestStart time.Time,
	resolvedModelName string,
	outChan chan<- string,
	errChan chan error,
) error {
	if s == nil || s.activeRerunFunc == nil {
		return errors.New("主动调度 rerun 未接入")
	}
	if session == nil {
		return errors.New("active schedule session 不能为空")
	}

	result, err := s.activeRerunFunc(ctx, session, userMessage, traceID, requestStart)
	if err != nil {
		session.Status = model.ActiveScheduleSessionStatusFailed
		session.State.FailedReason = strings.TrimSpace(err.Error())
		_ = s.persistActiveScheduleSessionBestEffort(ctx, session)
		return err
	}
	if result == nil {
		result = &ActiveScheduleSessionRerunResult{}
	}

	finalStatus := strings.TrimSpace(result.SessionStatus)
	if finalStatus == "" {
		if result.BusinessCard != nil {
			finalStatus = model.ActiveScheduleSessionStatusReadyPreview
		} else {
			finalStatus = model.ActiveScheduleSessionStatusWaitingUserReply
		}
	}
	session.Status = finalStatus
	session.State = result.SessionState
	previewID := strings.TrimSpace(result.PreviewID)
	if previewID != "" {
		session.CurrentPreviewID = previewID
	}
	if session.Status == model.ActiveScheduleSessionStatusReadyPreview {
		session.State.PendingQuestion = ""
		session.State.MissingInfo = nil
		session.State.FailedReason = ""
	}

	if previewID != "" {
		if err := s.persistActiveScheduleTriggerPreviewBestEffort(ctx, session.TriggerID, previewID); err != nil {
			return err
		}
	}

	if err := s.persistActiveScheduleSessionBestEffort(ctx, session); err != nil {
		return err
	}

	assistantText := strings.TrimSpace(result.AssistantText)
	if assistantText == "" && result.BusinessCard != nil {
		assistantText = strings.TrimSpace(result.BusinessCard.Summary)
	}
	if assistantText == "" {
		assistantText = "主动调度建议已更新。"
	}

	// 1. 把新结果写进 conversation history，保证刷新后仍然能看到 rerun 的正文。
	// 2. 再追加业务卡片时间线，前端可以按 timeline 重建主动调度卡片。
	if err := s.persistNewAgentConversationMessage(ctx, session.UserID, session.ConversationID, schema.AssistantMessage(assistantText, nil), 0); err != nil {
		return err
	}
	if result.BusinessCard != nil {
		if _, err := s.appendConversationTimelineEvent(
			ctx,
			session.UserID,
			session.ConversationID,
			model.AgentTimelineKindBusinessCard,
			"assistant",
			assistantText,
			map[string]any{"business_card": result.BusinessCard},
			0,
		); err != nil {
			return err
		}
	}

	emitActiveScheduleAssistantChunk(outChan, traceID, resolvedModelName, requestStart, assistantText, nil)
	if result.BusinessCard != nil {
		emitActiveScheduleBusinessCardChunk(outChan, session.SessionID, traceID, resolvedModelName, requestStart, result.BusinessCard)
	}
	return nil
}

func isActiveScheduleSessionBlockingStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case model.ActiveScheduleSessionStatusWaitingUserReply,
		model.ActiveScheduleSessionStatusRerunning:
		return true
	default:
		return false
	}
}

func emitActiveScheduleAssistantChunk(outChan chan<- string, traceID string, modelName string, requestStart time.Time, text string, extra *newagentstream.OpenAIChunkExtra) {
	payload, err := newagentstream.ToOpenAIAssistantChunkWithExtra(traceID, modelName, requestStart.Unix(), strings.TrimSpace(text), true, extra)
	if err != nil {
		log.Printf("构造主动调度 assistant chunk 失败 trace=%s err=%v", traceID, err)
		return
	}
	pushChunkNonBlocking(outChan, payload)
}

func emitActiveScheduleBusinessCardChunk(outChan chan<- string, blockID string, traceID string, modelName string, requestStart time.Time, card *newagentstream.StreamBusinessCardExtra) {
	if card == nil {
		return
	}
	payload, err := newagentstream.ToOpenAIStreamWithExtra(nil, traceID, modelName, requestStart.Unix(), true, newagentstream.NewBusinessCardExtra(blockID, "active_schedule_session", card))
	if err != nil {
		log.Printf("构造主动调度 business card chunk 失败 trace=%s err=%v", traceID, err)
		return
	}
	pushChunkNonBlocking(outChan, payload)
}

func pushChunkNonBlocking(outChan chan<- string, payload string) {
	if outChan == nil || strings.TrimSpace(payload) == "" {
		return
	}
	select {
	case outChan <- payload:
	default:
		log.Printf("主动调度 SSE 通道已满，丢弃 payload")
	}
}
