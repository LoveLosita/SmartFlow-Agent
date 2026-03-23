package schedulerefine

import (
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	// 固定业务时区，避免“今天/明天”在容器默认时区下偏移。
	timezoneName = "Asia/Shanghai"
	// 统一分钟级时间文本格式。
	datetimeLayout = "2006-01-02 15:04"

	// 预算默认值。
	defaultPlanMax        = 2
	defaultExecuteMax     = 24
	defaultPerTaskBudget  = 4
	defaultReplanMax      = 2
	defaultCompositeRetry = 2
	defaultRepairReserve  = 1
)

// RefineContract 表示本轮微调意图契约。
type RefineContract struct {
	Intent            string            `json:"intent"`
	Strategy          string            `json:"strategy"`
	HardRequirements  []string          `json:"hard_requirements"`
	HardAssertions    []RefineAssertion `json:"hard_assertions,omitempty"`
	KeepRelativeOrder bool              `json:"keep_relative_order"`
	OrderScope        string            `json:"order_scope"`
}

// RefineAssertion 表示可由后端直接判定的结构化硬断言。
//
// 字段说明：
// 1. Metric：断言指标名，例如 source_move_ratio_percent；
// 2. Operator：比较操作符，支持 == / <= / >= / between；
// 3. Value/Min/Max：阈值；
// 4. Week/TargetWeek：可选周次上下文。
type RefineAssertion struct {
	Metric     string `json:"metric"`
	Operator   string `json:"operator"`
	Value      int    `json:"value,omitempty"`
	Min        int    `json:"min,omitempty"`
	Max        int    `json:"max,omitempty"`
	Week       int    `json:"week,omitempty"`
	TargetWeek int    `json:"target_week,omitempty"`
}

// HardCheckReport 表示终审硬校验结果。
type HardCheckReport struct {
	PhysicsPassed bool     `json:"physics_passed"`
	PhysicsIssues []string `json:"physics_issues,omitempty"`

	IntentPassed bool     `json:"intent_passed"`
	IntentReason string   `json:"intent_reason,omitempty"`
	IntentUnmet  []string `json:"intent_unmet,omitempty"`

	OrderPassed bool     `json:"order_passed"`
	OrderIssues []string `json:"order_issues,omitempty"`

	RepairTried bool `json:"repair_tried"`
}

// ReactRoundObservation 记录每轮 ReAct 的关键观察。
type ReactRoundObservation struct {
	Round         int            `json:"round"`
	GoalCheck     string         `json:"goal_check,omitempty"`
	Decision      string         `json:"decision,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolParams    map[string]any `json:"tool_params,omitempty"`
	ToolSuccess   bool           `json:"tool_success"`
	ToolErrorCode string         `json:"tool_error_code,omitempty"`
	ToolResult    string         `json:"tool_result,omitempty"`
	Reflect       string         `json:"reflect,omitempty"`
}

// PlannerPlan 表示 Planner 生成的阶段执行计划。
type PlannerPlan struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps,omitempty"`
}

// RefineSlicePlan 表示切片节点输出。
type RefineSlicePlan struct {
	WeekFilter      []int  `json:"week_filter,omitempty"`
	SourceDays      []int  `json:"source_days,omitempty"`
	TargetDays      []int  `json:"target_days,omitempty"`
	ExcludeSections []int  `json:"exclude_sections,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// RefineObjective 表示“可执行且可校验”的目标约束。
//
// 设计说明：
// 1. 由 contract/slice 从自然语言编译得到；
// 2. 执行阶段（done 收口）与终审阶段（hard_check）共用同一份约束；
// 3. 避免“执行逻辑与终审逻辑各说各话”。
type RefineObjective struct {
	Mode string `json:"mode,omitempty"` // none | move_all | move_ratio

	SourceWeeks []int `json:"source_weeks,omitempty"`
	TargetWeeks []int `json:"target_weeks,omitempty"`
	SourceDays  []int `json:"source_days,omitempty"`
	TargetDays  []int `json:"target_days,omitempty"`

	ExcludeSections []int `json:"exclude_sections,omitempty"`

	BaselineSourceTaskCount int `json:"baseline_source_task_count,omitempty"`
	RequiredMoveMin         int `json:"required_move_min,omitempty"`
	RequiredMoveMax         int `json:"required_move_max,omitempty"`

	Reason string `json:"reason,omitempty"`
}

// ScheduleRefineState 是连续微调图的统一状态。
type ScheduleRefineState struct {
	// 1) 请求上下文
	TraceID        string
	UserID         int
	ConversationID string
	UserMessage    string
	RequestNow     time.Time
	RequestNowText string

	// 2) 继承自预览快照的数据
	TaskClassIDs []int
	Constraints  []string
	// InitialHybridEntries 保存本轮微调开始前的基线，用于终审做“前后对比”。
	// 说明：
	// 1. 只读语义，不参与执行期改写；
	// 2. 终审可基于它判断“来源任务是否真正迁移到目标区域”。
	InitialHybridEntries []model.HybridScheduleEntry
	HybridEntries        []model.HybridScheduleEntry
	AllocatedItems       []model.TaskClassItem
	CandidatePlans       []model.UserWeekSchedule

	// 3) 本轮执行状态
	UserIntent string
	Contract   RefineContract

	PlanMax       int
	PerTaskBudget int
	ExecuteMax    int
	ReplanMax     int
	// CompositeRetryMax 表示复合路由失败后的最大重试次数（不含首次尝试）。
	CompositeRetryMax int

	PlanUsed   int
	ReplanUsed int

	MaxRounds     int
	RepairReserve int
	RoundUsed     int
	ActionLogs    []string

	ConsecutiveFailures int
	ThinkingBoostArmed  bool
	ObservationHistory  []ReactRoundObservation

	CurrentPlan      PlannerPlan
	BatchMoveAllowed bool
	// DisableCompositeTools=true 表示已进入 ReAct 兜底，禁止再调用复合工具。
	DisableCompositeTools bool
	// CompositeRouteTried 标记是否尝试过“复合批处理路由”。
	CompositeRouteTried bool
	// CompositeRouteSucceeded 标记复合批处理路由是否成功收口。
	CompositeRouteSucceeded bool
	TaskActionUsed          map[int]int
	EntriesVersion          int
	SeenSlotQueries         map[string]struct{}

	// RequiredCompositeTool 表示本轮策略要求“必须至少成功一次”的复合工具。
	// 取值约定："" | "SpreadEven" | "MinContextSwitch"。
	RequiredCompositeTool string
	// CompositeToolCalled 记录复合工具是否至少调用过一次（不区分成功失败）。
	CompositeToolCalled map[string]bool
	// CompositeToolSuccess 记录复合工具是否至少成功过一次。
	CompositeToolSuccess map[string]bool

	SlicePlan          RefineSlicePlan
	Objective          RefineObjective
	WorksetTaskIDs     []int
	WorksetCursor      int
	CurrentTaskID      int
	CurrentTaskAttempt int

	LastFailedCallSignature string
	OriginOrderMap          map[int]int

	// 4) 终审状态
	HardCheck HardCheckReport

	// 5) 最终输出
	FinalSummary string
	Completed    bool
}

// NewScheduleRefineState 基于上一版预览快照初始化状态。
//
// 职责边界：
// 1. 负责初始化预算、上下文字段与可变状态容器；
// 2. 负责拷贝 preview 数据，避免跨请求引用污染；
// 3. 不负责做任何调度动作。
func NewScheduleRefineState(traceID string, userID int, conversationID string, userMessage string, preview *model.SchedulePlanPreviewCache) *ScheduleRefineState {
	now := nowToMinute()
	st := &ScheduleRefineState{
		TraceID:            strings.TrimSpace(traceID),
		UserID:             userID,
		ConversationID:     strings.TrimSpace(conversationID),
		UserMessage:        strings.TrimSpace(userMessage),
		RequestNow:         now,
		RequestNowText:     now.In(loadLocation()).Format(datetimeLayout),
		PlanMax:            defaultPlanMax,
		PerTaskBudget:      defaultPerTaskBudget,
		ExecuteMax:         defaultExecuteMax,
		ReplanMax:          defaultReplanMax,
		CompositeRetryMax:  defaultCompositeRetry,
		RepairReserve:      defaultRepairReserve,
		MaxRounds:          defaultExecuteMax + defaultRepairReserve,
		ActionLogs:         make([]string, 0, 32),
		ObservationHistory: make([]ReactRoundObservation, 0, 24),
		TaskActionUsed:     make(map[int]int),
		SeenSlotQueries:    make(map[string]struct{}),
		OriginOrderMap:     make(map[int]int),
		CompositeToolCalled: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		},
		CompositeToolSuccess: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		},
		CurrentPlan: PlannerPlan{
			Summary: "初始化完成，等待 Planner 生成执行计划。",
		},
		SlicePlan: RefineSlicePlan{
			Reason: "尚未切片",
		},
	}
	if preview == nil {
		return st
	}

	st.TaskClassIDs = append([]int(nil), preview.TaskClassIDs...)
	st.InitialHybridEntries = cloneHybridEntries(preview.HybridEntries)
	st.HybridEntries = cloneHybridEntries(preview.HybridEntries)
	st.AllocatedItems = cloneTaskClassItems(preview.AllocatedItems)
	st.CandidatePlans = cloneWeekSchedules(preview.CandidatePlans)
	st.OriginOrderMap = buildOriginOrderMap(st.HybridEntries)
	return st
}

func loadLocation() *time.Location {
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		return time.Local
	}
	return loc
}

func nowToMinute() time.Time {
	return time.Now().In(loadLocation()).Truncate(time.Minute)
}

func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.HybridScheduleEntry, len(src))
	copy(dst, src)
	return dst
}

func cloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.TaskClassItem, 0, len(src))
	for _, item := range src {
		copied := item
		if item.CategoryID != nil {
			v := *item.CategoryID
			copied.CategoryID = &v
		}
		if item.Order != nil {
			v := *item.Order
			copied.Order = &v
		}
		if item.Content != nil {
			v := *item.Content
			copied.Content = &v
		}
		if item.Status != nil {
			v := *item.Status
			copied.Status = &v
		}
		if item.EmbeddedTime != nil {
			t := *item.EmbeddedTime
			copied.EmbeddedTime = &t
		}
		dst = append(dst, copied)
	}
	return dst
}

func cloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.UserWeekSchedule, 0, len(src))
	for _, week := range src {
		eventsCopy := make([]model.WeeklyEventBrief, len(week.Events))
		copy(eventsCopy, week.Events)
		dst = append(dst, model.UserWeekSchedule{
			Week:   week.Week,
			Events: eventsCopy,
		})
	}
	return dst
}

// buildOriginOrderMap 构建 suggested 任务的初始顺序基线（task_item_id -> rank）。
func buildOriginOrderMap(entries []model.HybridScheduleEntry) map[int]int {
	orderMap := make(map[int]int)
	if len(entries) == 0 {
		return orderMap
	}
	suggested := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if isMovableSuggestedTask(entry) {
			suggested = append(suggested, entry)
		}
	}
	sort.SliceStable(suggested, func(i, j int) bool {
		left := suggested[i]
		right := suggested[j]
		if left.Week != right.Week {
			return left.Week < right.Week
		}
		if left.DayOfWeek != right.DayOfWeek {
			return left.DayOfWeek < right.DayOfWeek
		}
		if left.SectionFrom != right.SectionFrom {
			return left.SectionFrom < right.SectionFrom
		}
		if left.SectionTo != right.SectionTo {
			return left.SectionTo < right.SectionTo
		}
		return left.TaskItemID < right.TaskItemID
	})
	for i, entry := range suggested {
		orderMap[entry.TaskItemID] = i + 1
	}
	return orderMap
}
