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
		Enabled:             viper.GetBool("memory.enabled"),
		RAGEnabled:          viper.GetBool("memory.rag.enabled"),
		ReadMode:            memorymodel.NormalizeReadMode(viper.GetString("memory.read.mode")),
		InjectRenderMode:    memorymodel.NormalizeInjectRenderMode(viper.GetString("memory.inject.renderMode")),
		ExtractPrompt:       viper.GetString("memory.prompt.extract"),
		DecisionPrompt:      viper.GetString("memory.prompt.decision"),
		Threshold:           viper.GetFloat64("memory.threshold"),
		EnableReranker:      viper.GetBool("memory.enableReranker"),
		LLMTemperature:      viper.GetFloat64("memory.llm.temperature"),
		LLMTopP:             viper.GetFloat64("memory.llm.topP"),
		JobMaxRetry:         viper.GetInt("memory.job.maxRetry"),
		WorkerPollEvery:     viper.GetDuration("memory.worker.pollEvery"),
		WorkerClaimBatch:    viper.GetInt("memory.worker.claimBatch"),
		ReadConstraintLimit: viper.GetInt("memory.read.constraintLimit"),
		ReadPreferenceLimit: viper.GetInt("memory.read.preferenceLimit"),
		ReadFactLimit:       viper.GetInt("memory.read.factLimit"),
		ReadTodoHintLimit:   viper.GetInt("memory.read.todoHintLimit"),

		// 决策层配置：默认关闭，灰度开启后才会生效。
		DecisionEnabled:           viper.GetBool("memory.decision.enabled"),
		DecisionCandidateTopK:     viper.GetInt("memory.decision.candidateTopK"),
		DecisionCandidateMinScore: viper.GetFloat64("memory.decision.candidateMinScore"),
		DecisionFallbackMode:      viper.GetString("memory.decision.fallbackMode"),
		WriteMode:                 viper.GetString("memory.write.mode"),
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
	cfg.ReadConstraintLimit = cfg.EffectiveReadConstraintLimit()
	cfg.ReadPreferenceLimit = cfg.EffectiveReadPreferenceLimit()
	cfg.ReadFactLimit = cfg.EffectiveReadFactLimit()
	cfg.ReadTodoHintLimit = cfg.EffectiveReadTodoHintLimit()
	cfg.ReadMode = cfg.EffectiveReadMode()
	cfg.InjectRenderMode = cfg.EffectiveInjectRenderMode()

	// 决策层配置默认值兜底。
	// 说明：
	// 1. TopK 和 MinScore 是 Milvus 召回参数，需要保守默认值避免召回过多噪声候选；
	// 2. FallbackMode 默认退回旧路径新增，保证决策流程异常时不丢数据；
	// 3. WriteMode 由 DecisionEnabled 隐式决定，这里不做强制联动。
	if cfg.DecisionCandidateTopK <= 0 {
		cfg.DecisionCandidateTopK = 5
	}
	if cfg.DecisionCandidateMinScore <= 0 {
		cfg.DecisionCandidateMinScore = 0.6
	}
	if cfg.DecisionFallbackMode == "" {
		cfg.DecisionFallbackMode = "legacy_add"
	}
	if cfg.WriteMode == "" {
		cfg.WriteMode = "legacy"
	}

	return cfg
}
