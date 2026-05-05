package agentshared

import llmservice "github.com/LoveLosita/smartflow/backend/services/llm"

func ResolveThinkingMode(enabled bool) llmservice.ThinkingMode {
	if enabled {
		return llmservice.ThinkingModeEnabled
	}
	return llmservice.ThinkingModeDisabled
}
