package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
)

const defaultDecisionCompareMaxTokens = 600

// LLMDecisionOrchestrator 负责对"一条新 fact vs 一条旧记忆"做关系判断。
//
// 职责边界：
// 1. 每次只比较一对，是最小粒度的 LLM 调用；
// 2. LLM 只输出 relation（关系类型），不输出 action，不输出 target ID；
// 3. LLM 调用失败时返回 error，由上层决定是否视为 unrelated。
type LLMDecisionOrchestrator struct {
	client *llmservice.Client
	cfg    memorymodel.Config
	logger *log.Logger
}

// NewLLMDecisionOrchestrator 构造决策比对编排器。
func NewLLMDecisionOrchestrator(client *llmservice.Client, cfg memorymodel.Config) *LLMDecisionOrchestrator {
	return &LLMDecisionOrchestrator{
		client: client,
		cfg:    cfg,
		logger: log.Default(),
	}
}

// Compare 对单条新 fact 与单条旧候选做关系判断。
//
// 返回语义：
// 1. 成功时返回比对结果，relation 为四种合法值之一；
// 2. LLM 不可用或输出异常时返回 error，上层应视为 unrelated；
// 3. 不做最终决策，最终动作由确定性汇总逻辑产出。
func (o *LLMDecisionOrchestrator) Compare(
	ctx context.Context,
	billing llmservice.BillingContext,
	fact memorymodel.NormalizedFact,
	candidate memorymodel.CandidateSnapshot,
) (*memorymodel.ComparisonResult, error) {
	if o == nil || o.client == nil {
		return nil, fmt.Errorf("决策编排器未初始化")
	}

	// 1. 构建逐对比较 prompt：极简二元判断，LLM 只输出 relation。
	systemPrompt := buildDecisionCompareSystemPrompt()
	userPrompt := buildDecisionCompareUserPrompt(fact, candidate)

	messages := llmservice.BuildSystemUserMessages(systemPrompt, nil, userPrompt)
	invokeCtx := llmservice.WithBillingContext(ctx, billing)

	// 2. 调用 LLM 做结构化输出，温度用低值保证判断稳定。
	resp, _, err := llmservice.GenerateJSON[decisionCompareResponse](
		invokeCtx,
		o.client,
		messages,
		llmservice.GenerateOptions{
			Temperature: 0.1,
			MaxTokens:   defaultDecisionCompareMaxTokens,
			Thinking:    resolveMemoryThinkingMode(o.cfg.LLMThinking),
		},
	)
	if err != nil {
		if o.logger != nil {
			o.logger.Printf("[WARN][去重] 决策比对 LLM 调用失败: memory_type=%s candidate_id=%d err=%v", fact.MemoryType, candidate.MemoryID, err)
		}
		return nil, err
	}

	// 3. 映射 LLM 输出到 ComparisonResult，MemoryID 由代码填充而非 LLM。
	result := &memorymodel.ComparisonResult{
		MemoryID:       candidate.MemoryID,
		Relation:       normalizeRelation(resp.Relation),
		UpdatedContent: strings.TrimSpace(resp.UpdatedContent),
		UpdatedTitle:   strings.TrimSpace(resp.UpdatedTitle),
		Reason:         strings.TrimSpace(resp.Reason),
	}

	return result, nil
}

// decisionCompareResponse 是 LLM 逐对比较的 JSON 输出结构。
type decisionCompareResponse struct {
	Relation       string `json:"relation"`
	UpdatedContent string `json:"updated_content"`
	UpdatedTitle   string `json:"updated_title"`
	Reason         string `json:"reason"`
}

// normalizeRelation 统一 relation 字段为小写标准形式。
func normalizeRelation(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// buildDecisionCompareSystemPrompt 构建逐对比较的系统 prompt。
func buildDecisionCompareSystemPrompt() string {
	return strings.TrimSpace(`你是一个记忆关系判断器。请判断"新事实"和"旧记忆"之间的关系。

关系类型：
- duplicate：两者表达相同意思，新事实没有新信息
- update：新事实是对旧记忆的修正、补充或更精确表述
- conflict：新事实与旧记忆在同一话题上存在矛盾（如"喜欢X"变为"不喜欢X"、"去了A地"变为"实际去了B地"），旧记忆已过时
- unrelated：两者说的是不同的事情，或属于同一大类下的不同偏好（如"喜欢唱歌"与"喜欢打球"是不同爱好，不矛盾）

输出 JSON：
{"relation":"...","updated_content":"...","updated_title":"...","reason":"..."}

规则：
1. relation=update 时，updated_content 必须写出合并后的完整内容（不是只写差异部分）
2. 其余 relation 类型，updated_content 留空即可
3. reason 写简短判断依据
4. 只输出 JSON，不要输出解释或 markdown
5. conflict 仅限同一话题内的矛盾信息；不同话题的偏好、不同领域的兴趣一律判 unrelated`)
}

// buildDecisionCompareUserPrompt 构建逐对比较的用户 prompt。
func buildDecisionCompareUserPrompt(fact memorymodel.NormalizedFact, candidate memorymodel.CandidateSnapshot) string {
	return fmt.Sprintf("新事实：【%s】%s\n旧记忆：【%s】%s",
		fact.MemoryType, fact.Content,
		candidate.MemoryType, candidate.Content,
	)
}

// resolveMemoryThinkingMode 根据配置布尔值返回对应的 ThinkingMode。
func resolveMemoryThinkingMode(enabled bool) llmservice.ThinkingMode {
	if enabled {
		return llmservice.ThinkingModeEnabled
	}
	return llmservice.ThinkingModeDisabled
}
