package agentmodel

// SchedulePlanState 是“首次排程”skill 的运行时状态骨架。
type SchedulePlanState struct {
	TraceID            string
	UserID             int
	ConversationID     string
	UserInput          string
	TaskClassIDs       []int
	UseQuickRefineOnly bool
	Completed          bool
	FinalSummary       string
}

// ScheduleRefineState 是“连续微调排程”skill 的运行时状态骨架。
type ScheduleRefineState struct {
	TraceID        string
	UserID         int
	ConversationID string
	UserInput      string
	Completed      bool
	FinalSummary   string
}
