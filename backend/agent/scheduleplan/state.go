package scheduleplan

import (
	"time"

	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	// schedulePlanTimezoneName 是排程链路默认业务时区。
	// 与随口记保持一致，固定东八区，避免容器运行在 UTC 导致"明天/今晚"偏移。
	schedulePlanTimezoneName = "Asia/Shanghai"

	// schedulePlanDatetimeLayout 是排程链路内部统一的分钟级时间格式。
	schedulePlanDatetimeLayout = "2006-01-02 15:04"
)

// SchedulePlanState 是"智能排程"链路在 graph 节点间传递的统一状态容器。
//
// 设计目标：
// 1) 收拢排程请求全生命周期的上下文，降低节点间参数散���；
// 2) 支持"粗排 -> 校验 -> 修补重试 -> 落库"的完整链路追踪；
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
	// TaskClassID 是目标任务类 ID，由 Extra 字段或模型抽取获得。
	TaskClassID int
	// Strategy 是排程策略（steady/rapid），默认 steady。
	Strategy string

	// ── preview 节点输出 ──

	// CandidatePlans 是粗排算法生成的候选方案（展示型结构，供 SSE 推送给前端预览）。
	CandidatePlans []model.UserWeekSchedule
	// AllocatedItems 是粗排算法已分配的任务项（EmbeddedTime 已回填），供 ReAct 精排使用。
	AllocatedItems []model.TaskClassItem

	// ── ReAct 精排阶段 ──

	// HybridEntries 是混合日程条目列表，包含既有日程（existing）和粗排建议（suggested）。
	// ReAct 工具直接在此切片上操作（内存修改，不涉及 DB）。
	HybridEntries []model.HybridScheduleEntry
	// ReactRound 当前 ReAct 循环轮次。
	ReactRound int
	// ReactMaxRound 最大循环轮次（建议 3）。
	ReactMaxRound int
	// ReactSummary LLM 输出的优化摘要。
	ReactSummary string
	// ReactDone 标记 ReAct 是否已完成。
	ReactDone bool

	// ── 连续对话微调 ──

	// PreviousPlanJSON 是上一版已落库方案的 JSON 序列化，用于增量微调。
	// 从对话历史中提取，不做持久化。
	PreviousPlanJSON string
	// IsAdjustment 标记本次是否为微调请求（而非全新排程）。
	IsAdjustment bool

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
		TraceID:        traceID,
		UserID:         userID,
		ConversationID: conversationID,
		RequestNow:     now,
		RequestNowText: now.In(schedulePlanLocation()).Format(schedulePlanDatetimeLayout),
		Strategy:       "steady",
		ReactMaxRound:  3,
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
