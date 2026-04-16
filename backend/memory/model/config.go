package model

import "time"

// Config 是记忆模块配置对象（Day1 首版）。
//
// 职责边界：
// 1. 只承载模块运行参数，不承载业务状态；
// 2. 允许启动期统一注入，避免业务层直接依赖配置中心。
type Config struct {
	Enabled    bool
	RAGEnabled bool

	ExtractPrompt  string
	DecisionPrompt string

	Threshold      float64
	EnableReranker bool

	LLMTemperature float64
	LLMTopP        float64

	JobMaxRetry      int
	WorkerPollEvery  time.Duration
	WorkerClaimBatch int

	// 决策层配置。
	// 说明：
	// 1. DecisionEnabled 控制是否启用"召回→比对→汇总"决策流程；
	// 2. 默认关闭，旧路径完全保留，回滚无风险；
	// 3. DecisionFallbackMode 仅在决策流程整体报错时生效，不影响单条 LLM 比对失败（单条失败视为 unrelated）。
	DecisionEnabled           bool
	DecisionCandidateTopK     int     // Milvus 语义召回候选数上限
	DecisionCandidateMinScore float64 // Milvus 语义召回最低相似度
	DecisionFallbackMode      string  // "legacy_add"（退回旧路径直接新增）/ "drop"（丢弃）
	WriteMode                 string  // "legacy"（旧路径）/ "decision"（决策流程），仅 DecisionEnabled=true 时生效
}
