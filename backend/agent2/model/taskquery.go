package agentmodel

// TaskQueryState 是“任务查询”skill 的运行时状态骨架。
type TaskQueryState struct {
	UserInput       string
	RequestNowText  string
	NeedRetry       bool
	RetryCount      int
	MaxReflectRetry int
	FinalReply      string
}
