package agent

import "time"

const (
	// QuickNoteDatetimeMinuteLayout 是“随口记”链路内部统一的分钟级时间格式。
	// 说明：
	// 1) 用于把“当前时间基准”传给模型，避免模型在相对时间推断时出现秒级抖动。
	// 2) 用于日志和调试，读起来比 RFC3339 更直观。
	QuickNoteDatetimeMinuteLayout = "2006-01-02 15:04"

	// quickNoteTimezoneName 是随口记链路默认业务时区。
	// 这里固定为东八区，避免容器运行在 UTC 时把“明天/今晚”解释偏移到错误日期。
	quickNoteTimezoneName = "Asia/Shanghai"

	// QuickNotePriorityImportantUrgent 对应四象限里的“重要且紧急”。
	// 在当前 tasks 表中映射为 priority=1（数值越小优先级越高）。
	QuickNotePriorityImportantUrgent = 1
	// QuickNotePriorityImportantNotUrgent 对应“重要不紧急”。
	QuickNotePriorityImportantNotUrgent = 2
	// QuickNotePrioritySimpleNotImportant 对应“简单不重要”。
	QuickNotePrioritySimpleNotImportant = 3
	// QuickNotePriorityComplexNotImportant 对应“不简单不重要”。
	QuickNotePriorityComplexNotImportant = 4
)

// IsValidTaskPriority 判断优先级是否合法。
// 目前后端任务模型限定为 1~4。
func IsValidTaskPriority(priority int) bool {
	return priority >= QuickNotePriorityImportantUrgent && priority <= QuickNotePriorityComplexNotImportant
}

// PriorityLabelCN 把优先级数值转换为中文标签，便于拼接给用户的自然语言回复。
func PriorityLabelCN(priority int) string {
	switch priority {
	case QuickNotePriorityImportantUrgent:
		return "重要且紧急"
	case QuickNotePriorityImportantNotUrgent:
		return "重要不紧急"
	case QuickNotePrioritySimpleNotImportant:
		return "简单不重要"
	case QuickNotePriorityComplexNotImportant:
		return "不简单不重要"
	default:
		return "未知优先级"
	}
}

// QuickNoteState 是“AI随口记”链路在 graph 节点间传递的统一状态容器。
// 设计目标：
// 1) 把本次请求的上下文收拢到一个结构里，降低节点函数参数散落；
// 2) 让“识别、评估、写库、重试、回复”每一步都可追踪；
// 3) 便于后续扩展打点和可观测字段（例如时间解析失败原因）。
type QuickNoteState struct {
	// 基础上下文：用于日志关联与用户隔离。
	TraceID        string
	UserID         int
	ConversationID string

	// RequestNow 记录“请求进入随口记链路时”的时间基准（分钟级）。
	// 所有相对时间（明天/后天/下周一）都必须基于这个时间计算，
	// 这样同一次请求内不会因为时间流逝产生口径漂移。
	RequestNow time.Time
	// RequestNowText 是 RequestNow 的字符串形式，主要用于 prompt 注入。
	RequestNowText string

	// 用户原始输入（例如：提醒我下周日之前完成大作业）。
	UserInput string

	// 意图判定结果。
	IsQuickNoteIntent bool
	IntentJudgeReason string

	// 结构化抽取结果：由“意图识别/信息抽取”节点写入。
	ExtractedTitle        string
	ExtractedDeadline     *time.Time
	ExtractedDeadlineText string
	ExtractedPriority     int
	// ExtractedBanter 是聚合规划阶段生成的“轻松跟进句”。
	// 该字段非空时，最终回复阶段可直接复用，避免再触发一次独立润色模型调用。
	ExtractedBanter string
	// PlannedBySingleCall 标记本次是否走了“单请求聚合规划”快路径。
	// 用于在后续节点做更激进的性能策略（例如缺失字段时直接本地兜底，避免再触发模型调用）。
	PlannedBySingleCall bool

	// ExtractedPriorityReason 记录优先级评估理由，便于后续排查模型判断是否符合预期。
	ExtractedPriorityReason string
	// DeadlineValidationError 记录时间校验失败原因。
	// 只要该字段非空，就说明用户提供了无法解析的时间表达，本次请求不应落库。
	DeadlineValidationError string

	// 工具调用过程状态：用于重试与故障回溯。
	ToolAttemptCount int
	MaxToolRetry     int
	LastToolError    string

	// 最终持久化结果：由“写库工具”节点回填。
	PersistedTaskID int
	Persisted       bool

	// AssistantReply 是 graph 最终给用户的回复文案。
	AssistantReply string
}

// NewQuickNoteState 创建随口记状态对象并初始化默认重试次数。
func NewQuickNoteState(traceID string, userID int, conversationID, userInput string) *QuickNoteState {
	requestNow := quickNoteNowToMinute()
	return &QuickNoteState{
		TraceID:        traceID,
		UserID:         userID,
		ConversationID: conversationID,
		RequestNow:     requestNow,
		RequestNowText: formatQuickNoteTimeToMinute(requestNow),
		UserInput:      userInput,
		MaxToolRetry:   3,
	}
}

// CanRetryTool 判断当前是否还能继续重试工具调用。
func (s *QuickNoteState) CanRetryTool() bool {
	return s.ToolAttemptCount < s.MaxToolRetry
}

// RecordToolError 记录一次工具调用失败。
func (s *QuickNoteState) RecordToolError(errMsg string) {
	s.ToolAttemptCount++
	s.LastToolError = errMsg
}

// RecordToolSuccess 记录一次工具调用成功。
func (s *QuickNoteState) RecordToolSuccess(taskID int) {
	s.ToolAttemptCount++
	s.PersistedTaskID = taskID
	s.Persisted = true
	s.LastToolError = ""
}

// quickNoteLocation 返回随口记链路使用的业务时区。
func quickNoteLocation() *time.Location {
	loc, err := time.LoadLocation(quickNoteTimezoneName)
	if err != nil {
		return time.Local
	}
	return loc
}

// quickNoteNowToMinute 返回当前时间并截断到分钟级。
func quickNoteNowToMinute() time.Time {
	return time.Now().In(quickNoteLocation()).Truncate(time.Minute)
}

// formatQuickNoteTimeToMinute 将时间格式化为分钟级字符串。
func formatQuickNoteTimeToMinute(t time.Time) string {
	return t.In(quickNoteLocation()).Format(QuickNoteDatetimeMinuteLayout)
}
