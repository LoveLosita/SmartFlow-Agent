package agentllm

// TaskQueryPlanOutput 是“随口问任务”聚合规划的模型契约草案。
type TaskQueryPlanOutput struct {
	Intent         string   `json:"intent"`
	Quadrants      []int    `json:"quadrants"`
	SortBy         string   `json:"sort_by"`
	Limit          int      `json:"limit"`
	TimeRange      string   `json:"time_range"`
	NeedBroadening bool     `json:"need_broadening"`
	Keywords       []string `json:"keywords"`
}

// TaskQueryReflectOutput 是查询结果反思节点的模型契约草案。
type TaskQueryReflectOutput struct {
	Satisfied       bool   `json:"satisfied"`
	NeedRetry       bool   `json:"need_retry"`
	RetrySuggestion string `json:"retry_suggestion"`
}
