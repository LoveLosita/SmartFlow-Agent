package newagentnode

import (
	"context"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentprompt "github.com/LoveLosita/smartflow/backend/newAgent/prompt"
	newagentrouter "github.com/LoveLosita/smartflow/backend/newAgent/router"
	newagentshared "github.com/LoveLosita/smartflow/backend/newAgent/shared"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	"github.com/cloudwego/eino/schema"
)

const (
	quickTaskStageName = "quick_task"
	quickTaskBlockID   = "qt_main"
)

// QuickTaskNodeInput 描述快捷任务节点的输入。
type QuickTaskNodeInput struct {
	RuntimeState          *newagentmodel.AgentRuntimeState
	ConversationContext   *newagentmodel.ConversationContext
	UserInput             string
	Client                *infrallm.Client
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
	UrgencyThresholdAt string `json:"urgency_threshold_at,omitempty"`
	TaskID             *int   `json:"task_id,omitempty"`

	// query 参数
	Quadrant *int   `json:"quadrant,omitempty"`
	Keyword  string `json:"keyword,omitempty"`
	Limit    *int   `json:"limit,omitempty"`

	// ask 参数
	Question string `json:"question,omitempty"`
}

// RunQuickTaskNode 执行快捷任务节点：流式 LLM 提取意图 → 直接调 service → 追加结果。
func RunQuickTaskNode(ctx context.Context, input QuickTaskNodeInput) error {
	flowState := input.RuntimeState.EnsureCommonState()
	emitter := input.ChunkEmitter

	// 1. 构造 messages。
	messages := newagentprompt.BuildQuickTaskMessagesSimple(input.UserInput)

	// 2. 真流式调用 LLM。
	reader, err := input.Client.Stream(ctx, messages, infrallm.GenerateOptions{
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
		decision, parseErr = infrallm.ParseJSONObject[quickTaskDecision](result.DecisionJSON)
		if parseErr != nil {
			log.Printf("[DEBUG] quick_task: JSON 解析失败 chat=%s json=%s", flowState.ConversationID, result.DecisionJSON)
			if result.RawBuffer != "" {
				_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, result.RawBuffer, firstChunk)
				fullText.WriteString(result.RawBuffer)
			}
			break
		}
		log.Printf("[DEBUG] quick_task: 解析结果 chat=%s action=%s title=%s deadline_at=%s priority_group=%v urgency_threshold_at=%q",
			flowState.ConversationID, decision.Action, decision.Title, decision.DeadlineAt, decision.PriorityGroup, decision.UrgencyThresholdAt)

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
			_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, "抱歉，处理任务时出了点问题，请重试。", true)
		}
		msg := schema.AssistantMessage(finalText, nil)
		input.ConversationContext.AppendHistory(msg)
		persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)
		flowState.Phase = newagentmodel.PhaseDone
		return nil
	}

	log.Printf("[DEBUG] quick_task: chat=%s action=%s raw_title=%s", flowState.ConversationID, decision.Action, decision.Title)

	// 5. 根据意图执行操作。
	var resultText string
	switch decision.Action {
	case "create":
		resultText = handleQuickTaskCreate(ctx, input, decision, flowState)
	case "query":
		resultText = handleQuickTaskQuery(ctx, input, decision, flowState)
	case "ask":
		resultText = decision.Question
		if resultText == "" {
			resultText = "你想记录什么呢？告诉我具体内容吧。"
		}
	default:
		resultText = "抱歉，我没有理解你的意思。你可以试试说「记一下明天开会」或「看看我的任务」。"
	}

	// 6. 追加操作结果文本。
	if resultText != "" {
		_ = emitter.EmitAssistantText(quickTaskBlockID, quickTaskStageName, resultText, false)
		fullText.WriteString(resultText)
	}

	// 7. 写入对话历史。
	finalText := fullText.String()
	msg := schema.AssistantMessage(finalText, nil)
	input.ConversationContext.AppendHistory(msg)
	persistVisibleAssistantMessage(ctx, input.PersistVisibleMessage, flowState, msg)

	flowState.Phase = newagentmodel.PhaseDone
	return nil
}

// handleQuickTaskCreate 处理任务创建。
func handleQuickTaskCreate(
	ctx context.Context,
	input QuickTaskNodeInput,
	decision *quickTaskDecision,
	flowState *newagentmodel.CommonState,
) string {
	title := strings.TrimSpace(decision.Title)
	if title == "" {
		return "你想记录什么呢？告诉我具体内容吧。"
	}

	var deadline *time.Time
	if raw := strings.TrimSpace(decision.DeadlineAt); raw != "" {
		parsed, err := newagentshared.ParseOptionalDeadline(raw)
		if err != nil {
			return fmt.Sprintf("截止时间格式不太对（%s），不过我先把任务记下来啦。", err)
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

	log.Printf("[DEBUG] quick_task: CreateTask 参数 chat=%s title=%s priorityGroup=%d deadline=%v urgencyThreshold=%v urgency_raw=%q",
		flowState.ConversationID, title, priorityGroup, deadline, urgencyThreshold, decision.UrgencyThresholdAt)
	_, err := input.QuickTaskDeps.CreateTask(flowState.UserID, title, priorityGroup, deadline, urgencyThreshold)
	if err != nil {
		return fmt.Sprintf("记录失败了（%s），稍后再试试？", err)
	}

	flowState.UsedQuickNote = true

	priorityLabel := newagentshared.PriorityLabelCN(priorityGroup)
	deadlineStr := ""
	if deadline != nil {
		deadlineStr = deadline.In(newagentshared.ShanghaiLocation()).Format("2006-01-02 15:04")
	}

	if deadlineStr != "" {
		return fmt.Sprintf("已记录：%s（%s，截止 %s）", title, priorityLabel, deadlineStr)
	}
	return fmt.Sprintf("已记录：%s（%s）", title, priorityLabel)
}

// handleQuickTaskQuery 处理任务查询。
func handleQuickTaskQuery(
	ctx context.Context,
	input QuickTaskNodeInput,
	decision *quickTaskDecision,
	flowState *newagentmodel.CommonState,
) string {
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

	results, err := input.QuickTaskDeps.QueryTasks(ctx, flowState.UserID, params)
	if err != nil {
		return fmt.Sprintf("查询失败了（%s），稍后再试试？", err)
	}

	if len(results) == 0 {
		return "当前没有匹配的任务。"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("找到 %d 条任务：\n", len(results)))
	for _, t := range results {
		label := newagentshared.PriorityLabelCN(t.PriorityGroup)
		line := fmt.Sprintf("- %s（%s", t.Title, label)
		if t.DeadlineAt != "" {
			line += fmt.Sprintf("，截止 %s", t.DeadlineAt)
		}
		line += "）"
		if t.IsCompleted {
			line += " ✅"
		}
		sb.WriteString(line + "\n")
	}

	return sb.String()
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
