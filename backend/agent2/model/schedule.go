package agentmodel

import (
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	// SchedulePlanTimezoneName 是排程链路默认业务时区。
	// 与随口记保持一致，固定东八区，避免容器运行在 UTC 导致“明天/今晚”偏移。
	SchedulePlanTimezoneName = "Asia/Shanghai"

	// SchedulePlanDatetimeLayout 是排程链路内部统一的分钟级时间格式。
	SchedulePlanDatetimeLayout = "2006-01-02 15:04"

	// SchedulePlanDefaultDailyRefineConcurrency 是日内并发优化默认并发度。
	// 这里给一个保守默认值，避免未配置时直接把模型并发打满导致限流。
	SchedulePlanDefaultDailyRefineConcurrency = 3

	// SchedulePlanDefaultWeeklyAdjustBudget 是周级配平默认调整额度。
	// 额度存在的目的：
	// 1. 防止周级 ReAct 过度调整导致震荡；
	// 2. 控制 token 与时延成本；
	// 3. 让方案改动更可解释。
	SchedulePlanDefaultWeeklyAdjustBudget = 5

	// SchedulePlanDefaultWeeklyTotalBudget 是周级“总尝试次数”默认预算。
	//
	// 设计意图：
	// 1. 总预算统计“动作尝试次数”（成功/失败都记一次）；
	// 2. 有效预算统计“成功动作次数”（仅成功时记一次）；
	// 3. 通过双预算把“探索次数”和“有效改动次数”分离，降低模型无效空转成本。
	SchedulePlanDefaultWeeklyTotalBudget = 8

	// SchedulePlanDefaultWeeklyRefineConcurrency 是周级“按周并发”默认并发度。
	// 说明：
	// 1. 周级输入规模通常比单天更大，默认并发度不宜过高，避免触发模型侧限流；
	// 2. 可在运行时按请求状态覆盖。
	SchedulePlanDefaultWeeklyRefineConcurrency = 2

	// SchedulePlanAdjustmentScopeSmall 表示“小改动微调”。
	// 语义：优先走快速路径，只做轻量周级调整。
	SchedulePlanAdjustmentScopeSmall = "small"
	// SchedulePlanAdjustmentScopeMedium 表示“中等改动微调”。
	// 语义：跳过日内拆分，直接进入周级配平。
	SchedulePlanAdjustmentScopeMedium = "medium"
	// SchedulePlanAdjustmentScopeLarge 表示“大改动重排”。
	// 语义：必要时重新走全量路径（日内并发 + 周级配平）。
	SchedulePlanAdjustmentScopeLarge = "large"
)

const (
	schedulePlanTimezoneName                   = SchedulePlanTimezoneName
	schedulePlanDatetimeLayout                 = SchedulePlanDatetimeLayout
	schedulePlanDefaultDailyRefineConcurrency  = SchedulePlanDefaultDailyRefineConcurrency
	schedulePlanDefaultWeeklyAdjustBudget      = SchedulePlanDefaultWeeklyAdjustBudget
	schedulePlanDefaultWeeklyTotalBudget       = SchedulePlanDefaultWeeklyTotalBudget
	schedulePlanDefaultWeeklyRefineConcurrency = SchedulePlanDefaultWeeklyRefineConcurrency
	schedulePlanAdjustmentScopeSmall           = SchedulePlanAdjustmentScopeSmall
	schedulePlanAdjustmentScopeMedium          = SchedulePlanAdjustmentScopeMedium
	schedulePlanAdjustmentScopeLarge           = SchedulePlanAdjustmentScopeLarge
)

// DayGroup 是“按天拆分后”的最小优化单元。
//
// 设计目的：
// 1. 把全量周视角数据拆成“单天小包”，降低日内 ReAct 输入规模；
// 2. 支持并发优化不同天的数据，缩短整体等待；
// 3. 通过 SkipRefine 让低收益天数直接跳过，节省模型调用成本。
type DayGroup struct {
	Week       int
	DayOfWeek  int
	Entries    []model.HybridScheduleEntry
	SkipRefine bool
}

// SchedulePlanState 是“智能排程”链路在 graph 节点间传递的统一状态容器。
//
// 设计目标：
// 1) 收拢排程请求全生命周期的上下文，降低节点间参数散落；
// 2) 支持“粗排 -> 日内并发优化 -> 周级配平 -> 终审校验”的完整链路追踪；
// 3) 支持连续对话微调：保留上版方案 + 本次约束变更，便于增量重排。
type SchedulePlanState struct {
	// ── 基础上下文 ──
	TraceID        string
	UserID         int
	ConversationID string
	RequestNow     time.Time
	RequestNowText string

	// ── plan 节点输出 ──
	UserIntent         string
	Constraints        []string
	TaskClassIDs       []int
	Strategy           string
	TaskTags           map[int]string
	TaskTagHintsByName map[string]string

	// ── preview 节点输出 ──
	CandidatePlans    []model.UserWeekSchedule
	AllocatedItems    []model.TaskClassItem
	HasPlanningWindow bool
	PlanStartWeek     int
	PlanStartDay      int
	PlanEndWeek       int
	PlanEndDay        int

	// ── 日内并发优化阶段 ──
	DailyGroups            map[int]map[int]*DayGroup
	DailyResults           map[int]map[int][]model.HybridScheduleEntry
	DailyRefineConcurrency int

	// ── 周级 ReAct 精排阶段 ──
	HybridEntries           []model.HybridScheduleEntry
	MergeSnapshot           []model.HybridScheduleEntry
	ReactRound              int
	ReactMaxRound           int
	ReactSummary            string
	ReactDone               bool
	WeeklyAdjustBudget      int
	WeeklyAdjustUsed        int
	WeeklyTotalBudget       int
	WeeklyTotalUsed         int
	WeeklyRefineConcurrency int
	WeeklyActionLogs        []string

	// ── 连续对话微调 ──
	PreviousPlanJSON       string
	IsAdjustment           bool
	RestartRequested       bool
	AdjustmentScope        string
	AdjustmentReason       string
	AdjustmentConfidence   float64
	HasPreviousPreview     bool
	PreviousTaskClassIDs   []int
	PreviousHybridEntries  []model.HybridScheduleEntry
	PreviousAllocatedItems []model.TaskClassItem
	PreviousCandidatePlans []model.UserWeekSchedule

	// ── 最终输出 ──
	FinalSummary string
	Completed    bool
}

// NewSchedulePlanState 创建排程状态对象并初始化默认值。
func NewSchedulePlanState(traceID string, userID int, conversationID string) *SchedulePlanState {
	now := schedulePlanNowToMinute()
	return &SchedulePlanState{
		TraceID:                 traceID,
		UserID:                  userID,
		ConversationID:          conversationID,
		RequestNow:              now,
		RequestNowText:          now.In(schedulePlanLocation()).Format(schedulePlanDatetimeLayout),
		Strategy:                "steady",
		TaskTags:                make(map[int]string),
		TaskTagHintsByName:      make(map[string]string),
		DailyRefineConcurrency:  schedulePlanDefaultDailyRefineConcurrency,
		WeeklyRefineConcurrency: schedulePlanDefaultWeeklyRefineConcurrency,
		AdjustmentScope:         schedulePlanAdjustmentScopeLarge,
		ReactMaxRound:           2,
		WeeklyAdjustBudget:      schedulePlanDefaultWeeklyAdjustBudget,
		WeeklyTotalBudget:       schedulePlanDefaultWeeklyTotalBudget,
	}
}

// NormalizeSchedulePlanAdjustmentScope 归一化排程微调力度字段。
//
// 兜底策略：
// 1. 只接受 small/medium/large；
// 2. 任何未知值都回退为 large，保证不会误走“过轻”路径。
func NormalizeSchedulePlanAdjustmentScope(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case schedulePlanAdjustmentScopeSmall:
		return schedulePlanAdjustmentScopeSmall
	case schedulePlanAdjustmentScopeMedium:
		return schedulePlanAdjustmentScopeMedium
	default:
		return schedulePlanAdjustmentScopeLarge
	}
}

// schedulePlanLocation 返回排程链路使用的业务时区。
func schedulePlanLocation() *time.Location {
	loc, err := time.LoadLocation(schedulePlanTimezoneName)
	if err != nil {
		return time.Local
	}
	return loc
}

// schedulePlanNowToMinute 返回当前时间并截断到分钟级。
func schedulePlanNowToMinute() time.Time {
	return time.Now().In(schedulePlanLocation()).Truncate(time.Minute)
}

func normalizeAdjustmentScope(raw string) string {
	return NormalizeSchedulePlanAdjustmentScope(raw)
}

// ScheduleRefineState 先保留现有骨架，避免本轮“只迁 schedule_plan”时误动 refine。
type ScheduleRefineState struct {
	TraceID        string
	UserID         int
	ConversationID string
	UserInput      string
	Completed      bool
	FinalSummary   string
}
