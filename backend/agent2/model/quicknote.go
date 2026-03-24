package agentmodel

import (
	"time"

	agentshared "github.com/LoveLosita/smartflow/backend/agent2/shared"
)

const (
	// QuickNoteDatetimeMinuteLayout 是随口记链路统一使用的分钟级时间格式。
	QuickNoteDatetimeMinuteLayout = "2006-01-02 15:04"
	// QuickNoteTimezoneName 是随口记时间解析与展示优先使用的时区。
	QuickNoteTimezoneName = "Asia/Shanghai"

	QuickNotePriorityImportantUrgent     = TaskPriorityImportantUrgent
	QuickNotePriorityImportantNotUrgent  = TaskPriorityImportantNotUrgent
	QuickNotePrioritySimpleNotImportant  = TaskPrioritySimpleNotImportant
	QuickNotePriorityComplexNotImportant = TaskPriorityComplexNotImportant
)

// QuickNoteState 是随口记图在节点间流转的完整状态。
//
// 职责边界：
// 1. 负责保存意图识别、任务提取、工具重试和最终回复所需状态。
// 2. 不负责图编排，也不直接映射数据库任务实体。
type QuickNoteState struct {
	TraceID        string
	UserID         int
	ConversationID string
	RequestNow     time.Time
	RequestNowText string
	UserInput      string

	IsQuickNoteIntent bool
	IntentJudgeReason string

	ExtractedTitle            string
	ExtractedDeadline         *time.Time
	ExtractedDeadlineText     string
	ExtractedUrgencyThreshold *time.Time
	ExtractedPriority         int
	ExtractedBanter           string
	PlannedBySingleCall       bool
	ExtractedPriorityReason   string
	DeadlineValidationError   string

	ToolAttemptCount int
	MaxToolRetry     int
	LastToolError    string
	PersistedTaskID  int
	Persisted        bool
	AssistantReply   string
}

// NewQuickNoteState 负责创建随口记图的初始状态。
//
// 输入输出语义：
// 1. RequestNow 与 RequestNowText 会在创建时同步写入，保证整条链路共用同一时间基准。
// 2. MaxToolRetry 默认给 3，避免上层未配置时完全失去重试能力。
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

// CanRetryTool 返回当前是否还允许再次调用持久化工具。
//
// 输入输出语义：
// 1. true 表示“尚未达到最大重试次数”，调用方仍可继续重试。
// 2. false 表示必须收口，避免无限重试。
func (s *QuickNoteState) CanRetryTool() bool {
	return s.ToolAttemptCount < s.MaxToolRetry
}

// RecordToolError 记录一次工具失败，并推进重试计数。
//
// 职责边界：
// 1. 只更新与工具失败相关的状态。
// 2. 不决定是否继续重试，是否重试由节点分支逻辑判断。
func (s *QuickNoteState) RecordToolError(errMsg string) {
	s.ToolAttemptCount++
	s.LastToolError = errMsg
}

// RecordToolSuccess 记录一次工具成功结果。
//
// 输入输出语义：
// 1. taskID 必须是持久化后的真实任务 ID。
// 2. 成功后会清空 LastToolError，表示当前链路已进入稳定态。
func (s *QuickNoteState) RecordToolSuccess(taskID int) {
	s.ToolAttemptCount++
	s.PersistedTaskID = taskID
	s.Persisted = true
	s.LastToolError = ""
}
