package agentmodel

import (
	"time"

	agentshared "github.com/LoveLosita/smartflow/backend/agent2/shared"
)

const (
	// QuickNoteDatetimeMinuteLayout 是“随口记”链路内部统一的分钟级时间格式。
	// 说明：
	// 1) 用于把“当前时间基准”传给模型，避免模型在相对时间推断时出现秒级抖动。
	// 2) 用于日志和调试，读起来比 RFC3339 更直观。
	QuickNoteDatetimeMinuteLayout = "2006-01-02 15:04"

	// QuickNoteTimezoneName 是随口记链路默认业务时区。
	// 这里固定为东八区，避免容器运行在 UTC 时把“明天/今晚”解释偏移到错误日期。
	QuickNoteTimezoneName = "Asia/Shanghai"

	// QuickNotePriorityImportantUrgent 对应四象限里的“重要且紧急”。
	QuickNotePriorityImportantUrgent = 1
	// QuickNotePriorityImportantNotUrgent 对应“重要不紧急”。
	QuickNotePriorityImportantNotUrgent = 2
	// QuickNotePrioritySimpleNotImportant 对应“简单不重要”。
	QuickNotePrioritySimpleNotImportant = 3
	// QuickNotePriorityComplexNotImportant 对应“不简单不重要”。
	QuickNotePriorityComplexNotImportant = 4
)

// IsValidTaskPriority 判断优先级是否合法。
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
type QuickNoteState struct {
	TraceID        string
	UserID         int
	ConversationID string

	// RequestNow 记录“请求进入随口记链路时”的时间基准（分钟级）。
	RequestNow time.Time
	// RequestNowText 是 RequestNow 的字符串形式，主要用于 prompt 注入。
	RequestNowText string

	UserInput string

	IsQuickNoteIntent bool
	IntentJudgeReason string

	ExtractedTitle        string
	ExtractedDeadline     *time.Time
	ExtractedDeadlineText string
	// ExtractedUrgencyThreshold 表示“进入紧急象限的分界时间”。
	ExtractedUrgencyThreshold *time.Time
	ExtractedPriority         int
	// ExtractedBanter 是聚合规划阶段生成的“轻松跟进句”。
	ExtractedBanter string
	// PlannedBySingleCall 标记本次是否走了“单请求聚合规划”快路径。
	PlannedBySingleCall bool

	ExtractedPriorityReason string
	// DeadlineValidationError 记录时间校验失败原因。
	DeadlineValidationError string

	ToolAttemptCount int
	MaxToolRetry     int
	LastToolError    string

	PersistedTaskID int
	Persisted       bool

	AssistantReply string
}

// NewQuickNoteState 创建随口记状态对象并初始化默认重试次数。
func NewQuickNoteState(traceID string, userID int, conversationID, userInput string) *QuickNoteState {
	requestNow := agentshared.NowToMinute()
	return &QuickNoteState{
		TraceID:        traceID,
		UserID:         userID,
		ConversationID: conversationID,
		RequestNow:     requestNow,
		RequestNowText: agentshared.FormatMinute(requestNow),
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
