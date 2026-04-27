package newagentshared

import infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"

func ResolveThinkingMode(enabled bool) infrallm.ThinkingMode {
	if enabled {
		return infrallm.ThinkingModeEnabled
	}
	return infrallm.ThinkingModeDisabled
}
