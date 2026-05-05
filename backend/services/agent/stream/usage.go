package agentstream

import "github.com/cloudwego/eino/schema"

// CloneUsage 深拷贝 TokenUsage。
func CloneUsage(usage *schema.TokenUsage) *schema.TokenUsage {
	if usage == nil {
		return nil
	}
	copied := *usage
	return &copied
}

// MergeUsage 合并两段 usage，取更大值。
// 适用于同一次调用不同流分片的 usage 收敛。
func MergeUsage(base *schema.TokenUsage, incoming *schema.TokenUsage) *schema.TokenUsage {
	if incoming == nil {
		return CloneUsage(base)
	}
	if base == nil {
		return CloneUsage(incoming)
	}

	merged := *base
	if incoming.PromptTokens > merged.PromptTokens {
		merged.PromptTokens = incoming.PromptTokens
	}
	if incoming.CompletionTokens > merged.CompletionTokens {
		merged.CompletionTokens = incoming.CompletionTokens
	}
	if incoming.TotalTokens > merged.TotalTokens {
		merged.TotalTokens = incoming.TotalTokens
	}
	if incoming.PromptTokenDetails.CachedTokens > merged.PromptTokenDetails.CachedTokens {
		merged.PromptTokenDetails.CachedTokens = incoming.PromptTokenDetails.CachedTokens
	}
	if incoming.CompletionTokensDetails.ReasoningTokens > merged.CompletionTokensDetails.ReasoningTokens {
		merged.CompletionTokensDetails.ReasoningTokens = incoming.CompletionTokensDetails.ReasoningTokens
	}
	return &merged
}
