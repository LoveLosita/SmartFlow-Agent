package schedule

import "strings"

// buildAnalyzeHealthDecisionV2 生成 analyze_health 在主动优化场景下的最终裁决。
//
// 职责边界：
// 1. 先尊重 base 层的判断：只有 base 明确允许继续优化时，才进入候选枚举。
// 2. 候选只来自后端已经验证合法、并且复诊后确实变好的 move/swap 方案。
// 3. 若没有真正改善的候选，则明确返回 close，避免把 LLM 推回开放式全窗搜索。
func buildAnalyzeHealthDecisionV2(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) analyzeHealthDecision {
	base := buildAnalyzeHealthDecisionBase(state, snapshot)
	decision := analyzeHealthDecision{
		ShouldContinueOptimize: base.ShouldContinueOptimize,
		PrimaryProblem:         base.PrimaryProblem,
		ProblemScope:           base.ProblemScope,
		IsForcedImperfection:   base.IsForcedImperfection,
		RecommendedOperation:   base.RecommendedOperation,
		ImprovementSignal: buildHealthImprovementSignal(
			snapshot.Rhythm,
			snapshot.Tightness,
			base.ProblemScope,
			base.RecommendedOperation,
			snapshot.Profile,
			snapshot.Feasibility,
		),
	}

	if !shouldEnterHealthCandidateLoop(base) {
		decision.Candidates = []analyzeHealthCandidate{
			buildHealthCloseCandidate("保持当前安排并收口：当前不需要再进入主动优化候选。", snapshot, base),
		}
		decision.ShouldContinueOptimize = false
		return decision
	}

	bestScan, ok := findBestHealthProblemScanResult(state, snapshot)
	if !ok || bestScan.Problem.Kind != healthProblemHeavyAdjacent || bestScan.Problem.Pair == nil {
		decision.Candidates = []analyzeHealthCandidate{
			buildHealthCloseCandidate("保持当前安排并收口：当前没有值得继续处理的局部认知问题。", snapshot, base),
		}
		decision.ShouldContinueOptimize = false
		decision.PrimaryProblem = "当前没有发现值得继续处理的局部认知问题"
		decision.ProblemScope = nil
		decision.RecommendedOperation = "close"
		if snapshot.Tightness.TightnessLevel == "locked" || snapshot.Tightness.TightnessLevel == "tight" {
			decision.IsForcedImperfection = true
		}
		decision.ImprovementSignal = buildHealthImprovementSignal(
			snapshot.Rhythm,
			snapshot.Tightness,
			decision.ProblemScope,
			decision.RecommendedOperation,
			snapshot.Profile,
			snapshot.Feasibility,
		)
		return decision
	}

	decision.PrimaryProblem = bestScan.Problem.Summary
	decision.ProblemScope = bestScan.Problem.Scope
	decision.Candidates = append(decision.Candidates, bestScan.Candidates...)
	decision.Candidates = append(decision.Candidates,
		buildHealthCloseCandidate("如果不想继续挪动，也可以保持当前安排并直接收口。", snapshot, base),
	)
	decision.ShouldContinueOptimize = true
	decision.RecommendedOperation = strings.TrimSpace(bestScan.Candidates[0].Tool)
	decision.ImprovementSignal = buildHealthImprovementSignal(
		snapshot.Rhythm,
		snapshot.Tightness,
		decision.ProblemScope,
		decision.RecommendedOperation,
		snapshot.Profile,
		snapshot.Feasibility,
	)
	return decision
}

// findBestHealthProblemScanResult 每轮重扫所有 heavy_adjacent 天，并选出当前收益最高的一天。
//
// 步骤化说明：
// 1. 先收集所有仍需关注的 heavy_adjacent 天；这里只扫描问题天，不改候选类型。
// 2. 再对每一天复用现有单天候选试算逻辑，保持“合法且复诊后确实变好”这一过滤语义不变。
// 3. 最后只返回收益最高且达到最小阈值的一天；最终 decision.candidates 仍只来自这一天天然候选集。
func findBestHealthProblemScanResult(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) (analyzeHealthProblemScanResult, bool) {
	problems := collectRepairableHeavyAdjacentProblems(state, snapshot)
	if len(problems) == 0 {
		return analyzeHealthProblemScanResult{}, false
	}

	results := make([]analyzeHealthProblemScanResult, 0, len(problems))
	for _, problem := range problems {
		scan, ok := buildHealthProblemScanResult(state, snapshot, problem)
		if !ok {
			continue
		}
		results = append(results, scan)
	}
	return selectBestHealthProblemScanResult(results)
}

// shouldEnterHealthCandidateLoop 判断本轮是否应进入“候选式主动优化”。
//
// 说明：
// 1. 只有 base 已判定“值得继续优化”时才放行。
// 2. 当前主动优化闭环只接受 move / swap 两类操作，其它动作不进入候选生成。
// 3. 这样可以挡住 “ask_user / close / forced imperfection” 被后续枚举误覆盖的问题。
func shouldEnterHealthCandidateLoop(base analyzeHealthDecisionBase) bool {
	if !base.ShouldContinueOptimize {
		return false
	}
	switch strings.TrimSpace(base.RecommendedOperation) {
	case "move", "swap":
		return true
	default:
		return false
	}
}
