package model

import (
	"strings"
	"time"
)

const (
	// MemoryReadModeLegacy 表示读取侧沿用“RAG 优先，失败再走 legacy”旧链路。
	MemoryReadModeLegacy = "legacy"
	// MemoryReadModeHybrid 表示读取侧走“结构化强约束 + 语义候选”混合链路。
	MemoryReadModeHybrid = "hybrid"

	// MemoryInjectRenderModeFlat 表示沿用扁平列表渲染。
	MemoryInjectRenderModeFlat = "flat"
	// MemoryInjectRenderModeTypedV2 表示按记忆类型分段渲染。
	MemoryInjectRenderModeTypedV2 = "typed_v2"

	// DefaultReadConstraintLimit 是 constraint 默认预算上限。
	DefaultReadConstraintLimit = 5
	// DefaultReadPreferenceLimit 是 preference 默认预算上限。
	DefaultReadPreferenceLimit = 5
	// DefaultReadFactLimit 是 fact 默认预算上限。
	DefaultReadFactLimit = 5
)

// Config 是记忆模块配置对象（Day1 首版）。
//
// 职责边界：
// 1. 只承载模块运行参数，不承载业务状态；
// 2. 允许启动期统一注入，避免业务层直接依赖配置中心。
type Config struct {
	Enabled    bool
	RAGEnabled bool

	ReadMode            string
	ReadConstraintLimit int
	ReadPreferenceLimit int
	ReadFactLimit       int
	InjectRenderMode    string

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

	// 写入置信度阈值。
	// 说明：
	// 1. 抽取结果 confidence 低于此值直接丢弃，不做入库；
	// 2. 默认 0.5，与"守门员"prompt 的 confidence>=0.5 输出规则配合；
	// 3. fallback 路径 confidence 设为 0.45，低于默认阈值，LLM 不可用时不写入。
	WriteMinConfidence float64

	// 记忆模块 LLM 调用是否开启 thinking，由 config.yaml 的 agent.thinking.memory 注入。
	LLMThinking bool
}

// NormalizeReadMode 统一读取模式字符串。
func NormalizeReadMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case MemoryReadModeHybrid:
		return MemoryReadModeHybrid
	default:
		return MemoryReadModeLegacy
	}
}

// NormalizeInjectRenderMode 统一注入渲染模式字符串。
func NormalizeInjectRenderMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case MemoryInjectRenderModeTypedV2:
		return MemoryInjectRenderModeTypedV2
	default:
		return MemoryInjectRenderModeFlat
	}
}

// EffectiveReadConstraintLimit 返回 constraint 生效预算。
func (c Config) EffectiveReadConstraintLimit() int {
	return normalizePositiveLimit(c.ReadConstraintLimit, DefaultReadConstraintLimit)
}

// EffectiveReadPreferenceLimit 返回 preference 生效预算。
func (c Config) EffectiveReadPreferenceLimit() int {
	return normalizePositiveLimit(c.ReadPreferenceLimit, DefaultReadPreferenceLimit)
}

// EffectiveReadFactLimit 返回 fact 生效预算。
func (c Config) EffectiveReadFactLimit() int {
	return normalizePositiveLimit(c.ReadFactLimit, DefaultReadFactLimit)
}

// EffectiveReadMode 返回生效读取模式。
func (c Config) EffectiveReadMode() string {
	return NormalizeReadMode(c.ReadMode)
}

// EffectiveInjectRenderMode 返回生效渲染模式。
func (c Config) EffectiveInjectRenderMode() string {
	return NormalizeInjectRenderMode(c.InjectRenderMode)
}

// TotalReadBudget 返回三类记忆的总预算上限。
func (c Config) TotalReadBudget() int {
	return c.EffectiveReadConstraintLimit() +
		c.EffectiveReadPreferenceLimit() +
		c.EffectiveReadFactLimit()
}

func normalizePositiveLimit(value int, defaultValue int) int {
	if value <= 0 {
		return defaultValue
	}
	return value
}
