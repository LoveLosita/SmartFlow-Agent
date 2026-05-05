package agentexecute

import (
	"fmt"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/services/agent/model"
	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
	"github.com/cloudwego/eino/schema"
)

const (
	planCurrentStepKey   = "current_step"
	planCurrentStepTitle = "当前步骤"
)

func prepareExecuteNodeInput(input ExecuteNodeInput) (*agentmodel.AgentRuntimeState, *agentmodel.ConversationContext, *agentstream.ChunkEmitter, error) {
	if input.RuntimeState == nil {
		return nil, nil, nil, fmt.Errorf("execute node: runtime state 不能为空")
	}
	if input.Client == nil {
		return nil, nil, nil, fmt.Errorf("execute node: execute client 未注入")
	}

	input.RuntimeState.EnsureCommonState()
	if input.ConversationContext == nil {
		input.ConversationContext = agentmodel.NewConversationContext("")
	}
	if input.ChunkEmitter == nil {
		input.ChunkEmitter = agentstream.NewChunkEmitter(agentstream.NoopPayloadEmitter(), "", "", time.Now().Unix())
	}
	return input.RuntimeState, input.ConversationContext, input.ChunkEmitter, nil
}

func syncExecutePinnedContext(
	conversationContext *agentmodel.ConversationContext,
	flowState *agentmodel.CommonState,
) {
	if conversationContext == nil || flowState == nil {
		return
	}

	execContent := buildExecuteContextPinnedMarkdown(flowState)
	if strings.TrimSpace(execContent) != "" {
		conversationContext.UpsertPinnedBlock(agentmodel.ContextBlock{
			Key:     executePinnedKey,
			Title:   "执行上下文",
			Content: execContent,
		})
	}

	if !flowState.HasPlan() {
		conversationContext.RemovePinnedBlock(planCurrentStepKey)
		return
	}

	step, ok := flowState.CurrentPlanStep()
	if !ok {
		conversationContext.RemovePinnedBlock(planCurrentStepKey)
		return
	}

	current, total := flowState.PlanProgress()
	title := strings.TrimSpace(planCurrentStepTitle)
	if title == "" {
		title = "当前步骤"
	}
	conversationContext.UpsertPinnedBlock(agentmodel.ContextBlock{
		Key:     planCurrentStepKey,
		Title:   title,
		Content: buildCurrentPlanStepPinnedMarkdown(step, current, total),
	})
}

func appendExecuteStepAdvancedMarker(conversationContext *agentmodel.ConversationContext) {
	if conversationContext == nil {
		return
	}

	history := conversationContext.HistorySnapshot()
	if len(history) > 0 {
		last := history[len(history)-1]
		if last != nil && last.Extra != nil {
			if kind, ok := last.Extra[executeHistoryKindKey].(string); ok && strings.TrimSpace(kind) == executeHistoryKindStepAdvanced {
				return
			}
		}
	}

	conversationContext.AppendHistory(&schema.Message{
		Role:    schema.Assistant,
		Content: "",
		Extra: map[string]any{
			executeHistoryKindKey: executeHistoryKindStepAdvanced,
		},
	})
}

func buildExecuteContextPinnedMarkdown(flowState *agentmodel.CommonState) string {
	if flowState == nil {
		return ""
	}

	lines := make([]string, 0, 8)
	if flowState.HasPlan() {
		lines = append(lines, "执行模式：计划执行（按步骤推进）")
		current, total := flowState.PlanProgress()
		lines = append(lines, fmt.Sprintf("计划进度：第 %d/%d 步", current, total))

		if step, ok := flowState.CurrentPlanStep(); ok {
			lines = append(lines, "当前步骤："+compactExecutePinnedText(step.Content))
			doneWhen := compactExecutePinnedText(step.DoneWhen)
			if doneWhen != "" {
				lines = append(lines, "完成判定(done_when)："+doneWhen)
			}
			lines = append(lines, "动作纪律：未满足 done_when 禁止 next_plan；满足后优先 next_plan。")
		} else {
			lines = append(lines, "当前步骤：不可读（可能已执行完成）")
		}
	} else {
		lines = append(lines, "执行模式：自由执行（无预定义步骤）")
	}

	if flowState.MaxRounds > 0 {
		lines = append(lines, fmt.Sprintf("轮次预算：%d/%d", flowState.RoundUsed, flowState.MaxRounds))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func buildCurrentPlanStepPinnedMarkdown(step agentmodel.PlanStep, current, total int) string {
	lines := make([]string, 0, 4)
	lines = append(lines, fmt.Sprintf("步骤进度：第 %d/%d 步", current, total))

	content := compactExecutePinnedText(step.Content)
	if content == "" {
		content = "（空）"
	}
	lines = append(lines, "步骤内容："+content)

	doneWhen := compactExecutePinnedText(step.DoneWhen)
	if doneWhen != "" {
		lines = append(lines, "完成判定："+doneWhen)
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func compactExecutePinnedText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", "；")
	return strings.TrimSpace(text)
}
