package scheduleplan

import (
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	// schedulePlanTimezoneName 是排程链路默认业务时区。
	// 与随口记保持一致，固定东八区，避免容器运行在 UTC 导致"明天/今晚"偏移。
	schedulePlanTimezoneName = "Asia/Shanghai"

	// schedulePlanDatetimeLayout 是排程链路内部统一的分钟级时间格式。
	schedulePlanDatetimeLayout = "2006-01-02 15:04"

	// schedulePlanDefaultDailyRefineConcurrency 是日内并发优化默认并发度。
	// 这里给一个保守默认值，避免未配置时直接把模型并发打满导致限流。
	schedulePlanDefaultDailyRefineConcurrency = 3

	// schedulePlanDefaultWeeklyAdjustBudget 是周级配平默认调整额度。
	// 额度存在的目的：
	// 1. 防止周级 ReAct 过度调整导致震荡；
	// 2. 控制 token 与时延成本；
	// 3. 让方案改动更可解释。
	schedulePlanDefaultWeeklyAdjustBudget = 5

	// schedulePlanDefaultWeeklyTotalBudget 是周级“总尝试次数”默认预算。
	//
	// 设计意图：
	// 1. 总预算统计“动作尝试次数”（成功/失败都记一次）；
	// 2. 有效预算统计“成功动作次数”（仅成功时记一次）；
	// 3. 通过双预算把“探索次数”和“有效改动次数”分离，降低模型无效空转成本。
	schedulePlanDefaultWeeklyTotalBudget = 8

	// schedulePlanDefaultWeeklyRefineConcurrency 是周级“按周并发”默认并发度。
	// 说明：
	// 1. 周级输入规模通常比单天更大，默认并发度不宜过高，避免触发模型侧限流；
	// 2. 可在运行时按请求状态覆盖。
	schedulePlanDefaultWeeklyRefineConcurrency = 2

	// schedulePlanAdjustmentScopeSmall 表示“小改动微调”。
	// 语义：优先走快速路径，只做轻量周级调整。
	schedulePlanAdjustmentScopeSmall = "small"
	// schedulePlanAdjustmentScopeMedium 表示“中等改动微调”。
	// 语义：跳过日内拆分，直接进入周级配平。
	schedulePlanAdjustmentScopeMedium = "medium"
	// schedulePlanAdjustmentScopeLarge 表示“大改动重排”。
	// 语义：必要时重新走全量路径（日内并发 + 周级配平）。
	schedulePlanAdjustmentScopeLarge = "large"
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

	// UserIntent 是模型对用户排程意图的结构化摘要（如"帮我安排高数复习计划"）。
	UserIntent string
	// Constraints 是用户提出的硬约束列表（如 ["早八不排", "周末休息"]）。
	Constraints []string
	// TaskClassIDs 是本次请求携带的任务类集合（统一主语义）。
	//
	// 设计说明：
	// 1. 这里明确不再维护单值 task_class_id，避免“单值和切片同时存在”导致语义漂移；
	// 2. 分流依据统一为 len(TaskClassIDs)：
	//    2.1 len==1：跳过 daily 并发，直接进入 weekly refine；
	//    2.2 len>=2：进入 daily 并发后再 weekly refine；
	// 3. 输入清洗（去重、过滤非法值）由 plan 节点完成，这里只承载最终状态。
	TaskClassIDs []int
	// Strategy 是排程策略（steady/rapid），默认 steady。
	Strategy string
	// TaskTags 是“任务项 ID -> 认知类型标签”的映射。
	// 使用 ID 而不是名称，目的是规避“同名任务”带来的映射冲突。
	TaskTags map[int]string
	// TaskTagHintsByName 是“任务名称 -> 认知类型标签”的临时映射。
	// 该字段只作为 plan 输出兼容层：
	// 1. 若模型暂时给不出 task_item_id，只给名称；
	// 2. 后续在 hybridBuild/dailySplit 阶段再转换为 TaskTags(ID 维度)。
	TaskTagHintsByName map[string]string

	// ── preview 节点输出 ──

	// CandidatePlans 是粗排算法生成的候选方案（展示型结构，供后续节点做预览与总结）。
	CandidatePlans []model.UserWeekSchedule
	// AllocatedItems 是粗排算法已分配的任务项（EmbeddedTime 已回填），供 ReAct 精排使用。
	AllocatedItems []model.TaskClassItem
	// HasPlanningWindow 标记是否成功解析出“任务类时间窗”的相对周/天边界。
	//
	// 语义：
	// 1. true：PlanStart*/PlanEnd* 字段可用于 Move 工具的硬边界校验；
	// 2. false：表示当前运行未拿到窗口信息（例如依赖未注入），工具层将仅做基础校验。
	HasPlanningWindow bool
	// PlanStartWeek / PlanStartDay 表示全局排程窗口起点（相对周/天）。
	PlanStartWeek int
	PlanStartDay  int
	// PlanEndWeek / PlanEndDay 表示全局排程窗口终点（相对周/天）。
	PlanEndWeek int
	PlanEndDay  int

	// ── 日内并发优化阶段 ──

	// DailyGroups 是按 (week, day) 拆分后的单日优化输入。
	// 结构：week -> day -> DayGroup。
	DailyGroups map[int]map[int]*DayGroup
	// DailyResults 是单日优化输出。
	// 结构：week -> day -> []HybridScheduleEntry。
	DailyResults map[int]map[int][]model.HybridScheduleEntry
	// DailyRefineConcurrency 是日内并发优化的并发度。
	// 说明：该值由配置注入，可按环境调节。
	DailyRefineConcurrency int

	// ── 周级 ReAct 精排阶段 ──

	// HybridEntries 是混合日程条目列表，包含既有日程（existing）和粗排建议（suggested）。
	// 周级 ReAct 工具直接在此切片上操作（内存修改，不涉及 DB）。
	HybridEntries []model.HybridScheduleEntry
	// MergeSnapshot 是 merge 后快照。
	// 终审失败时回退到该快照，确保至少保留“日内优化成果”。
	MergeSnapshot []model.HybridScheduleEntry
	// ReactRound 当前周级 ReAct 循环轮次。
	ReactRound int
	// ReactMaxRound 周级 ReAct 最大循环轮次。
	ReactMaxRound int
	// ReactSummary 周级 ReAct 输出的优化摘要。
	ReactSummary string
	// ReactDone 标记周级 ReAct 是否已完成。
	ReactDone bool
	// WeeklyAdjustBudget 是周级跨天调整额度上限。
	// 语义：有效动作预算（仅工具调用成功时扣减）。
	WeeklyAdjustBudget int
	// WeeklyAdjustUsed 是周级跨天调整已使用额度。
	// 语义：有效动作已使用次数（仅成功调用时递增）。
	WeeklyAdjustUsed int
	// WeeklyTotalBudget 是周级总动作预算。
	// 语义：总尝试次数预算（成功/失败都扣减）。
	WeeklyTotalBudget int
	// WeeklyTotalUsed 是周级总动作已使用次数。
	// 语义：成功/失败每执行一次工具调用都递增。
	WeeklyTotalUsed int
	// WeeklyRefineConcurrency 是周级“按周并发”并发度。
	WeeklyRefineConcurrency int
	// WeeklyActionLogs 记录周级优化阶段的关键动作流水。
	//
	// 设计目的：
	// 1. 供 final_check 的总结模型理解“优化过程”，而非只看最终静态结果；
	// 2. 供调试排查时快速回放“每轮做了什么动作、是否成功、为何失败”。
	WeeklyActionLogs []string

	// ── 连续对话微调 ──

	// PreviousPlanJSON 是上一版已落库方案的 JSON 序列化，用于增量微调。
	// 从对话历史中提取，不做持久化。
	PreviousPlanJSON string
	// IsAdjustment 标记本次是否为微调请求（而非全新排程）。
	IsAdjustment bool
	// RestartRequested 标记本轮是否要求“放弃历史快照并重新排程”。
	//
	// 语义：
	// 1. true：强制清空 Previous* 并走全新构建；
	// 2. false：允许按同会话历史快照做增量微调。
	RestartRequested bool
	// AdjustmentScope 表示本轮改动力度分级（small/medium/large）。
	//
	// 分流语义：
	// 1. small：走快速微调节点，再进入周级优化；
	// 2. medium：跳过 daily，直接周级优化；
	// 3. large：优先走全量路径（多任务类时会经过 daily 并发）。
	AdjustmentScope string
	// AdjustmentReason 是模型给出的力度判定理由，用于日志排障与 review。
	AdjustmentReason string
	// AdjustmentConfidence 是模型给出的力度判定置信度（0-1）。
	AdjustmentConfidence float64
	// HasPreviousPreview 标记是否命中“同会话上一次排程预览快照”。
	//
	// 语义：
	// 1. true：可以尝试复用上次 HybridEntries 作为本轮优化起点；
	// 2. false：按全新排程路径构建粗排底板。
	HasPreviousPreview bool
	// PreviousTaskClassIDs 是上一次预览对应的任务类集合。
	//
	// 用途：
	// 1. 本轮未显式传 task_class_ids 时作为兜底；
	// 2. 仅会话内承接，不改动数据库。
	PreviousTaskClassIDs []int
	// PreviousHybridEntries 是上一次预览保存的混合日程条目。
	//
	// 用途：
	// 1. 连续对话微调时直接复用，避免重新粗排；
	// 2. 若为空则回退到粗排构建路径。
	PreviousHybridEntries []model.HybridScheduleEntry
	// PreviousAllocatedItems 是上一次预览保存的任务块分配结果。
	//
	// 用途：
	// 1. 保持 final_check 的数量核对口径稳定；
	// 2. return_preview 阶段可继续回填 embedded_time。
	PreviousAllocatedItems []model.TaskClassItem
	// PreviousCandidatePlans 是上一版预览保存的周视图结构化结果。
	//
	// 用途：
	// 1. 连续微调时可直接复用，避免重复转换；
	// 2. 兜底展示层（即使本轮未走全量粗排，仍可给前端稳定结构）。
	PreviousCandidatePlans []model.UserWeekSchedule

	// ── 最终输出 ──

	// FinalSummary 是 graph 最终给用户的回复文案。
	FinalSummary string
	// Completed 标记整个排程链路是否成功完成。
	Completed bool
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

// normalizeAdjustmentScope 归一化排程微调力度字段。
//
// 兜底策略：
// 1. 只接受 small/medium/large；
// 2. 任何未知值都回退为 large，保证不会误走“过轻”路径。
func normalizeAdjustmentScope(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case schedulePlanAdjustmentScopeSmall:
		return schedulePlanAdjustmentScopeSmall
	case schedulePlanAdjustmentScopeMedium:
		return schedulePlanAdjustmentScopeMedium
	default:
		return schedulePlanAdjustmentScopeLarge
	}
}
