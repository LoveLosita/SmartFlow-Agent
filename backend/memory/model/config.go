package model

import "time"

// Config 是记忆模块配置对象（Day1 首版）。
//
// 职责边界：
// 1. 只承载模块运行参数，不承载业务状态；
// 2. 允许启动期统一注入，避免业务层直接依赖配置中心。
type Config struct {
	Enabled bool

	ExtractPrompt  string
	DecisionPrompt string

	Threshold      float64
	EnableReranker bool

	LLMTemperature float64
	LLMTopP        float64

	JobMaxRetry      int
	WorkerPollEvery  time.Duration
	WorkerClaimBatch int
}
