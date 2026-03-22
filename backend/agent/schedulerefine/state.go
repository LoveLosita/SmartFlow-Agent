package schedulerefine

import (
	"sort"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	// timezoneName 固定排程链路使用的业务时区，避免容器默认时区导致“明天/今晚”偏移。
	timezoneName = "Asia/Shanghai"
	// datetimeLayout 统一使用分钟级时间文本，方便模型理解与日志比对。
	datetimeLayout = "2006-01-02 15:04"
	// defaultPlanMax 是 Planner 最大调用次数（包含首次规划 + 重规划）。
	defaultPlanMax = 2
	// defaultExecuteMax 是执行阶段最大工具动作轮次。
	defaultExecuteMax = 16
	// defaultReplanMax 是执行阶段允许触发的重规划次数上限。
	defaultReplanMax = 2
	// defaultRepairReserve 表示为“终审修复”保留的最小动作预算。
	defaultRepairReserve = 1
)

// RefineContract 表示“微调意图契约”。
//
// 职责边界：
// 1. 负责承载“本轮微调到底要满足什么”的结构化目标；
// 2. 负责给后续 ReAct 动作与终审硬校验提供统一语义；
// 3. 不负责实际排程修改动作执行（动作由工具层负责）。
type RefineContract struct {
	Intent            string   `json:"intent"`
	Strategy          string   `json:"strategy"`
	HardRequirements  []string `json:"hard_requirements"`
	KeepRelativeOrder bool     `json:"keep_relative_order"`
	OrderScope        string   `json:"order_scope"`
	Reason            string   `json:"reason"`
}

// HardCheckReport 表示“终审硬校验报告”。
//
// 职责边界：
// 1. 记录规则层（物理冲突）是否通过；
// 2. 记录语义层（是否满足用户要求）是否通过；
// 3. 记录顺序层（是否保持相对顺序）是否通过；
// 4. 记录失败原因与修复尝试信息，便于后续持续优化 prompt；
// 5. 不负责直接决定是否落库（落库决策仍由服务层控制）。
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

// ReactRoundObservation 用于沉淀“每轮 ReAct 的可见观测信息”。
//
// 职责边界：
// 1. 负责记录每轮“计划 -> 动作 -> 观察 -> 反思”的关键信息；
// 2. 既用于 SSE 透传，也用于下一轮 prompt 的上下文回灌；
// 3. 不承担排程真实数据存储职责（真实排程仍在 HybridEntries）。
type ReactRoundObservation struct {
	Round         int            `json:"round"`
	GoalCheck     string         `json:"goal_check,omitempty"`
	Decision      string         `json:"decision,omitempty"`
	MissingInfo   []string       `json:"missing_info,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolParams    map[string]any `json:"tool_params,omitempty"`
	ToolSuccess   bool           `json:"tool_success"`
	ToolErrorCode string         `json:"tool_error_code,omitempty"`
	ToolResult    string         `json:"tool_result,omitempty"`
	Reflect       string         `json:"reflect,omitempty"`
}

// PlannerPlan 表示“本轮执行前的结构化计划”。
//
// 职责边界：
// 1. 负责记录模型当前建议的执行路径（先查什么、再做什么）；
// 2. 负责在失败重规划后替换为新版本，供执行器下一轮参考；
// 3. 不直接约束工具执行结果（执行合法性仍由工具层硬校验负责）。
type PlannerPlan struct {
	Summary        string   `json:"summary"`
	Steps          []string `json:"steps,omitempty"`
	SuccessSignals []string `json:"success_signals,omitempty"`
	Fallback       string   `json:"fallback,omitempty"`
}

// ScheduleRefineState 是“连续微调图”的统一状态容器。
//
// 职责边界：
// 1. 负责在图节点间传递“上一版排程快照 + 本轮用户微调请求 + 动作日志 + 终审报告”；
// 2. 负责承载最终对用户可见的 summary 与结构化 candidate_plans；
// 3. 不负责 Redis/MySQL 读写（持久化由 service 层负责）。
type ScheduleRefineState struct {
	// 1. 基础请求上下文。
	TraceID        string
	UserID         int
	ConversationID string
	UserMessage    string
	RequestNow     time.Time
	RequestNowText string

	// 2. 继承自上一版预览快照的可调度数据。
	TaskClassIDs   []int
	Constraints    []string
	HybridEntries  []model.HybridScheduleEntry
	AllocatedItems []model.TaskClassItem
	CandidatePlans []model.UserWeekSchedule

	// 3. 本轮微调过程状态。
	UserIntent string
	Contract   RefineContract

	PlanMax    int
	ExecuteMax int
	ReplanMax  int

	PlanUsed   int
	ReplanUsed int

	// MaxRounds 保留“总预算”语义，供终审修复节点继续复用：
	// MaxRounds = ExecuteMax + RepairReserve
	MaxRounds     int
	RepairReserve int
	RoundUsed     int
	ActionLogs    []string

	// ConsecutiveFailures 记录执行阶段连续失败次数，用于触发“失败兜底 thinking”。
	ConsecutiveFailures int
	// ThinkingBoostArmed 表示“当前失败串已触发过一次 thinking 兜底”。
	ThinkingBoostArmed bool

	LastToolResult     string
	ObservationHistory []ReactRoundObservation
	CurrentPlan        PlannerPlan
	LastPostStrategy   string
	// LastFailedCallSignature 记录“上一轮失败动作签名（tool+params）”。用于后端硬拦截重复失败动作。
	LastFailedCallSignature string
	OriginOrderMap          map[int]int

	// 4. 终审校验状态。
	HardCheck HardCheckReport

	// 5. 最终输出。
	FinalSummary string
	Completed    bool
}

// NewScheduleRefineState 基于“上一版排程预览快照”初始化连续微调状态。
//
// 步骤化说明：
// 1. 先初始化请求基础字段与默认预算，保证图内每个节点都能读取到稳定上下文。
// 2. 再把 preview 的核心排程数据做深拷贝注入，避免跨请求引用污染。
// 3. 最后构建 origin_order_map，作为“保持相对顺序”硬约束的判定基线。
// 4. 若 preview 为空，仍返回可用 state，由上层决定是报错还是降级。
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
		ExecuteMax:         defaultExecuteMax,
		ReplanMax:          defaultReplanMax,
		RepairReserve:      defaultRepairReserve,
		MaxRounds:          defaultExecuteMax + defaultRepairReserve,
		ActionLogs:         make([]string, 0, 24),
		ObservationHistory: make([]ReactRoundObservation, 0, 16),
		OriginOrderMap:     make(map[int]int),
		CurrentPlan: PlannerPlan{
			Summary: "初始化完成，等待 Planner 生成执行计划。",
		},
	}
	if preview == nil {
		return st
	}

	st.TaskClassIDs = append([]int(nil), preview.TaskClassIDs...)
	st.HybridEntries = cloneHybridEntries(preview.HybridEntries)
	st.AllocatedItems = cloneTaskClassItems(preview.AllocatedItems)
	st.CandidatePlans = cloneWeekSchedules(preview.CandidatePlans)
	st.OriginOrderMap = buildOriginOrderMap(st.HybridEntries)
	return st
}

// loadLocation 返回排程链路使用的业务时区。
func loadLocation() *time.Location {
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		return time.Local
	}
	return loc
}

// nowToMinute 返回当前时刻并截断到分钟级，降低 prompt 中秒级噪声。
func nowToMinute() time.Time {
	return time.Now().In(loadLocation()).Truncate(time.Minute)
}

// cloneHybridEntries 深拷贝混合日程切片。
func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	if len(src) == 0 {
		return nil
	}
	dst := make([]model.HybridScheduleEntry, len(src))
	copy(dst, src)
	return dst
}

// cloneTaskClassItems 深拷贝任务块切片（包含指针字段）。
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

// cloneWeekSchedules 深拷贝周视图切片。
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

// buildOriginOrderMap 从当前 suggested 排程位置构建“初始相对顺序映射”。
//
// 步骤化说明：
// 1. 先筛出所有可调的 suggested 任务；
// 2. 按 week/day/section/task_item_id 稳定排序，得到“时间先后基线”；
// 3. 把 task_item_id -> rank 写入 map，后续 Move/Swap 都基于该 rank 做顺序硬校验。
func buildOriginOrderMap(entries []model.HybridScheduleEntry) map[int]int {
	orderMap := make(map[int]int)
	if len(entries) == 0 {
		return orderMap
	}
	suggested := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Status == "suggested" && entry.TaskItemID > 0 {
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
	for idx, entry := range suggested {
		orderMap[entry.TaskItemID] = idx + 1
	}
	return orderMap
}
