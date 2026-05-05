package model

import "time"

// ExtractJobPayload 是 memory_jobs.payload_json 的领域视图。
//
// 职责边界：
// 1. 只描述抽取任务执行所需字段；
// 2. 与数据库模型解耦，避免后续表结构调整污染 worker 逻辑。
type ExtractJobPayload struct {
	UserID          int       `json:"user_id"`
	ConversationID  string    `json:"conversation_id"`
	AssistantID     string    `json:"assistant_id,omitempty"`
	RunID           string    `json:"run_id,omitempty"`
	SourceMessageID int64     `json:"source_message_id,omitempty"`
	SourceRole      string    `json:"source_role"`
	SourceText      string    `json:"source_text"`
	OccurredAt      time.Time `json:"occurred_at"`
	TraceID         string    `json:"trace_id,omitempty"`
	IdempotencyKey  string    `json:"idempotency_key"`
}

// FactCandidate 表示抽取阶段得到的候选事实。
type FactCandidate struct {
	MemoryType       string
	Title            string
	Content          string
	Confidence       float64
	Importance       float64
	SensitivityLevel int
	IsExplicit       bool
}

// NormalizedFact 表示通过标准化后的可入库事实。
type NormalizedFact struct {
	MemoryType        string
	Title             string
	Content           string
	NormalizedContent string
	ContentHash       string
	Confidence        float64
	Importance        float64
	SensitivityLevel  int
	IsExplicit        bool
}
