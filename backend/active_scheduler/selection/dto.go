package selection

import (
	"github.com/LoveLosita/smartflow/backend/active_scheduler/candidate"
	schedulercontext "github.com/LoveLosita/smartflow/backend/active_scheduler/context"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/observe"
)

const (
	ActionSelectCandidate = "select_candidate"
	ActionAskUser         = "ask_user"
	ActionNotifyOnly      = "notify_only"
	ActionClose           = "close"
)

// SelectRequest 是主动调度选择器的输入。
//
// 职责边界：
// 1. 只承载已经由 dry-run 生成并校验过的上下文、观测结果和候选；
// 2. 不包含任何模型实例，不负责 prompt 拼接；
// 3. 由 graph runner 在 dry-run 之后传入，避免选择器直接回查数据库。
type SelectRequest struct {
	ActiveContext *schedulercontext.ActiveScheduleContext `json:"-"`
	Observation   observe.Result                          `json:"-"`
	Candidates    []candidate.Candidate                   `json:"-"`
}

// Result 是选择器的结构化输出。
//
// 职责边界：
// 1. 只记录最终选中的候选与给用户看的解释摘要；
// 2. 不包含正式日程写入结果，也不包含通知投递结果；
// 3. FallbackUsed 只表示本次是否回退到了确定性兜底，不允许靠 selected_candidate_id 推断。
type Result struct {
	Action              string  `json:"action"`
	SelectedCandidateID string  `json:"selected_candidate_id,omitempty"`
	Reason              string  `json:"reason,omitempty"`
	ExplanationText     string  `json:"explanation_text,omitempty"`
	NotificationSummary string  `json:"notification_summary,omitempty"`
	AskUserQuestion     string  `json:"ask_user_question,omitempty"`
	FallbackUsed        bool    `json:"fallback_used,omitempty"`
	Confidence          float64 `json:"confidence,omitempty"`
}

// CandidateView 是暴露给 LLM 的最小候选视图。
//
// 职责边界：
// 1. 只保留用于有限选择的基础信息和少量结构化维度；
// 2. 不暴露 score / validation 这类内部实现细节；
// 3. 不直接携带原始日程事实，避免模型看到过多上下文。
type CandidateView struct {
	CandidateID   string `json:"candidate_id"`
	CandidateType string `json:"candidate_type"`
	Title         string `json:"title"`
	Summary       string `json:"summary"`
	BeforeSummary string `json:"before_summary"`
	AfterSummary  string `json:"after_summary"`
	ChangeSummary string `json:"change_summary"`
	CapacityFit   string `json:"capacity_fit"`
	RiskLevel     string `json:"risk_level"`
}
