package newagentnode

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	taskmodel "github.com/LoveLosita/smartflow/backend/model"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentrouter "github.com/LoveLosita/smartflow/backend/newAgent/router"
	newagentshared "github.com/LoveLosita/smartflow/backend/newAgent/shared"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/cloudwego/eino/schema"
)

const (
	quickTaskStageName        = "quick_task"
	quickTaskBlockID          = "qt_main"
	quickTaskResultCardID     = "quick_task.result"
	taskRecordSourceQuickNote = "quick_note"
)

// QuickTaskNodeInput 描述快捷任务节点的输入。
type QuickTaskNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	Client                *llmservice.Client
	ChunkEmitter          *newagentstream.ChunkEmitter
	QuickTaskDeps         newagentmodel.QuickTaskDeps
	PersistVisibleMessage newagentmodel.PersistVisibleMessageFunc
}

// quickTaskDecision 是从 LLM 输出中解析的结构化意图。
type quickTaskDecision struct {
	Action             string `json:"action"`
	Title              string `json:"title,omitempty"`
	DeadlineAt         string `json:"deadline_at,omitempty"`
	PriorityGroup      *int   `json:"priority_group,omitempty"`
	EstimatedSections  *int   `json:"estimated_sections,omitempty"`
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty"`
	TaskID             *int   `json:"task_id,omitempty"`

	// query 参数
	Quadrant       *int   `json:"quadrant,omitempty"`
	Keyword        string `json:"keyword,omitempty"`
	Limit          *int   `json:"limit,omitempty"`
	DeadlineAfter  string `json:"deadline_after,omitempty"`
	DeadlineBefore string `json:"deadline_before,omitempty"`

	// ask 参数
	Question string `json:"question,omitempty"`
}

// quickTaskActionResult 是 quick_task 执行动作后的统一回包。
//
// 职责边界：
// 1. AssistantText 是本轮要补发给用户的短正文；
// 2. BusinessCard 仅在“业务真实成功”时携带，失败/追问场景必须为空；
// 3. 不负责直接发射，发射时机由 RunQuickTaskNode 统一控制。
type quickTaskActionResult struct {
	AssistantText string
	BusinessCard  *newagentstream.StreamBusinessCardExtra
}

// RunQuickTaskNode 执行快捷任务节点：流式 LLM 提取意图 → 直接调 service → 追加结果。
func RunQuickTaskNode(ctx context.Context, input QuickTaskNodeInput) error {
	flowState := input.RuntimeState.EnsureCommonState()
	emitter := input.ChunkEmitter

	// 1. 构造 messages。
	messages := newagentprompt.BuildQuickTaskMessagesSimple(input.UserInput)

	// 2. 真流式调用 LLM。
	reader, err := input.Client.Stream(ctx, messages, llmservice.GenerateOptions{
		Temperature: 0.3,
		MaxTokens:   512,
	})
	if err != nil {
		log.Printf("[WARN] quick_task: Stream 调用失败 chat=%s err=%v", flowState.ConversationID, err)
		_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, "抱歉，处理任务时出了点问题，请重试。", true)
		flowState.Phase = newagentmodel.PhaseDone
		return nil
	}

	// 3. 两阶段流式解析。
	parser := newagentrouter.NewStreamDecisionParser()
	firstChunk := true
	var decision *quickTaskDecision
	var fullText strings.Builder

	// 阶段一：解析决策标签。
	for {
		chunk, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			log.Printf("[WARN] quick_task stream recv error chat=%s err=%v", flowState.ConversationID, recvErr)
			break
		}

		content := ""
		if chunk != nil {
			content = chunk.Content
		}

		visible, ready, _ := parser.Feed(content)
		if !ready {
			continue
		}

		result := parser.Result()

		// Fallback / 解析失败：把原始文本当作纯回复推送。
		if result.Fallback || result.ParseFailed {
			log.Printf("[DEBUG] quick_task: 标签解析失败 chat=%s raw=%s", flowState.ConversationID, result.RawBuffer)
			if result.RawBuffer != "" {
				_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, result.RawBuffer, firstChunk)
				fullText.WriteString(result.RawBuffer)
			}
			break
		}

		// 解析 JSON。
		log.Printf("[DEBUG] quick_task: LLM 原始决策 JSON chat=%s json=%s", flowState.ConversationID, result.DecisionJSON)
		var parseErr error
		decision, parseErr = llmservice.ParseJSONObject[quickTaskDecision](result.DecisionJSON)
		if parseErr != nil {
			log.Printf("[DEBUG] quick_task: JSON 解析失败 chat=%s json=%s", flowState.ConversationID, result.DecisionJSON)
			if result.RawBuffer != "" {
				_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, result.RawBuffer, firstChunk)
				fullText.WriteString(result.RawBuffer)
			}
			break
		}
		log.Printf("[DEBUG] quick_task: 解析结果 chat=%s action=%s title=%s deadline_at=%s priority_group=%v estimated_sections=%v urgency_threshold_at=%q",
			flowState.ConversationID, decision.Action, decision.Title, decision.DeadlineAt, decision.PriorityGroup, decision.EstimatedSections, decision.UrgencyThresholdAt)

		// 阶段二：流式推送标签后正文。
		if visible != "" {
			if emitErr := emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, visible, firstChunk); emitErr != nil {
				log.Printf("[WARN] quick_task emit error chat=%s err=%v", flowState.ConversationID, emitErr)
			}
			fullText.WriteString(visible)
			firstChunk = false
		}
		for {
			chunk2, recvErr2 := reader.Recv()
			if recvErr2 == io.EOF {
				break
			}
			if recvErr2 != nil {
				log.Printf("[WARN] quick_task stream error chat=%s err=%v", flowState.ConversationID, recvErr2)
				break
			}
			if chunk2 == nil || chunk2.Content == "" {
				continue
			}
			if emitErr := emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, chunk2.Content, firstChunk); emitErr != nil {
				log.Printf("[WARN] quick_task emit error chat=%s err=%v", flowState.ConversationID, emitErr)
			}
			fullText.WriteString(chunk2.Content)
			firstChunk = false
		}
		break
	}

	// 4. 流结束但未解析到决策 → 降级为纯文本回复。
	if decision == nil {
		finalText := fullText.String()
		if strings.TrimSpace(finalText) == "" {
			finalText = "抱歉，处理任务时出了点问题，请重试。"
			_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, finalText, true)
		}
		msg := schema.AssistantMessage(finalText, nil)
		input.ConversationContext.AppendHistory(msg)
		persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		flowState.Phase = newagentmodel.PhaseDone
		return nil
	}

	log.Printf("[DEBUG] quick_task: chat=%s action=%s raw_title=%s", flowState.ConversationID, decision.Action, decision.Title)

	// 5. 根据意图执行操作。
	result := quickTaskActionResult{}
	switch decision.Action {
	case "create":
		result = handleQuickTaskCreate(ctx, input, decision, flowState)
	case "query":
		result = handleQuickTaskQuery(ctx, input, decision, flowState)
	case "ask":
		result.AssistantText = decision.Question
		if result.AssistantText == "" {
			result.AssistantText = "你想记录什么呢？告诉我具体内容吧。"
		}
	default:
		result.AssistantText = "抱歉，我没有理解你的意思。你可以试试说「记一下明天开会」或「看看我的任务」。"
	}

	// 6. 追加操作结果正文。
	if result.AssistantText != "" {
		_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, result.AssistantText, false)
		fullText.WriteString(result.AssistantText)
	}

	messagePersisted := false
	// 7.1 有业务卡片时，先落正文，再发卡片，保证 timeline 顺序与前端展示一致。
	// 1. 先持久化正文，确保 timeline 里的 assistant_text seq 一定早于 business_card；
	// 2. 再发 business_card，保证“短正文 + 紧跟卡片”的时序契约；
	// 3. 卡片发射失败只记日志，不回滚正文，避免用户侧出现“看不到结果文本”的回退。
	if result.BusinessCard != nil {
		finalText := fullText.String()
		if strings.TrimSpace(finalText) != "" {
			msg := schema.AssistantMessage(finalText, nil)
			input.ConversationContext.AppendHistory(msg)
			persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
			messagePersisted = true
		}
		if emitErr := emitter.EmitBusinessCard(quickTaskResultCardID, quickTaskStageName, result.BusinessCard); emitErr != nil {
			log.Printf("[WARN] quick_task emit business_card error chat=%s err=%v", flowState.ConversationID, emitErr)
		}
	}

	// 7.2 非卡片路径沿用原有收口：本轮正文统一一次性写入 history。
	if !messagePersisted {
		finalText := fullText.String()
		msg := schema.AssistantMessage(finalText, nil)
		input.ConversationContext.AppendHistory(msg)
		persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
	}

	flowState.Phase = newagentmodel.PhaseDone
	return nil
}

// handleQuickTaskCreate 处理任务创建。
func handleQuickTaskCreate(
	ctx context.Context,
	input QuickTaskNodeInput,
	decision *quickTaskDecision,
	flowState *newagentmodel.CommonState,
) quickTaskActionResult {
	_ = ctx
	title := strings.TrimSpace(decision.Title)
	if title == "" {
		return quickTaskActionResult{AssistantText: "你想记录什么呢？告诉我具体内容吧。"}
	}

	var deadline *time.Time
	if raw := strings.TrimSpace(decision.DeadlineAt); raw != "" {
		parsed, err := newagentshared.ParseOptionalDeadline(raw)
		if err != nil {
			return quickTaskActionResult{AssistantText: fmt.Sprintf("截止时间格式不太对（%s），不过我先把任务记下来啦。", err)}
		}
		deadline = parsed
	}

	priorityGroup := 0
	if decision.PriorityGroup != nil && newagentshared.IsValidTaskPriority(*decision.PriorityGroup) {
		priorityGroup = *decision.PriorityGroup
	}
	if priorityGroup == 0 {
		priorityGroup = quickNoteFallbackPriority(deadline)
	}
	estimatedSections := taskmodel.NormalizeEstimatedSections(decision.EstimatedSections)

	var urgencyThreshold *time.Time
	if raw := strings.TrimSpace(decision.UrgencyThresholdAt); raw != "" {
		parsed, err := newagentshared.ParseOptionalDeadline(raw)
		if err == nil {
			urgencyThreshold = parsed
		}
	}
	// LLM 经常省略 urgency_threshold_at，代码兜底：priorityGroup=2 且有 deadline 时自动推算。
	if urgencyThreshold == nil && priorityGroup == 2 && deadline != nil {
		fallback := deadline.Add(-24 * time.Hour)
		urgencyThreshold = &fallback
	}

	log.Printf("[DEBUG] quick_task: CreateTask 参数 chat=%s title=%s priorityGroup=%d estimatedSections=%d deadline=%v urgencyThreshold=%v urgency_raw=%q estimated_raw=%v",
		flowState.ConversationID, title, priorityGroup, estimatedSections, deadline, urgencyThreshold, decision.UrgencyThresholdAt, decision.EstimatedSections)
	taskID, err := input.QuickTaskDeps.CreateTask(flowState.UserID, title, priorityGroup, estimatedSections, deadline, urgencyThreshold)
	if err != nil {
		return quickTaskActionResult{AssistantText: fmt.Sprintf("记录失败了（%s），稍后再试试？", err)}
	}

	flowState.UsedQuickNote = true
	return quickTaskActionResult{
		AssistantText: "已帮你记下这条任务。",
		BusinessCard:  buildTaskRecordBusinessCard(taskID, title, priorityGroup, estimatedSections, deadline, urgencyThreshold),
	}
}

// handleQuickTaskQuery 处理任务查询。
func handleQuickTaskQuery(
	ctx context.Context,
	input QuickTaskNodeInput,
	decision *quickTaskDecision,
	flowState *newagentmodel.CommonState,
) quickTaskActionResult {
	params := newagentmodel.TaskQueryParams{
		SortBy:           "deadline",
		Order:            "asc",
		Limit:            5,
		IncludeCompleted: false,
	}

	if decision.Quadrant != nil && *decision.Quadrant >= 1 && *decision.Quadrant <= 4 {
		params.Quadrant = decision.Quadrant
	}
	if kw := strings.TrimSpace(decision.Keyword); kw != "" {
		params.Keyword = kw
	}
	if decision.Limit != nil && *decision.Limit > 0 && *decision.Limit <= 20 {
		params.Limit = *decision.Limit
	}
	params.DeadlineAfter = parseQuickTaskQueryDeadlineBoundary(decision.DeadlineAfter, "deadline_after", flowState)
	params.DeadlineBefore = parseQuickTaskQueryDeadlineBoundary(decision.DeadlineBefore, "deadline_before", flowState)
	// 1. 若模型给出了颠倒的时间窗（before<=after），当前轮降级为“不加时间窗”继续查询；
	// 2. 这样能避免误筛选成空结果，同时把异常留给日志排查；
	// 3. 这里只做兜底，不尝试替模型自动纠正语义，避免引入额外猜测。
	if params.DeadlineAfter != nil && params.DeadlineBefore != nil && !params.DeadlineBefore.After(*params.DeadlineAfter) {
		log.Printf("[WARN] quick_task: query 时间窗无效 chat=%s after=%s before=%s，已降级为无时间窗筛选",
			flowState.ConversationID,
			formatQuickTaskTime(params.DeadlineAfter),
			formatQuickTaskTime(params.DeadlineBefore),
		)
		params.DeadlineAfter = nil
		params.DeadlineBefore = nil
	}

	results, err := input.QuickTaskDeps.QueryTasks(ctx, flowState.UserID, params)
	if err != nil {
		return quickTaskActionResult{AssistantText: fmt.Sprintf("查询失败了（%s），稍后再试试？", err)}
	}

	card := buildTaskQueryBusinessCard(params, results)
	if len(results) == 0 {
		return quickTaskActionResult{
			AssistantText: "我这边没查到匹配任务。",
			BusinessCard:  card,
		}
	}

	return quickTaskActionResult{
		AssistantText: fmt.Sprintf("我找到 %d 条任务，整理成卡片给你。", len(results)),
		BusinessCard:  card,
	}
}

func buildTaskRecordBusinessCard(taskID int, title string, priorityGroup int, estimatedSections int, deadline *time.Time, urgencyThreshold *time.Time) *newagentstream.StreamBusinessCardExtra {
	data := map[string]any{
		"id":                 taskID,
		"title":              strings.TrimSpace(title),
		"priority_group":     priorityGroup,
		"estimated_sections": estimatedSections,
		"priority_label":     newagentshared.PriorityLabelCN(priorityGroup),
		"status":             "todo",
	}
	if formatted := formatQuickTaskTime(deadline); formatted != "" {
		data["deadline_at"] = formatted
	}
	if formatted := formatQuickTaskTime(urgencyThreshold); formatted != "" {
		data["urgency_threshold_at"] = formatted
	}

	// 说明：
	// 1. quick_task 当前只有 action=create，未显式区分“随口记 / 正式创建任务”；
	// 2. 仅凭当前 prompt 决策无法稳定判断 source=create_task，会引入误判；
	// 3. 本轮按最小安全口径固定为 quick_note，等后续补稳定判别字段再切分。
	return &newagentstream.StreamBusinessCardExtra{
		CardType: "task_record",
		Title:    "已帮你记下",
		Summary:  "一条轻量提醒已写入任务系统",
		Source:   taskRecordSourceQuickNote,
		Data:     data,
	}
}

func buildTaskQueryBusinessCard(params newagentmodel.TaskQueryParams, results []newagentmodel.TaskQueryResult) *newagentstream.StreamBusinessCardExtra {
	taskItems := make([]map[string]any, 0, len(results))
	for _, task := range results {
		item := map[string]any{
			"id":                 task.ID,
			"title":              strings.TrimSpace(task.Title),
			"priority_group":     task.PriorityGroup,
			"estimated_sections": task.EstimatedSections,
			"priority_label":     newagentshared.PriorityLabelCN(task.PriorityGroup),
			"is_completed":       task.IsCompleted,
		}
		if deadline := strings.TrimSpace(task.DeadlineAt); deadline != "" {
			item["deadline_at"] = deadline
		}
		taskItems = append(taskItems, item)
	}

	title := fmt.Sprintf("找到 %d 条任务", len(results))
	if len(results) == 0 {
		title = "未找到匹配任务"
	}

	data := map[string]any{
		"result_count": len(results),
		"shown_count":  len(results),
		"tasks":        taskItems,
	}
	queryFilters := buildTaskQueryFilters(params)
	if len(queryFilters) > 0 {
		data["query_filters"] = queryFilters
	}
	querySummary := buildTaskQuerySummary(queryFilters)
	if querySummary != "" {
		data["query_summary"] = querySummary
	}

	return &newagentstream.StreamBusinessCardExtra{
		CardType: "task_query",
		Title:    title,
		Summary:  querySummary,
		Data:     data,
	}
}

// buildTaskQueryFilter 生成查询条件的稳定结构化描述。
//
// 职责边界：
// 1. key/operator/value 提供前端可依赖的机器语义；
// 2. label/display_text 提供前端可直接展示的中文文案；
// 3. query_summary 只能从 display_text 派生，前端不要再反向解析 summary。
func buildTaskQueryFilter(key string, label string, value any, operator string, displayText string) map[string]any {
	filter := map[string]any{
		"key":          key,
		"label":        label,
		"value":        value,
		"display_text": strings.TrimSpace(displayText),
	}
	if strings.TrimSpace(operator) != "" {
		filter["operator"] = strings.TrimSpace(operator)
	}
	return filter
}

func buildTaskQueryFilters(params newagentmodel.TaskQueryParams) []map[string]any {
	filters := make([]map[string]any, 0, 6)
	if params.Quadrant != nil && *params.Quadrant >= 1 && *params.Quadrant <= 4 {
		label := newagentshared.PriorityLabelCN(*params.Quadrant)
		filters = append(filters, buildTaskQueryFilter(
			"quadrant",
			"象限",
			*params.Quadrant,
			"eq",
			fmt.Sprintf("象限：%s", label),
		))
	}
	if kw := strings.TrimSpace(params.Keyword); kw != "" {
		filters = append(filters, buildTaskQueryFilter(
			"keyword",
			"关键词",
			kw,
			"contains",
			fmt.Sprintf("关键词：%s", kw),
		))
	}
	if params.DeadlineAfter != nil {
		formatted := formatQuickTaskTime(params.DeadlineAfter)
		filters = append(filters, buildTaskQueryFilter(
			"deadline_after",
			"截止起始",
			formatted,
			"gte",
			fmt.Sprintf("截止时间≥%s", formatted),
		))
	}
	if params.DeadlineBefore != nil {
		formatted := formatQuickTaskTime(params.DeadlineBefore)
		filters = append(filters, buildTaskQueryFilter(
			"deadline_before",
			"截止结束",
			formatted,
			"lt",
			fmt.Sprintf("截止时间<%s", formatted),
		))
	}
	if !params.IncludeCompleted {
		filters = append(filters, buildTaskQueryFilter(
			"include_completed",
			"完成状态",
			false,
			"eq",
			"仅未完成",
		))
	}

	sortValue := "deadline_asc"
	sortDisplay := "按截止时间升序"
	switch strings.ToLower(strings.TrimSpace(params.SortBy)) {
	case "priority":
		if strings.ToLower(strings.TrimSpace(params.Order)) == "desc" {
			sortValue = "priority_desc"
			sortDisplay = "按优先级降序"
		} else {
			sortValue = "priority_asc"
			sortDisplay = "按优先级升序"
		}
	case "id":
		if strings.ToLower(strings.TrimSpace(params.Order)) == "desc" {
			sortValue = "id_desc"
			sortDisplay = "按创建顺序倒序"
		} else {
			sortValue = "id_asc"
			sortDisplay = "按创建顺序正序"
		}
	default:
		if strings.ToLower(strings.TrimSpace(params.Order)) == "desc" {
			sortValue = "deadline_desc"
			sortDisplay = "按截止时间降序"
		}
	}
	filters = append(filters, buildTaskQueryFilter(
		"sort",
		"排序",
		sortValue,
		"eq",
		sortDisplay,
	))
	return filters
}

func buildTaskQuerySummary(filters []map[string]any) string {
	parts := make([]string, 0, len(filters))
	for _, filter := range filters {
		if text, ok := filter["display_text"].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, strings.TrimSpace(text))
		}
	}
	return strings.Join(parts, "；")
}

// parseQuickTaskQueryDeadlineBoundary 解析 quick_task 查询时间窗边界。
//
// 职责边界：
// 1. 只负责把 query 的 deadline_after/deadline_before 文本解析成时间；
// 2. 解析失败时仅记录日志并返回 nil，不中断查询主链路；
// 3. 不负责时间窗合法性校验（如 before<=after），该校验由调用方统一处理。
func parseQuickTaskQueryDeadlineBoundary(raw string, field string, flowState *newagentmodel.CommonState) *time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	parsed, err := newagentshared.ParseOptionalDeadline(value)
	if err != nil {
		chatID := ""
		if flowState != nil {
			chatID = flowState.ConversationID
		}
		log.Printf("[WARN] quick_task: query %s 解析失败 chat=%s raw=%q err=%v，已降级为无该筛选条件", field, chatID, value, err)
		return nil
	}
	return parsed
}

func formatQuickTaskTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(newagentshared.ShanghaiLocation()).Format("2006-01-02 15:04")
}

// quickNoteFallbackPriority 根据截止时间推断默认优先级。
func quickNoteFallbackPriority(deadline *time.Time) int {
	if deadline != nil {
		if time.Until(*deadline) <= 48*time.Hour {
			return newagentshared.QuickNotePriorityImportantUrgent
		}
		return newagentshared.QuickNotePriorityImportantNotUrgent
	}
	return newagentshared.QuickNotePrioritySimpleNotImportant
}
