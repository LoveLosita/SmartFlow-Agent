package agentllm

// ScheduleIntentOutput 是智能排程一级意图识别的模型契约草案。
type ScheduleIntentOutput struct {
	Intent          string `json:"intent"`
	NeedRefine      bool   `json:"need_refine"`
	AdjustmentScope string `json:"adjustment_scope"`
}

// SchedulePlanOutput 是首次排程规划节点的模型契约草案。
type SchedulePlanOutput struct {
	Goal        string   `json:"goal"`
	Constraints []string `json:"constraints"`
	Strategy    string   `json:"strategy"`
}

// ScheduleRefineOutput 是连续微调阶段的模型契约草案。
type ScheduleRefineOutput struct {
	Decision       string   `json:"decision"`
	HardAssertions []string `json:"hard_assertions"`
	NextAction     string   `json:"next_action"`
}
