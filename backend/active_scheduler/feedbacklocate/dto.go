package feedbacklocate

import "strings"

const (
	// ActionSelectCandidate 表示模型已经把补充信息定位到某个 schedule_event。
	ActionSelectCandidate = "select_candidate"
	// ActionAskUser 表示模型无法稳定定位，需要继续追问用户。
	ActionAskUser = "ask_user"

	// TargetTypeScheduleEvent 是本阶段允许返回的唯一目标类型。
	TargetTypeScheduleEvent = "schedule_event"
)

// Request 是反馈定位节点的最小输入。
//
// 职责边界：
// 1. 只承载定位当前补充信息所需的上下文，不携带正式排程写入能力。
// 2. 不负责候选筛选或 preview 落库，最终只返回“定位成功”或“继续追问”。
type Request struct {
	UserID          int
	UserMessage     string
	PendingQuestion string
	MissingInfo     []string
}

// Result 是反馈定位节点的最小输出。
//
// 职责边界：
// 1. 只表达“是否已经定位到 schedule_event”以及“是否需要继续 ask_user”。
// 2. 不携带正式日程写入结果，也不直接产出 preview。
type Result struct {
	Action          string
	TargetType      string
	TargetID        int
	Reason          string
	AskUserQuestion string
}

// IsResolved 表示本次定位是否已经拿到可校验的 schedule_event。
//
// 输入输出语义：
// 1. 只有 action=select_candidate 且 target_type=schedule_event 且 target_id>0 才算成功。
// 2. 其余情况都视为需要继续 ask_user。
func (r Result) IsResolved() bool {
	return strings.EqualFold(strings.TrimSpace(r.Action), ActionSelectCandidate) &&
		strings.EqualFold(strings.TrimSpace(r.TargetType), TargetTypeScheduleEvent) &&
		r.TargetID > 0
}

// ShouldAskUser 表示本次定位是否应该回退为追问。
func (r Result) ShouldAskUser() bool {
	return !r.IsResolved()
}

type promptInput struct {
	GeneratedAt     string            `json:"generated_at"`
	UserMessage     string            `json:"user_message"`
	PendingQuestion string            `json:"pending_question,omitempty"`
	MissingInfo     []string          `json:"missing_info,omitempty"`
	Window          promptWindowInput `json:"window"`
	Candidates      []eventCandidate  `json:"candidates"`
}

type promptWindowInput struct {
	StartAt string `json:"start_at"`
	EndAt   string `json:"end_at"`
}

type eventCandidate struct {
	TargetID    int    `json:"target_id"`
	Title       string `json:"title"`
	SourceType  string `json:"source_type,omitempty"`
	RelatedID   int    `json:"related_id,omitempty"`
	SlotSummary string `json:"slot_summary,omitempty"`
}

type llmResponse struct {
	Action          string `json:"action"`
	TargetType      string `json:"target_type"`
	TargetID        int    `json:"target_id"`
	Reason          string `json:"reason"`
	AskUserQuestion string `json:"ask_user_question"`
}
