package service

import (
	"time"

	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	"github.com/spf13/viper"
)

// LoadConfigFromViper 读取记忆模块配置并做默认值兜底。
//
// 默认策略：
// 1. temperature/top_p 使用低随机参数，提升可复现性；
// 2. Day1 先提供参数位，不强制所有参数立即生效；
// 3. 轮询与重试参数给出保守默认值，避免对主链路造成压力。
func LoadConfigFromViper() memorymodel.Config {
	cfg := memorymodel.Config{
		Enabled:          viper.GetBool("memory.enabled"),
		RAGEnabled:       viper.GetBool("memory.rag.enabled"),
		ExtractPrompt:    viper.GetString("memory.prompt.extract"),
		DecisionPrompt:   viper.GetString("memory.prompt.decision"),
		Threshold:        viper.GetFloat64("memory.threshold"),
		EnableReranker:   viper.GetBool("memory.enableReranker"),
		LLMTemperature:   viper.GetFloat64("memory.llm.temperature"),
		LLMTopP:          viper.GetFloat64("memory.llm.topP"),
		JobMaxRetry:      viper.GetInt("memory.job.maxRetry"),
		WorkerPollEvery:  viper.GetDuration("memory.worker.pollEvery"),
		WorkerClaimBatch: viper.GetInt("memory.worker.claimBatch"),
	}

	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.55
	}
	if cfg.LLMTemperature <= 0 {
		cfg.LLMTemperature = 0.1
	}
	if cfg.LLMTopP <= 0 {
		cfg.LLMTopP = 0.2
	}
	if cfg.JobMaxRetry <= 0 {
		cfg.JobMaxRetry = 6
	}
	if cfg.WorkerPollEvery <= 0 {
		cfg.WorkerPollEvery = 2 * time.Second
	}
	if cfg.WorkerClaimBatch <= 0 {
		cfg.WorkerClaimBatch = 1
	}
	return cfg
}
