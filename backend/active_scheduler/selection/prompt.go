package selection

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/candidate"
)

const selectionSystemPrompt = `
你是 SmartFlow 主动调度的候选选择器。

你的职责很窄：
1. 只在后端已经生成并校验过的候选里做有限选择；
2. 只参考结构化事实、候选基础信息、capacity_fit 和 risk_level；
3. 不要输出推理过程，不要输出 score，不要输出 confidence，不要编造新的候选；
4. 只输出 JSON，不要输出 markdown，不要输出解释性正文。

允许的 action：
- select_candidate
- ask_user
- notify_only
- close

输出 JSON 结构：
{
  "action": "select_candidate",
  "selected_candidate_id": "cand_xxx",
  "reason": "简短选择理由",
  "explanation_text": "给用户看的简短解释",
  "notification_summary": "通知里要显示的简短摘要",
  "ask_user_question": "需要追问时填写，否则留空"
}

规则：
1. selected_candidate_id 必须来自候选列表；如果 action=close，也要选候选列表里对应的 close 候选；
2. 如果需要追问，优先选择 ask_user 候选，并把 ask_user_question 写清楚；
3. 如果只需要提醒，不要编造正式日程变更；
4. 如果候选里已经有明显更稳妥的项，优先选风险更低且 capacity_fit 更好的那个；
5. 不要试图重排整段日程，第一版只在候选之间做有限裁决；
6. 如果信息不足，就直接走 ask_user，不要硬猜。
`

type selectionPromptInput struct {
	GeneratedAt  string                 `json:"generated_at"`
	Trigger      selectionTriggerInput  `json:"trigger"`
	DecisionHint selectionDecisionInput `json:"decision_hint"`
	Context      selectionContextInput  `json:"context"`
	Candidates   []CandidateView        `json:"candidates"`
}

type selectionTriggerInput struct {
	TriggerID   string `json:"trigger_id"`
	TriggerType string `json:"trigger_type"`
	Source      string `json:"source"`
	TargetType  string `json:"target_type"`
	TargetID    int    `json:"target_id"`
	TargetTitle string `json:"target_title"`
	RequestedAt string `json:"requested_at"`
	TraceID     string `json:"trace_id"`
}

type selectionDecisionInput struct {
	Action              string `json:"action"`
	PrimaryIssueCode    string `json:"primary_issue_code"`
	ReasonCode          string `json:"reason_code"`
	ShouldNotify        bool   `json:"should_notify"`
	ShouldWritePreview  bool   `json:"should_write_preview"`
	FallbackCandidateID string `json:"fallback_candidate_id,omitempty"`
}

type selectionContextInput struct {
	WindowStart  string   `json:"window_start"`
	WindowEnd    string   `json:"window_end"`
	WindowReason string   `json:"window_reason"`
	MissingInfo  []string `json:"missing_info"`
	Warnings     []string `json:"warnings"`
	TraceSteps   []string `json:"trace_steps"`
}

func buildSelectionPromptInput(req SelectRequest, now time.Time) selectionPromptInput {
	activeContext := req.ActiveContext
	decision := req.Observation.Decision
	input := selectionPromptInput{
		GeneratedAt: now.In(time.Local).Format(time.RFC3339),
		DecisionHint: selectionDecisionInput{
			Action:              string(decision.Action),
			PrimaryIssueCode:    string(decision.PrimaryIssueCode),
			ReasonCode:          decision.ReasonCode,
			ShouldNotify:        decision.ShouldNotify,
			ShouldWritePreview:  decision.ShouldWritePreview,
			FallbackCandidateID: strings.TrimSpace(decision.FallbackCandidateID),
		},
	}
	if activeContext != nil {
		input.Trigger = selectionTriggerInput{
			TriggerID:   activeContext.Trigger.TriggerID,
			TriggerType: string(activeContext.Trigger.TriggerType),
			Source:      string(activeContext.Trigger.Source),
			TargetType:  string(activeContext.Trigger.TargetType),
			TargetID:    activeContext.Trigger.TargetID,
			TargetTitle: activeContext.Target.Title,
			RequestedAt: activeContext.Trigger.RequestedAt.In(time.Local).Format(time.RFC3339),
			TraceID:     activeContext.Trace.TraceID,
		}
		input.Context = selectionContextInput{
			WindowStart:  activeContext.Window.StartAt.In(time.Local).Format(time.RFC3339),
			WindowEnd:    activeContext.Window.EndAt.In(time.Local).Format(time.RFC3339),
			WindowReason: activeContext.Window.WindowReason,
			MissingInfo:  append([]string(nil), activeContext.DerivedFacts.MissingInfo...),
			Warnings:     append([]string(nil), activeContext.Trace.Warnings...),
			TraceSteps:   append([]string(nil), activeContext.Trace.BuildSteps...),
		}
	}

	input.Candidates = make([]CandidateView, 0, len(req.Candidates))
	for _, item := range req.Candidates {
		input.Candidates = append(input.Candidates, buildCandidateView(req, item))
	}
	return input
}

func buildSelectionUserPrompt(input selectionPromptInput) (string, error) {
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("请基于下面的结构化事实，从候选中选一个最合适的结果。\n")
	sb.WriteString("输入：\n")
	sb.WriteString(string(raw))
	sb.WriteString("\n")
	sb.WriteString("只输出 JSON。")
	return sb.String(), nil
}

func buildCandidateView(req SelectRequest, item candidate.Candidate) CandidateView {
	return CandidateView{
		CandidateID:   item.CandidateID,
		CandidateType: string(item.CandidateType),
		Title:         item.Title,
		Summary:       item.Summary,
		BeforeSummary: item.BeforeSummary,
		AfterSummary:  item.AfterSummary,
		ChangeSummary: buildChangeSummary(item),
		CapacityFit:   deriveCapacityFit(req, item),
		RiskLevel:     deriveRiskLevel(req, item),
	}
}

func buildChangeSummary(item candidate.Candidate) string {
	if len(item.Changes) == 0 {
		return "无正式变更"
	}
	lines := make([]string, 0, len(item.Changes))
	for _, change := range item.Changes {
		lines = append(lines, summarizeChange(change))
	}
	return strings.Join(lines, "；")
}

func summarizeChange(change candidate.ChangeItem) string {
	switch change.ChangeType {
	case candidate.ChangeTypeAdd:
		if change.ToSlot != nil {
			return fmt.Sprintf("新增到 第%d周 第%d天 第%d-%d节，持续%d节",
				change.ToSlot.Start.Week,
				change.ToSlot.Start.DayOfWeek,
				change.ToSlot.Start.Section,
				change.ToSlot.End.Section,
				change.DurationSections,
			)
		}
		return "新增一段日程"
	case candidate.ChangeTypeCreateMakeup:
		if change.ToSlot != nil {
			return fmt.Sprintf("为目标补做一段第%d周第%d天第%d-%d节的时间块",
				change.ToSlot.Start.Week,
				change.ToSlot.Start.DayOfWeek,
				change.ToSlot.Start.Section,
				change.ToSlot.End.Section,
			)
		}
		return "新增补做块"
	case candidate.ChangeTypeAskUser:
		return "需要用户补充信息"
	default:
		return "不修改正式日程"
	}
}

func deriveCapacityFit(req SelectRequest, item candidate.Candidate) string {
	switch item.CandidateType {
	case candidate.TypeAskUser, candidate.TypeNotifyOnly, candidate.TypeClose:
		return "not_applicable"
	}

	if !item.Validation.Valid {
		return "insufficient"
	}

	gap := req.Observation.Metrics.Window.CapacityGap
	switch {
	case gap > 0:
		return "insufficient"
	case gap == 0:
		return "tight"
	default:
		return "fit"
	}
}

func deriveRiskLevel(req SelectRequest, item candidate.Candidate) string {
	if !item.Validation.Valid {
		return "high"
	}
	switch item.CandidateType {
	case candidate.TypeCreateMakeup:
		return "medium"
	case candidate.TypeAddTaskPoolToSchedule:
		if req.Observation.Metrics.Window.CapacityGap == 0 {
			return "medium"
		}
		return "low"
	case candidate.TypeAskUser, candidate.TypeNotifyOnly, candidate.TypeClose:
		return "low"
	default:
		return "low"
	}
}
