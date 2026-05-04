package observe

import (
	"time"

	schedulercontext "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/context"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
)

type DecisionAction string

const (
	DecisionActionClose           DecisionAction = "close"
	DecisionActionAskUser         DecisionAction = "ask_user"
	DecisionActionNotifyOnly      DecisionAction = "notify_only"
	DecisionActionSelectCandidate DecisionAction = "select_candidate"
)

type IssueCode string

const (
	IssueTargetCompleted                IssueCode = "target_completed"
	IssueTargetAlreadyScheduled         IssueCode = "target_already_scheduled"
	IssueNoValidTimeWindow              IssueCode = "no_valid_time_window"
	IssueCapacityInsufficient           IssueCode = "capacity_insufficient"
	IssueNoFreeSlot                     IssueCode = "no_free_slot"
	IssueFeedbackTargetUnknown          IssueCode = "feedback_target_unknown"
	IssueNeedMakeupBlock                IssueCode = "need_makeup_block"
	IssueCanAddTaskPoolToSchedule       IssueCode = "can_add_task_pool_to_schedule"
	IssueCanCompressWithNextDynamicTask IssueCode = "can_compress_with_next_dynamic_task"
)

// Metrics 是主动观测阶段输出的事实指标。
type Metrics struct {
	Target   TargetMetrics
	Window   WindowMetrics
	Feedback FeedbackMetrics
	Risk     RiskMetrics
}

type TargetMetrics struct {
	Completed             bool
	AlreadyScheduled      bool
	DeadlineAlreadyPassed bool
	MinutesToDeadline     int
	EstimatedSections     int
}

type WindowMetrics struct {
	TotalSlots                int
	FreeSlots                 int
	OccupiedSlots             int
	UsableSlotsBeforeDeadline int
	CapacityGap               int
}

type FeedbackMetrics struct {
	HasFeedback              bool
	FeedbackTargetKnown      bool
	UnfinishedElapsedMinutes int
}

type RiskMetrics struct {
	ConflictCount      int
	AffectedEventCount int
	AffectedTaskCount  int
	RequiresReorder    bool
}

type Issue struct {
	IssueID              string
	Code                 IssueCode
	Severity             string
	TargetType           string
	TargetID             int
	Reason               string
	Evidence             map[string]string
	CanGenerateCandidate bool
}

type Decision struct {
	Action               DecisionAction
	ReasonCode           string
	PrimaryIssueCode     IssueCode
	ShouldNotify         bool
	ShouldWritePreview   bool
	LLMSelectionRequired bool
	FallbackCandidateID  string
}

type Result struct {
	Metrics  Metrics
	Issues   []Issue
	Decision Decision
	Trace    []string
}

// Analyzer 负责把 ActiveScheduleContext 转成确定性观测结果。
//
// 职责边界：
// 1. 只生成 metrics / issues / 初步 decision；
// 2. 不枚举候选，不调用 LLM；
// 3. 候选生成后由 FinalizeDecision 根据候选数量收口最终 action。
type Analyzer struct{}

func NewAnalyzer() *Analyzer {
	return &Analyzer{}
}

// Observe 执行主动观测。
func (a *Analyzer) Observe(ctx *schedulercontext.ActiveScheduleContext) Result {
	result := Result{
		Metrics: buildMetrics(ctx),
		Trace: []string{
			"1. 基于上下文构造 metrics，保证后续裁决只依赖结构化事实。",
			"2. 按触发类型检测 issue，不在观测阶段修改正式日程。",
		},
	}
	result.Issues = detectIssues(ctx, result.Metrics)
	result.Decision = provisionalDecision(result.Issues)
	return result
}

// FinalizeDecision 根据候选生成结果收口最终裁决。
//
// 职责边界：
// 1. 只根据后端已生成、已校验的候选收口 decision；
// 2. 不改变候选内容；
// 3. 候选为空时不能写 preview，必须降级为 ask_user / notify_only / close。
func (a *Analyzer) FinalizeDecision(result Result, candidateCount int, fallbackCandidateID string) Result {
	if len(result.Issues) == 0 {
		result.Decision = Decision{Action: DecisionActionClose, ReasonCode: "no_issue"}
		return result
	}
	primary := result.Issues[0].Code
	if hasIssue(result.Issues, IssueTargetCompleted) || hasIssue(result.Issues, IssueTargetAlreadyScheduled) {
		result.Decision = Decision{Action: DecisionActionClose, ReasonCode: string(primary), PrimaryIssueCode: primary}
		return result
	}
	if hasIssue(result.Issues, IssueFeedbackTargetUnknown) || hasIssue(result.Issues, IssueNoValidTimeWindow) {
		result.Decision = Decision{Action: DecisionActionAskUser, ReasonCode: string(primary), PrimaryIssueCode: primary, ShouldNotify: true}
		return result
	}
	if candidateCount > 0 {
		result.Decision = Decision{
			Action:               DecisionActionSelectCandidate,
			ReasonCode:           "candidate_available",
			PrimaryIssueCode:     primary,
			ShouldNotify:         true,
			ShouldWritePreview:   true,
			LLMSelectionRequired: true,
			FallbackCandidateID:  fallbackCandidateID,
		}
		return result
	}
	result.Decision = Decision{Action: DecisionActionNotifyOnly, ReasonCode: string(primary), PrimaryIssueCode: primary, ShouldNotify: true}
	return result
}

func buildMetrics(ctx *schedulercontext.ActiveScheduleContext) Metrics {
	estimated := ctx.Target.EstimatedSections
	if estimated <= 0 {
		estimated = 1
	}
	usable := countUsableSlots(ctx)
	deadlinePassed := false
	minutesToDeadline := 0
	if ctx.Target.DeadlineAt != nil {
		deadlinePassed = ctx.Target.DeadlineAt.Before(ctx.Now.EffectiveNow)
		minutesToDeadline = int(ctx.Target.DeadlineAt.Sub(ctx.Now.EffectiveNow).Minutes())
	}
	return Metrics{
		Target: TargetMetrics{
			Completed:             ctx.DerivedFacts.TargetCompleted,
			AlreadyScheduled:      ctx.DerivedFacts.TargetAlreadyScheduled,
			DeadlineAlreadyPassed: deadlinePassed,
			MinutesToDeadline:     minutesToDeadline,
			EstimatedSections:     estimated,
		},
		Window: WindowMetrics{
			TotalSlots:                len(ctx.ScheduleFacts.FreeSlots) + len(ctx.ScheduleFacts.OccupiedSlots),
			FreeSlots:                 len(ctx.ScheduleFacts.FreeSlots),
			OccupiedSlots:             len(ctx.ScheduleFacts.OccupiedSlots),
			UsableSlotsBeforeDeadline: usable,
			CapacityGap:               estimated - usable,
		},
		Feedback: FeedbackMetrics{
			HasFeedback:         ctx.Trigger.TriggerType == trigger.TriggerTypeUnfinishedFeedback && ctx.FeedbackFacts.FeedbackID != "",
			FeedbackTargetKnown: ctx.FeedbackFacts.TargetKnown,
		},
		Risk: RiskMetrics{
			AffectedEventCount: len(ctx.ScheduleFacts.Events),
			RequiresReorder:    ctx.Trigger.TriggerType == trigger.TriggerTypeUnfinishedFeedback && len(ctx.ScheduleFacts.FreeSlots) == 0,
		},
	}
}

func countUsableSlots(ctx *schedulercontext.ActiveScheduleContext) int {
	if ctx.Target.DeadlineAt == nil {
		return len(ctx.ScheduleFacts.FreeSlots)
	}
	count := 0
	for _, slot := range ctx.ScheduleFacts.FreeSlots {
		if slot.StartAt.IsZero() || !slot.StartAt.After(*ctx.Target.DeadlineAt) {
			count++
		}
	}
	return count
}

func detectIssues(ctx *schedulercontext.ActiveScheduleContext, metrics Metrics) []Issue {
	switch ctx.Trigger.TriggerType {
	case trigger.TriggerTypeImportantUrgentTask:
		return detectImportantUrgentIssues(ctx, metrics)
	case trigger.TriggerTypeUnfinishedFeedback:
		return detectUnfinishedFeedbackIssues(ctx, metrics)
	default:
		return []Issue{}
	}
}

func detectImportantUrgentIssues(ctx *schedulercontext.ActiveScheduleContext, metrics Metrics) []Issue {
	if metrics.Target.Completed {
		return []Issue{newIssue(IssueTargetCompleted, ctx, "目标任务已完成，主动调度无需继续处理。", false)}
	}
	if metrics.Target.AlreadyScheduled {
		return []Issue{newIssue(IssueTargetAlreadyScheduled, ctx, "目标任务已经进入日程，不能重复加入 task_pool。", false)}
	}
	if len(ctx.DerivedFacts.MissingInfo) > 0 {
		return []Issue{newIssue(IssueNoValidTimeWindow, ctx, "缺少目标任务或时间窗事实，需要用户补充信息。", false)}
	}
	if metrics.Window.FreeSlots == 0 {
		return []Issue{newIssue(IssueNoFreeSlot, ctx, "滚动 24 小时内没有可用节次。", false)}
	}
	if metrics.Window.CapacityGap > 0 {
		return []Issue{newIssue(IssueCapacityInsufficient, ctx, "可用节次不足以完整放入目标任务。", false)}
	}
	return []Issue{newIssue(IssueCanAddTaskPoolToSchedule, ctx, "目标任务可加入滚动 24 小时内的空闲节次。", true)}
}

func detectUnfinishedFeedbackIssues(ctx *schedulercontext.ActiveScheduleContext, metrics Metrics) []Issue {
	if !metrics.Feedback.HasFeedback || !metrics.Feedback.FeedbackTargetKnown {
		return []Issue{newIssue(IssueFeedbackTargetUnknown, ctx, "无法确定用户反馈的未完成日程块，需要进一步确认。", false)}
	}
	if metrics.Window.FreeSlots == 0 {
		return []Issue{newIssue(IssueNoFreeSlot, ctx, "反馈目标已定位，但滚动 24 小时内没有补做空位。", false)}
	}
	return []Issue{newIssue(IssueNeedMakeupBlock, ctx, "反馈目标已定位，可生成新增补做块候选。", true)}
}

func newIssue(code IssueCode, ctx *schedulercontext.ActiveScheduleContext, reason string, canGenerate bool) Issue {
	return Issue{
		IssueID:    string(code) + ":1",
		Code:       code,
		Severity:   issueSeverity(code),
		TargetType: string(ctx.Trigger.TargetType),
		TargetID:   ctx.Trigger.TargetID,
		Reason:     reason,
		Evidence: map[string]string{
			"trigger_type": string(ctx.Trigger.TriggerType),
			"window_start": ctx.Window.StartAt.Format(time.RFC3339),
			"window_end":   ctx.Window.EndAt.Format(time.RFC3339),
		},
		CanGenerateCandidate: canGenerate,
	}
}

func issueSeverity(code IssueCode) string {
	switch code {
	case IssueTargetCompleted, IssueTargetAlreadyScheduled:
		return "info"
	case IssueFeedbackTargetUnknown, IssueNoValidTimeWindow:
		return "warning"
	default:
		return "critical"
	}
}

func provisionalDecision(issues []Issue) Decision {
	if len(issues) == 0 {
		return Decision{Action: DecisionActionClose, ReasonCode: "no_issue"}
	}
	return Decision{Action: DecisionActionNotifyOnly, ReasonCode: "pending_candidates", PrimaryIssueCode: issues[0].Code}
}

func hasIssue(issues []Issue, code IssueCode) bool {
	for _, issue := range issues {
		if issue.Code == code {
			return true
		}
	}
	return false
}
