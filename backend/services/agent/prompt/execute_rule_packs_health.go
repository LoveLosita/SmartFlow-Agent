package agentprompt

import "strings"

func buildExecuteScheduleAnalyzePackV2() executeRulePack {
	return executeRulePack{
		Name: executeRulePackScheduleAnalyze,
		Content: strings.TrimSpace(`
- analyze 包已激活：优先使用 analyze_health 判断“现在还值不值得继续主动优化”，不要把它当成全能体检表。
- 若需要维度级细诊断（如 rhythm），再 add packs=["deep_analyze"]，不要默认把所有分析都铺开。
- 在主动优化专用模式里，analyze_health 会直接返回 decision.candidates：这些就是后端已经验证合法、并且复诊后确实变好的 move/swap 候选。
- 一旦 decision.candidates 已经给出，下一步应直接从候选里选一个去执行；不要再自己搜索 query_target_tasks / query_available_slots。
- 若 analyze_health 显示 should_continue_optimize=false，优先收口；不要因为“理论上还还能动”就继续局部修补。`),
	}
}

func buildExecuteDiagLoopMicroPackV2() executeRulePack {
	return executeRulePack{
		Name: executeRulePackMicroDiagLoop,
		Content: strings.TrimSpace(`
- 粗排后的主动优化允许多轮 execute，但每一轮都必须围绕“当前主问题”做局部、小范围、可解释的调整。
- 在主动优化专用模式里，analyze_health 负责“出候选题”，你只负责在 decision.candidates 里做选择，不负责重新全窗搜点。
- 若当前问题主要来自时间窗过紧，或所有合法候选都只是平移没有变轻，应接受局部不完美并收口。
- 若连续两轮诊断没有明显改善，或当前 recommended_operation 已经是 close，应优先收口。
- 主动优化优先在已有落位之间做选择：swap 优先，move 次之；不要做全窗口搜索。`),
	}
}
