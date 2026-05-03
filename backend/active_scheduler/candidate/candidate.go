package candidate

import (
	"fmt"
	"sort"

	schedulercontext "github.com/LoveLosita/smartflow/backend/active_scheduler/context"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/observe"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/ports"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
)

type Type string

const (
	TypeAddTaskPoolToSchedule       Type = "add_task_pool_to_schedule"
	TypeCreateMakeup                Type = "create_makeup"
	TypeAskUser                     Type = "ask_user"
	TypeNotifyOnly                  Type = "notify_only"
	TypeClose                       Type = "close"
	TypeCompressWithNextDynamicTask Type = "compress_with_next_dynamic_task" // 预留常量：第一版禁止生成该候选。
)

type ChangeType string

const (
	ChangeTypeAdd          ChangeType = "add"
	ChangeTypeCreateMakeup ChangeType = "create_makeup"
	ChangeTypeAskUser      ChangeType = "ask_user"
	ChangeTypeNone         ChangeType = "none"
)

// Candidate 是主动调度后端确定性生成的候选。
//
// 职责边界：
// 1. 只描述可写 preview 的结构化变更或非变更建议；
// 2. 不包含 DAO model，不直接修改正式日程；
// 3. 第一版不会生成 compress_with_next_dynamic_task。
type Candidate struct {
	CandidateID   string
	CandidateType Type
	Title         string
	Summary       string
	Target        Target
	Changes       []ChangeItem
	BeforeSummary string
	AfterSummary  string
	Risk          string
	Score         int
	Validation    Validation
	Source        string
}

type Target struct {
	TargetType string
	TargetID   int
	Title      string
}

type ChangeItem struct {
	ChangeType       ChangeType
	TargetType       string
	TargetID         int
	FromSlot         *ports.Slot
	ToSlot           *ports.SlotSpan
	DurationSections int
	AffectedEventIDs []int
	EditedAllowed    bool
	Metadata         map[string]string
}

type Validation struct {
	Valid  bool
	Reason string
}

// Generator 负责枚举、校验、排序并截断候选。
//
// 职责边界：
// 1. 只消费 context 和 observe 结果；
// 2. 不调用 LLM，不写 preview，不发通知；
// 3. 校验失败的候选直接丢弃，避免把合法性判断交给后续选择器。
type Generator struct{}

func NewGenerator() *Generator {
	return &Generator{}
}

// GenerateCandidates 执行 dry-run 主链路第三步：生成候选。
func (g *Generator) GenerateCandidates(ctx *schedulercontext.ActiveScheduleContext, observation observe.Result) []Candidate {
	var candidates []Candidate
	for _, issue := range observation.Issues {
		switch issue.Code {
		case observe.IssueTargetCompleted, observe.IssueTargetAlreadyScheduled:
			candidates = append(candidates, closeCandidate(ctx, issue))
		case observe.IssueFeedbackTargetUnknown:
			candidates = append(candidates, askUserCandidate(ctx, issue, "我还不能确定是哪一个日程块没有完成，需要用户确认目标。"))
		case observe.IssueCanAddTaskPoolToSchedule:
			if candidate, ok := g.addTaskPoolCandidate(ctx); ok {
				candidates = append(candidates, candidate)
			}
		case observe.IssueNeedMakeupBlock:
			if candidate, ok := g.createMakeupCandidate(ctx); ok {
				candidates = append(candidates, candidate)
			}
		case observe.IssueNoFreeSlot, observe.IssueCapacityInsufficient:
			candidates = append(candidates, notifyOnlyCandidate(ctx, issue, "当前 24 小时内没有足够空位，第一版不会生成压缩融合候选。"))
		case observe.IssueNoValidTimeWindow:
			candidates = append(candidates, askUserCandidate(ctx, issue, "缺少必要时间窗或目标事实，需要用户补充后再安排。"))
		}
	}
	return trimCandidates(rankCandidates(validateCandidates(candidates)))
}

func (g *Generator) addTaskPoolCandidate(ctx *schedulercontext.ActiveScheduleContext) (Candidate, bool) {
	needed := ctx.Target.EstimatedSections
	if needed <= 0 {
		needed = 1
	}
	span, ok := firstContiguousFreeSpan(ctx.ScheduleFacts.FreeSlots, needed)
	if !ok {
		return Candidate{}, false
	}
	id := fmt.Sprintf("%s:%d:%d:%d:%d", TypeAddTaskPoolToSchedule, ctx.Target.TaskID, span.Start.Week, span.Start.DayOfWeek, span.Start.Section)
	return Candidate{
		CandidateID:   id,
		CandidateType: TypeAddTaskPoolToSchedule,
		Title:         "加入日程",
		Summary:       "把重要且紧急任务放入滚动 24 小时内的空闲节次。",
		Target:        targetFromContext(ctx),
		Changes: []ChangeItem{{
			ChangeType:       ChangeTypeAdd,
			TargetType:       string(trigger.TargetTypeTaskPool),
			TargetID:         ctx.Target.TaskID,
			ToSlot:           &span,
			DurationSections: needed,
			EditedAllowed:    true,
			Metadata: map[string]string{
				"task_source_type": string(trigger.TargetTypeTaskPool),
			},
		}},
		BeforeSummary: "任务尚未进入正式日程。",
		AfterSummary:  "任务将占用第一个可用连续节次块。",
		Risk:          "仅新增 task_pool 日程块，不移动已有日程。",
		Score:         100 - span.Start.Section,
		Validation:    Validation{Valid: true},
		Source:        "backend_deterministic",
	}, true
}

func (g *Generator) createMakeupCandidate(ctx *schedulercontext.ActiveScheduleContext) (Candidate, bool) {
	if ctx == nil || ctx.FeedbackFacts.TargetTaskItemID <= 0 {
		return Candidate{}, false
	}
	span, ok := firstContiguousFreeSpan(ctx.ScheduleFacts.FreeSlots, 1)
	if !ok {
		return Candidate{}, false
	}
	targetID := ctx.FeedbackFacts.TargetEventID
	if targetID <= 0 {
		targetID = ctx.Trigger.TargetID
	}
	id := fmt.Sprintf("%s:%d:%d:%d:%d", TypeCreateMakeup, targetID, span.Start.Week, span.Start.DayOfWeek, span.Start.Section)
	return Candidate{
		CandidateID:   id,
		CandidateType: TypeCreateMakeup,
		Title:         "新增补做块",
		Summary:       "为未完成的日程块新增一个补做时间，不移动原任务。",
		Target:        targetFromContext(ctx),
		Changes: []ChangeItem{{
			ChangeType:       ChangeTypeCreateMakeup,
			TargetType:       string(trigger.TargetTypeScheduleEvent),
			TargetID:         targetID,
			ToSlot:           &span,
			DurationSections: 1,
			AffectedEventIDs: []int{targetID},
			EditedAllowed:    true,
			Metadata: map[string]string{
				"makeup_for_event_id": fmt.Sprintf("%d", targetID),
			},
		}},
		BeforeSummary: "用户反馈该日程块未完成。",
		AfterSummary:  "新增 1 节补做块，原日程不移动。",
		Risk:          "第一版不做局部重排；若补做块仍不合适，需要用户手动调整。",
		Score:         90 - span.Start.Section,
		Validation:    Validation{Valid: true},
		Source:        "backend_deterministic",
	}, true
}

func closeCandidate(ctx *schedulercontext.ActiveScheduleContext, issue observe.Issue) Candidate {
	return Candidate{
		CandidateID:   fmt.Sprintf("%s:%s:%d", TypeClose, ctx.Trigger.TargetType, ctx.Trigger.TargetID),
		CandidateType: TypeClose,
		Title:         "关闭主动调度",
		Summary:       issue.Reason,
		Target:        targetFromContext(ctx),
		Changes: []ChangeItem{{
			ChangeType: ChangeTypeNone,
			TargetType: string(ctx.Trigger.TargetType),
			TargetID:   ctx.Trigger.TargetID,
		}},
		BeforeSummary: "当前事实已覆盖触发原因。",
		AfterSummary:  "无需生成预览或通知。",
		Risk:          "无正式日程变更。",
		Score:         0,
		Validation:    Validation{Valid: true},
		Source:        "backend_deterministic",
	}
}

func askUserCandidate(ctx *schedulercontext.ActiveScheduleContext, issue observe.Issue, summary string) Candidate {
	return Candidate{
		CandidateID:   fmt.Sprintf("%s:%s:%d", TypeAskUser, issue.Code, ctx.Trigger.TargetID),
		CandidateType: TypeAskUser,
		Title:         "需要用户确认",
		Summary:       summary,
		Target:        targetFromContext(ctx),
		Changes: []ChangeItem{{
			ChangeType: ChangeTypeAskUser,
			TargetType: string(ctx.Trigger.TargetType),
			TargetID:   ctx.Trigger.TargetID,
		}},
		BeforeSummary: "缺少安全生成调整方案所需的事实。",
		AfterSummary:  "等待用户补充信息后再重新 dry-run。",
		Risk:          "不会修改正式日程。",
		Score:         0,
		Validation:    Validation{Valid: true},
		Source:        "backend_deterministic",
	}
}

func notifyOnlyCandidate(ctx *schedulercontext.ActiveScheduleContext, issue observe.Issue, summary string) Candidate {
	return Candidate{
		CandidateID:   fmt.Sprintf("%s:%s:%d", TypeNotifyOnly, issue.Code, ctx.Trigger.TargetID),
		CandidateType: TypeNotifyOnly,
		Title:         "仅提醒",
		Summary:       summary,
		Target:        targetFromContext(ctx),
		Changes: []ChangeItem{{
			ChangeType: ChangeTypeNone,
			TargetType: string(ctx.Trigger.TargetType),
			TargetID:   ctx.Trigger.TargetID,
		}},
		BeforeSummary: "当前窗口没有可安全安排的连续空位。",
		AfterSummary:  "不生成压缩融合或正式变更。",
		Risk:          "任务可能继续保持未安排状态。",
		Score:         0,
		Validation:    Validation{Valid: true},
		Source:        "backend_deterministic",
	}
}

func targetFromContext(ctx *schedulercontext.ActiveScheduleContext) Target {
	return Target{
		TargetType: string(ctx.Trigger.TargetType),
		TargetID:   ctx.Trigger.TargetID,
		Title:      ctx.Target.Title,
	}
}

func firstContiguousFreeSpan(slots []ports.Slot, needed int) (ports.SlotSpan, bool) {
	if needed <= 0 {
		return ports.SlotSpan{}, false
	}
	sorted := append([]ports.Slot(nil), slots...)
	sort.Slice(sorted, func(i, j int) bool {
		return slotLess(sorted[i], sorted[j])
	})
	for i := range sorted {
		end := i + needed - 1
		if end >= len(sorted) {
			break
		}
		if isContiguous(sorted[i : end+1]) {
			return ports.SlotSpan{Start: sorted[i], End: sorted[end], DurationSections: needed}, true
		}
	}
	return ports.SlotSpan{}, false
}

func isContiguous(slots []ports.Slot) bool {
	if len(slots) == 0 {
		return false
	}
	for i := 1; i < len(slots); i++ {
		prev := slots[i-1]
		curr := slots[i]
		if prev.Week != curr.Week || prev.DayOfWeek != curr.DayOfWeek || prev.Section+1 != curr.Section {
			return false
		}
	}
	return true
}

func slotLess(left, right ports.Slot) bool {
	if !left.StartAt.IsZero() && !right.StartAt.IsZero() && !left.StartAt.Equal(right.StartAt) {
		return left.StartAt.Before(right.StartAt)
	}
	if left.Week != right.Week {
		return left.Week < right.Week
	}
	if left.DayOfWeek != right.DayOfWeek {
		return left.DayOfWeek < right.DayOfWeek
	}
	return left.Section < right.Section
}

func validateCandidates(candidates []Candidate) []Candidate {
	valid := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.CandidateType == TypeCompressWithNextDynamicTask {
			// 1. 压缩融合只作为 schema 预留；
			// 2. 第一版 dry-run 禁止生成，防止后续 preview/apply 误认为可以执行。
			continue
		}
		if candidate.CandidateID == "" || candidate.CandidateType == "" {
			continue
		}
		if candidate.CandidateType == TypeAddTaskPoolToSchedule && len(candidate.Changes) == 0 {
			continue
		}
		valid = append(valid, candidate)
	}
	return valid
}

func rankCandidates(candidates []Candidate) []Candidate {
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	return candidates
}

func trimCandidates(candidates []Candidate) []Candidate {
	if len(candidates) <= 3 {
		return candidates
	}
	return candidates[:3]
}
