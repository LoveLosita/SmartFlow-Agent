package sv

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"github.com/cloudwego/eino/schema"
)

const (
	// conversationTitleTimeout 是异步标题生成的超时时间。
	// 该过程不在主请求链路里，但仍要设置上限，避免后台协程长时间阻塞。
	conversationTitleTimeout = 4 * time.Second
	// conversationTitleHistoryLimit 限制参与“生成标题”的最近消息条数。
	// 只取最近几轮可减少 token 成本，同时足够概括当前会话主题。
	conversationTitleHistoryLimit = 8
	// conversationTitleMaxChars 是标题最大字符数（按 rune 计）。
	// 控制标题长度，避免前端展示溢出。
	conversationTitleMaxChars = 24
	// conversationListDefaultPage 是会话列表默认页码。
	conversationListDefaultPage = 1
	// conversationListDefaultPageSize 是会话列表默认分页大小。
	conversationListDefaultPageSize = 20
	// conversationListMaxPageSize 是会话列表单页上限，避免超大分页压垮数据库。
	conversationListMaxPageSize = 100
	// conversationTitleTokenAdjustReason 是“标题异步生成 token 账本调整”原因码。
	// 用于日志和后续审计归因。
	conversationTitleTokenAdjustReason = "conversation_title_async"
)

const conversationTitlePrompt = `你是 SmartMate 的会话标题生成器。
请基于给定对话内容，生成一个简短中文标题。

要求：
1) 只输出标题文本，不要解释，不要加引号，不要 markdown。
2) 标题长度控制在 8~20 个中文字符，尽量自然、口语化。
3) 不要出现“用户/助手/对话/聊天记录”等泛化词。
4) 如果内容是任务提醒类，标题应体现核心事项。`

// GetConversationMeta 返回单个会话的元信息（供前端轮询/主动拉取）。
// 说明：
// 1) 该接口和 SSE 流解耦，不依赖流式 header；
// 2) title 允许为空，前端可根据 has_title 决定是否展示占位文案。
func (s *AgentService) GetConversationMeta(ctx context.Context, userID int, chatID string) (*model.GetConversationMetaResponse, error) {
	chat, err := s.repo.GetConversationMeta(ctx, userID, strings.TrimSpace(chatID))
	if err != nil {
		return nil, err
	}

	title := ""
	if chat.Title != nil {
		title = strings.TrimSpace(*chat.Title)
	}

	return &model.GetConversationMetaResponse{
		ConversationID: chat.ChatID,
		Title:          title,
		HasTitle:       title != "",
		MessageCount:   chat.MessageCount,
		LastMessageAt:  chat.LastMessageAt,
		Status:         chat.Status,
	}, nil
}

// GetConversationList 返回“当前用户会话列表（分页）”。
//
// 职责边界：
// 1. 负责分页参数规范化（默认值、上限保护）；
// 2. 负责状态过滤值校验（仅允许 active/archived）；
// 3. 负责把 DAO 模型转换成前端响应 DTO；
// 4. 不负责缓存（由上层架构决策按需引入）。
func (s *AgentService) GetConversationList(ctx context.Context, userID, page, pageSize int, status string) (*model.GetConversationListResponse, error) {
	// 1. 先做参数规范化，保证 DAO 层始终收到安全参数。
	normalizedPage := normalizeConversationListPage(page)
	normalizedPageSize := normalizeConversationListPageSize(pageSize)

	// 2. 校验状态过滤器：
	// 2.1 允许空值（表示不过滤）；
	// 2.2 仅接受 active/archived，避免把任意字符串下推到 SQL。
	normalizedStatus, valid := normalizeConversationStatus(status)
	if !valid {
		return nil, respond.WrongParamType
	}

	// 3. 查库拿分页结果。
	chats, total, err := s.repo.GetConversationList(ctx, userID, normalizedPage, normalizedPageSize, normalizedStatus)
	if err != nil {
		return nil, err
	}

	// 4. 转换为响应 DTO，统一 title/has_title 语义，避免前端重复处理空指针。
	items := make([]model.GetConversationListItem, 0, len(chats))
	for _, chatItem := range chats {
		title := ""
		if chatItem.Title != nil {
			title = strings.TrimSpace(*chatItem.Title)
		}
		items = append(items, model.GetConversationListItem{
			ConversationID: chatItem.ChatID,
			Title:          title,
			HasTitle:       title != "",
			MessageCount:   chatItem.MessageCount,
			LastMessageAt:  chatItem.LastMessageAt,
			Status:         chatItem.Status,
			CreatedAt:      chatItem.CreatedAt,
		})
	}

	// 5. 计算 has_more 语义，前端可直接用于“继续加载”按钮。
	hasMore := int64(normalizedPage*normalizedPageSize) < total
	return &model.GetConversationListResponse{
		List:     items,
		Page:     normalizedPage,
		PageSize: normalizedPageSize,
		Limit:    normalizedPageSize,
		Total:    total,
		HasMore:  hasMore,
	}, nil
}

func normalizeConversationListPage(page int) int {
	if page <= 0 {
		return conversationListDefaultPage
	}
	return page
}

func normalizeConversationListPageSize(pageSize int) int {
	if pageSize <= 0 {
		return conversationListDefaultPageSize
	}
	if pageSize > conversationListMaxPageSize {
		return conversationListMaxPageSize
	}
	return pageSize
}

func normalizeConversationStatus(status string) (string, bool) {
	normalized := strings.TrimSpace(strings.ToLower(status))
	if normalized == "" {
		return "", true
	}
	if normalized == "active" || normalized == "archived" {
		return normalized, true
	}
	return "", false
}

// ensureConversationTitleAsync 在后台异步生成并写入会话标题。
// 设计约束：
// 1) 仅在“标题为空”时尝试生成，避免覆盖用户已确认/已存在标题；
// 2) 失败只记日志，不影响当前聊天链路；
// 3) 标题素材优先来自 Redis 历史（命中快、与当前上下文一致）。
func (s *AgentService) ensureConversationTitleAsync(userID int, chatID string) {
	if s == nil || s.repo == nil || s.agentCache == nil {
		return
	}
	if strings.TrimSpace(chatID) == "" {
		return
	}

	go func() {
		// 1. 后台任务使用独立超时上下文，避免受请求 ctx 取消影响。
		ctx, cancel := context.WithTimeout(context.Background(), conversationTitleTimeout)
		defer cancel()

		// 2. 先查当前标题；若已存在则直接返回，不做多余模型调用。
		title, exists, err := s.repo.GetConversationTitle(ctx, userID, chatID)
		if err != nil {
			log.Printf("异步生成会话标题失败(读取标题失败) chat=%s err=%v", chatID, err)
			return
		}
		if !exists || strings.TrimSpace(title) != "" {
			return
		}

		// 3. 从 Redis 读取当前会话历史，作为标题生成素材。
		history, err := s.agentCache.GetHistory(ctx, chatID)
		if err != nil {
			log.Printf("异步生成会话标题失败(读取历史失败) chat=%s err=%v", chatID, err)
			return
		}
		if len(history) == 0 {
			return
		}

		// 4. 调用模型生成标题，并做格式清洗。
		generated, titleTokens, err := s.generateConversationTitle(ctx, history)
		if err != nil {
			log.Printf("异步生成会话标题失败(模型生成失败) chat=%s err=%v", chatID, err)
			return
		}
		if strings.TrimSpace(generated) == "" {
			return
		}

		// 4.1 标题生成的模型消耗不再走旧 token 账本。
		// 4.1.1 当前 Credit 计费统一由独立 LLM 服务出口处理；
		// 4.1.2 这里只保留 titleTokens 变量，避免同轮继续改动模型返回签名。
		_ = titleTokens

		// 5. 只在标题仍为空时写入，保证并发幂等。
		if err = s.repo.UpdateConversationTitleIfEmpty(ctx, userID, chatID, generated); err != nil {
			log.Printf("异步生成会话标题失败(写库失败) chat=%s err=%v", chatID, err)
		}
	}()
}

// generateConversationTitle 使用聊天模型从近期历史生成标题。
func (s *AgentService) generateConversationTitle(ctx context.Context, history []*schema.Message) (string, int, error) {
	modelInst := s.pickTitleModel()
	if modelInst == nil {
		return "", 0, fmt.Errorf("标题生成模型未初始化")
	}

	// 1. 只取最近 N 条，降低 token 并聚焦当前会话主题。
	trimmed := tailMessages(history, conversationTitleHistoryLimit)
	prompt := buildConversationTitleUserPrompt(trimmed)
	if strings.TrimSpace(prompt) == "" {
		return "", 0, fmt.Errorf("缺少可用历史内容")
	}

	messages := []*schema.Message{
		schema.SystemMessage(conversationTitlePrompt),
		schema.UserMessage(prompt),
	}

	// 2. 标题生成属于结构化短输出，关闭 thinking 并限制 tokens，降低延迟与发散。
	resp, err := modelInst.GenerateText(ctx, messages, llmservice.GenerateOptions{
		Temperature: 0.2,
		MaxTokens:   40,
		Thinking:    llmservice.ThinkingModeDisabled,
	})
	if err != nil {
		return "", 0, err
	}
	if resp == nil {
		return "", 0, fmt.Errorf("标题生成模型返回为空")
	}

	// 2.1 标题链路的 token 从模型响应 usage 中提取；缺失则按 0 处理，不影响主流程。
	titleTokens := 0
	if resp.Usage != nil {
		titleTokens = normalizeUsageTotal(
			resp.Usage.TotalTokens,
			resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens,
		)
	}
	return normalizeConversationTitle(resp.Text), titleTokens, nil
}

// pickTitleModel 选择用于标题生成的模型。
// 优先 Lite（成本低、速度快）；Lite 不可用时回退 Pro。
func (s *AgentService) pickTitleModel() *llmservice.Client {
	if s == nil || s.llmService == nil {
		return nil
	}
	if client := s.llmService.LiteClient(); client != nil {
		return client
	}
	return s.llmService.ProClient()
}

// buildConversationTitleUserPrompt 把消息历史拼成可读文本供模型总结。
func buildConversationTitleUserPrompt(messages []*schema.Message) string {
	var builder strings.Builder
	builder.WriteString("请根据以下对话内容生成标题：\n")
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		// 单条消息做长度裁剪，避免超长回复把标题主题“冲淡”。
		content = trimRunes(content, 80)
		role := "助手"
		if strings.EqualFold(strings.TrimSpace(string(msg.Role)), string(schema.User)) {
			role = "用户"
		}
		builder.WriteString(role)
		builder.WriteString("：")
		builder.WriteString(content)
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func tailMessages(messages []*schema.Message, limit int) []*schema.Message {
	if limit <= 0 || len(messages) <= limit {
		return messages
	}
	return messages[len(messages)-limit:]
}

// normalizeConversationTitle 清洗模型输出，确保可直接展示/存库。
func normalizeConversationTitle(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	if idx := strings.Index(text, "\n"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	text = strings.Trim(text, "\"'“”‘’《》[]【】")
	text = strings.TrimPrefix(text, "标题：")
	text = strings.TrimPrefix(text, "标题:")
	text = strings.TrimSpace(text)
	text = trimRunes(text, conversationTitleMaxChars)
	return strings.TrimSpace(text)
}

func trimRunes(text string, limit int) string {
	if limit <= 0 || text == "" {
		return ""
	}
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	runes := []rune(text)
	return string(runes[:limit])
}

// GetContextStats 获取指定会话的上下文窗口 token 分布统计。
func (s *AgentService) GetContextStats(ctx context.Context, userID int, chatID string) (string, error) {
	return s.repo.LoadContextTokenStats(ctx, userID, chatID)
}
