package schedule

import (
	"fmt"
	"sort"
	"strings"
)

const (
	healthProblemNone            = ""
	healthProblemHeavyAdjacent   = "heavy_adjacent"
	analyzeHealthMinBenefitScore = 1

	healthCandidateEffectImprove        = "improve"
	healthCandidateEffectPartialImprove = "partial_improve"
	healthCandidateEffectShift          = "shift"
	healthCandidateEffectNoGain         = "no_gain"
	healthCandidateEffectRegress        = "regress"
	healthCandidateEffectClose          = "close"
)

type analyzeHealthCandidate struct {
	CandidateID string                      `json:"candidate_id"`
	Tool        string                      `json:"tool,omitempty"`
	Arguments   map[string]any              `json:"arguments,omitempty"`
	Summary     string                      `json:"summary"`
	Effect      string                      `json:"effect"`
	After       analyzeHealthCandidateAfter `json:"after"`
}

type analyzeHealthCandidateAfter struct {
	CanClose             bool    `json:"can_close"`
	PrimaryProblem       string  `json:"primary_problem"`
	RecommendedOperation string  `json:"recommended_operation,omitempty"`
	HeavyAdjacentDays    int     `json:"heavy_adjacent_days"`
	MaxSwitchCount       int     `json:"max_switch_count"`
	SameTypeRatio        float64 `json:"same_type_transition_ratio"`
}

type analyzeHealthSnapshot struct {
	Subjects    []analyzeSubjectItem
	Days        []analyzeContextDay
	Rhythm      analyzeRhythmMetrics
	Tightness   analyzeTightnessMetrics
	Profile     analyzeSemanticProfileMetrics
	Feasibility analyzeFeasibility
}

type analyzeHealthProblem struct {
	Kind       string
	DayIndex   int
	Summary    string
	Scope      *analyzeProblemScope
	Pair       *analyzeHeavyAdjacentPair
	PreferSwap bool
}

type analyzeHealthDecisionBase struct {
	ShouldContinueOptimize bool
	PrimaryProblem         string
	ProblemScope           *analyzeProblemScope
	IsForcedImperfection   bool
	RecommendedOperation   string
}

type analyzeHealthCandidateRanked struct {
	Candidate analyzeHealthCandidate
	Score     int
}

type analyzeHealthProblemScanResult struct {
	Problem         analyzeHealthProblem
	Candidates      []analyzeHealthCandidate
	BestScore       int
	BestCandidateID string
	PriorityScore   int
}

type analyzeHealthProblemScoreOnlyResult struct {
	Problem       analyzeHealthProblem
	BestScore     int
	BestOperation string
	PriorityScore int
}

type analyzeHeavyAdjacentPair struct {
	DayIndex int
	Left     ScheduleTask
	Right    ScheduleTask
}

// buildAnalyzeHealthSnapshotFromState 统一计算 analyze_health 需要复用的核心快照。
//
// 职责边界：
// 1. 这里只负责“从 state 派生节奏/紧度/画像/可行性”；
// 2. 不直接生成 issues / 候选动作，避免分析层和决策层耦死；
// 3. 候选模拟复诊时也复用这条路径，保证前后口径一致。
func buildAnalyzeHealthSnapshotFromState(state *ScheduleState) analyzeHealthSnapshot {
	subjects := computeAnalyzeSubjectMetricsV2(state, true, "")
	days := computeAnalyzeContextDaysV2(state)
	rhythmOverview := computeAnalyzeRhythmOverviewV2(subjects, days)
	rhythm := analyzeRhythmMetrics{
		Overview: rhythmOverview,
		Subjects: subjects,
		Days:     days,
	}
	return analyzeHealthSnapshot{
		Subjects:    subjects,
		Days:        days,
		Rhythm:      rhythm,
		Tightness:   computeAnalyzeTightnessMetrics(state, rhythm),
		Profile:     computeSemanticProfileMetrics(subjects),
		Feasibility: computeHealthFeasibilityV2(state),
	}
}

// buildAnalyzeHealthDecisionBase 生成“不带候选动作”的基础裁决结果。
//
// 说明：
// 1. 这层只回答“当前是否还有值得继续修的主问题”，不负责枚举具体 move/swap 参数；
// 2. 主分析与候选模拟都会复用这层，避免出现“主诊断口径”和“候选复诊口径”不一致；
// 3. 当前 P1 只把 heavy_adjacent 作为可进入候选枚举的问题，其它问题仅保留指标展示，不驱动继续优化。
func buildAnalyzeHealthDecisionBase(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) analyzeHealthDecisionBase {
	decision := analyzeHealthDecisionBase{
		PrimaryProblem:       "当前没有发现值得继续处理的局部认知问题",
		RecommendedOperation: "close",
	}

	if !snapshot.Feasibility.IsFeasible {
		decision.PrimaryProblem = fmt.Sprintf("当前时间窗容量不足，还缺 %d 节可用容量", snapshot.Feasibility.CapacityGap)
		decision.IsForcedImperfection = true
		decision.RecommendedOperation = "ask_user"
		return decision
	}
	if snapshot.Profile.MissingCompleteProfileCount > 0 {
		decision.PrimaryProblem = fmt.Sprintf("仍有 %d 门任务类缺少完整语义画像，先补齐再谈主动优化", snapshot.Profile.MissingCompleteProfileCount)
		decision.RecommendedOperation = "ask_user"
		return decision
	}

	problem, ok := pickPrimaryHealthProblem(state, snapshot)
	if !ok {
		return decision
	}
	decision.PrimaryProblem = problem.Summary
	decision.ProblemScope = problem.Scope

	// P1 只支持“高认知相邻”进入候选求解；其他问题先只做观测，不驱动自动挪动。
	if problem.Kind != healthProblemHeavyAdjacent {
		return decision
	}

	if snapshot.Tightness.TightnessLevel == "locked" {
		decision.IsForcedImperfection = true
		return decision
	}

	decision.RecommendedOperation = "swap"
	decision.ShouldContinueOptimize = true
	return decision
}

// buildAnalyzeHealthFinalDecisionBrief 基于当前 active 扫描器语义，生成候选 after 视图所需的最小判定。
//
// 职责边界：
// 1. 这里只产出 should_continue_optimize / primary_problem / recommended_operation / is_forced_imperfection。
// 2. 这里通过 score-only 扫描层复用“全局扫描 + 最小收益阈值”的收口规则，但不构造 candidates / summary / after。
// 3. score-only 扫描只能读取 state/snapshot 并计算最佳收益，绝不能回流到 simulate 后再调用 brief，避免递归闭环。
func buildAnalyzeHealthFinalDecisionBrief(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) analyzeHealthDecisionBase {
	base := buildAnalyzeHealthDecisionBase(state, snapshot)
	decision := analyzeHealthDecisionBase{
		ShouldContinueOptimize: base.ShouldContinueOptimize,
		PrimaryProblem:         base.PrimaryProblem,
		ProblemScope:           base.ProblemScope,
		IsForcedImperfection:   base.IsForcedImperfection,
		RecommendedOperation:   base.RecommendedOperation,
	}

	if !shouldEnterHealthCandidateLoop(base) {
		decision.ShouldContinueOptimize = false
		return decision
	}

	bestScoreOnly, ok := findBestHealthProblemScoreOnly(state, snapshot)
	if !ok || bestScoreOnly.Problem.Kind != healthProblemHeavyAdjacent || bestScoreOnly.Problem.Pair == nil {
		decision.ShouldContinueOptimize = false
		decision.PrimaryProblem = "当前没有发现值得继续处理的局部认知问题"
		decision.ProblemScope = nil
		decision.RecommendedOperation = "close"
		if snapshot.Tightness.TightnessLevel == "locked" || snapshot.Tightness.TightnessLevel == "tight" {
			decision.IsForcedImperfection = true
		}
		return decision
	}

	decision.ShouldContinueOptimize = true
	decision.PrimaryProblem = bestScoreOnly.Problem.Summary
	decision.ProblemScope = bestScoreOnly.Problem.Scope
	decision.RecommendedOperation = strings.TrimSpace(bestScoreOnly.BestOperation)
	return decision
}

// buildAnalyzeHealthCandidateAfterBrief 生成候选执行后的轻量复诊摘要。
//
// 职责边界：
// 1. 只负责为 candidate.after / candidate.summary 提供“执行后看起来如何”的展示结论；
// 2. 不负责再次枚举 move/swap 候选，也不决定顶层 analyze_health 是否继续优化；
// 3. 输入是已经模拟执行后的 state/snapshot，输出沿用 analyzeHealthDecisionBase 的字段语义。
func buildAnalyzeHealthCandidateAfterBrief(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) analyzeHealthDecisionBase {
	base := buildAnalyzeHealthDecisionBase(state, snapshot)
	decision := analyzeHealthDecisionBase{
		ShouldContinueOptimize: base.ShouldContinueOptimize,
		PrimaryProblem:         base.PrimaryProblem,
		ProblemScope:           base.ProblemScope,
		IsForcedImperfection:   base.IsForcedImperfection,
		RecommendedOperation:   base.RecommendedOperation,
	}

	if !shouldEnterHealthCandidateLoop(base) {
		decision.ShouldContinueOptimize = false
		return decision
	}

	// 1. candidate.after 位于候选模拟内层，不能再跑全局候选枚举。
	// 2. 顶层 buildAnalyzeHealthDecisionV2 已经负责严谨筛选“是否值得继续优化”；
	//    这里保留基础诊断即可，避免每个候选递归触发 score-only 全局扫描。
	// 3. 若仍存在基础诊断认为可优化的问题，则如实展示给前端和 LLM；下一轮会再次调用
	//    analyze_health 做正式复诊，作为真正的收口依据。
	return decision
}

// pickPrimaryHealthProblem 选择当前最值得处理的局部问题。
func pickPrimaryHealthProblem(state *ScheduleState, snapshot analyzeHealthSnapshot) (analyzeHealthProblem, bool) {
	best := analyzeHealthProblem{}
	bestScore := -1
	bestBalanceAlignment := -1
	for _, day := range snapshot.Rhythm.Days {
		if !day.HeavyAdjacent || shouldTreatHeavyAdjacencyAsAcceptable(snapshot.Rhythm, day) {
			continue
		}
		pair, ok := findHeavyAdjacentPairOnDay(state, day.DayIndex)
		if !ok {
			continue
		}
		score := scoreHeavyAdjacentProblem(day, pair)
		balanceAlignment := scoreHeavyAdjacentBalanceAlignment(snapshot.Rhythm.Overview.BlockBalance, day)
		if score < bestScore {
			continue
		}
		if score == bestScore && balanceAlignment <= bestBalanceAlignment {
			continue
		}
		bestScore = score
		bestBalanceAlignment = balanceAlignment
		best = analyzeHealthProblem{
			Kind:     healthProblemHeavyAdjacent,
			DayIndex: day.DayIndex,
			Summary:  fmt.Sprintf("第 %d 天存在高认知强度任务相邻，学起来会发紧", day.DayIndex),
			Scope: &analyzeProblemScope{
				DayRange: []int{day.DayIndex},
				TaskIDs:  []int{pair.Left.StateID, pair.Right.StateID},
			},
			Pair:       pair,
			PreferSwap: true,
		}
	}
	if best.Kind == "" {
		return analyzeHealthProblem{}, false
	}
	return best, true
}

// collectRepairableHeavyAdjacentProblems 收集当前所有仍值得扫描的 heavy_adjacent 问题天。
//
// 职责边界：
// 1. 这里只负责把可扫描的问题天列出来，不负责最终选哪一天。
// 2. 仍然只收集 heavy_adjacent，继续复用当前“可接受相邻天”放宽语义。
// 3. 若当天找不到对应相邻任务对，则直接跳过，避免把不完整问题送入候选试算。
func collectRepairableHeavyAdjacentProblems(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) []analyzeHealthProblem {
	if state == nil {
		return nil
	}

	out := make([]analyzeHealthProblem, 0, len(snapshot.Rhythm.Days))
	for _, day := range snapshot.Rhythm.Days {
		if !day.HeavyAdjacent || shouldTreatHeavyAdjacencyAsAcceptable(snapshot.Rhythm, day) {
			continue
		}
		pair, ok := findHeavyAdjacentPairOnDay(state, day.DayIndex)
		if !ok {
			continue
		}
		out = append(out, analyzeHealthProblem{
			Kind:     healthProblemHeavyAdjacent,
			DayIndex: day.DayIndex,
			Summary:  fmt.Sprintf("第 %d 天存在高认知强度任务相邻，学起来会发紧", day.DayIndex),
			Scope: &analyzeProblemScope{
				DayRange: []int{day.DayIndex},
				TaskIDs:  []int{pair.Left.StateID, pair.Right.StateID},
			},
			Pair:       pair,
			PreferSwap: true,
		})
	}
	return out
}

// scoreHeavyAdjacentBalanceAlignment 在 heavy_adjacent 同类问题里提供极轻的节奏倾向参考。
//
// 职责边界：
// 1. 这里只在 heavy_adjacent 候选内部做同分细排，不改变“只处理 heavy_adjacent”的主链路边界。
// 2. 当整体更偏碎时，优先选择同样更碎的 heavy_adjacent 天；当整体更偏压缩时，优先选择块更长的 heavy_adjacent 天。
// 3. 若整体 block_balance 接近 0，则返回 0，保持原有排序语义不变。
func scoreHeavyAdjacentBalanceAlignment(blockBalance int, day analyzeContextDay) int {
	switch {
	case blockBalance > 0:
		return int(day.Fragmentation * 100)
	case blockBalance < 0:
		return day.MaxBlock*10 - day.SwitchCount
	default:
		return 0
	}
}

func scoreHeavyAdjacentProblem(day analyzeContextDay, pair *analyzeHeavyAdjacentPair) int {
	score := 300 + day.SwitchCount*8 + int(day.Fragmentation*20)
	if pair != nil && pair.Left.TaskClassID > 0 && pair.Right.TaskClassID > 0 && pair.Left.TaskClassID != pair.Right.TaskClassID {
		score += 15
	}
	return score
}

func scoreHeavyAdjacentProblemDay(rhythm analyzeRhythmMetrics, dayIndex int) int {
	for _, day := range rhythm.Days {
		if day.DayIndex != dayIndex {
			continue
		}
		return scoreHeavyAdjacentProblem(day, nil)
	}
	return 0
}

// findBestHealthProblemScoreOnly 扫描当前所有 heavy_adjacent 问题天，并只返回“是否存在过阈值候选”所需的最小信息。
//
// 步骤化说明：
// 1. 这里只做 score-only 扫描：复用合法 move/swap 枚举与收益计算，但不构造候选文案、after 视图或 summary。
// 2. 返回值只关心最值得修的问题天、最佳收益和最佳操作类型，供 after brief / 其他只读裁决器复用。
// 3. 一旦最佳收益未过阈值，就直接返回 false，让上层按正式扫描器语义收口。
func findBestHealthProblemScoreOnly(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
) (analyzeHealthProblemScoreOnlyResult, bool) {
	problems := collectRepairableHeavyAdjacentProblems(state, snapshot)
	if len(problems) == 0 {
		return analyzeHealthProblemScoreOnlyResult{}, false
	}

	results := make([]analyzeHealthProblemScoreOnlyResult, 0, len(problems))
	for _, problem := range problems {
		result, ok := buildHealthProblemScoreOnly(state, snapshot, problem)
		if !ok {
			continue
		}
		results = append(results, result)
	}
	return selectBestHealthProblemScoreOnly(results)
}

// buildHealthProblemScoreOnly 只计算单个问题天的最佳候选收益，不构造任何候选展示字段。
//
// 职责边界：
// 1. 这里仍然只允许 move / swap，并复用现有合法性过滤与收益口径。
// 2. 这里不产出 candidate.after / candidate.summary，也不调用 brief helper，避免递归。
// 3. 这条路径存在的唯一目的，是回答“这个问题天还有没有过阈值收益的候选”。
func buildHealthProblemScoreOnly(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
	problem analyzeHealthProblem,
) (analyzeHealthProblemScoreOnlyResult, bool) {
	if state == nil || problem.Kind != healthProblemHeavyAdjacent || problem.Pair == nil {
		return analyzeHealthProblemScoreOnlyResult{}, false
	}

	bestScore := 0
	bestOperation := ""
	found := false

	pool := collectSuggestedTaskItems(state)
	movable := extractSuggestedProblemTasks(problem.Pair)
	if len(movable) == 0 {
		return analyzeHealthProblemScoreOnlyResult{}, false
	}

	checked := 0
	for _, anchor := range movable {
		for _, other := range pool {
			if anchor.StateID == other.StateID {
				continue
			}
			if other.TaskClassID <= 0 || anchor.TaskClassID <= 0 || other.TaskClassID == anchor.TaskClassID {
				continue
			}
			if taskDuration(anchor) != taskDuration(other) {
				continue
			}
			checked++
			score, ok := simulateHealthSwapScoreOnly(state, snapshot, problem, anchor, other)
			if ok {
				bestScore, bestOperation, found = updateBestHealthScoreOnly(score, "swap", bestScore, bestOperation, found)
			}
			if checked >= 48 {
				goto movePhase
			}
		}
	}

movePhase:
	for _, task := range movable {
		placements := enumerateLegalMovePlacements(state, task, 24)
		for _, target := range placements {
			score, ok := simulateHealthMoveScoreOnly(state, snapshot, problem, task, target)
			if ok {
				bestScore, bestOperation, found = updateBestHealthScoreOnly(score, "move", bestScore, bestOperation, found)
			}
		}
	}

	if !found {
		return analyzeHealthProblemScoreOnlyResult{}, false
	}
	return analyzeHealthProblemScoreOnlyResult{
		Problem:       problem,
		BestScore:     bestScore,
		BestOperation: bestOperation,
		PriorityScore: scoreHeavyAdjacentProblemDay(snapshot.Rhythm, problem.DayIndex),
	}, true
}

func simulateHealthSwapScoreOnly(
	state *ScheduleState,
	baseline analyzeHealthSnapshot,
	problem analyzeHealthProblem,
	anchor ScheduleTask,
	other ScheduleTask,
) (int, bool) {
	clone := state.Clone()
	if clone == nil {
		return 0, false
	}
	result := Swap(clone, anchor.StateID, other.StateID)
	if strings.Contains(result, "失败") || strings.Contains(result, "澶辫触") {
		return 0, false
	}

	after := buildAnalyzeHealthSnapshotFromState(clone)
	_, score, ok := evaluateHealthCandidateScoreOnly(baseline, after, problem, "swap", 4)
	if !ok {
		return 0, false
	}
	return applyHealthCandidateRankingBias("swap", score), true
}

func simulateHealthMoveScoreOnly(
	state *ScheduleState,
	baseline analyzeHealthSnapshot,
	problem analyzeHealthProblem,
	task ScheduleTask,
	target TaskSlot,
) (int, bool) {
	clone := state.Clone()
	if clone == nil {
		return 0, false
	}
	result := Move(clone, task.StateID, target.Day, target.SlotStart)
	if strings.Contains(result, "失败") || strings.Contains(result, "澶辫触") {
		return 0, false
	}

	after := buildAnalyzeHealthSnapshotFromState(clone)
	moveCost := absInt(target.Day-task.Slots[0].Day)*2 + absInt(target.SlotStart-task.Slots[0].SlotStart)
	_, score, ok := evaluateHealthCandidateScoreOnly(baseline, after, problem, "move", moveCost)
	if !ok {
		return 0, false
	}
	return applyHealthCandidateRankingBias("move", score), true
}

func selectBestHealthProblemScoreOnly(
	results []analyzeHealthProblemScoreOnlyResult,
) (analyzeHealthProblemScoreOnlyResult, bool) {
	if len(results) == 0 {
		return analyzeHealthProblemScoreOnlyResult{}, false
	}

	best := analyzeHealthProblemScoreOnlyResult{}
	bestSet := false
	for _, item := range results {
		if !meetsHealthCandidateBenefitThreshold(item.BestScore) {
			continue
		}
		if !bestSet || shouldPreferHealthScoreCandidate(
			item.BestScore,
			item.PriorityScore,
			item.Problem.DayIndex,
			best.BestScore,
			best.PriorityScore,
			best.Problem.DayIndex,
		) {
			best = item
			bestSet = true
		}
	}
	if !bestSet {
		return analyzeHealthProblemScoreOnlyResult{}, false
	}
	return best, true
}

func updateBestHealthScoreOnly(
	score int,
	operation string,
	bestScore int,
	bestOperation string,
	found bool,
) (int, string, bool) {
	if !found {
		return score, operation, true
	}
	if score > bestScore {
		return score, operation, true
	}
	if score == bestScore && strings.TrimSpace(operation) < strings.TrimSpace(bestOperation) {
		return score, operation, true
	}
	return bestScore, bestOperation, true
}

// applyHealthCandidateRankingBias 统一补齐候选最终排序分里的轻量工具偏置。
//
// 职责边界：
// 1. 这里只负责把“基础收益分”映射成“最终排序分”，避免正式扫描器和 score-only 扫描口径漂移。
// 2. 当前只有 swap 额外加分；move 保持原分不变。
// 3. 该函数不参与候选合法性判断，只参与“谁更值得优先处理”的排序。
func applyHealthCandidateRankingBias(operation string, baseScore int) int {
	score := baseScore
	if strings.EqualFold(strings.TrimSpace(operation), "swap") {
		score += 12
	}
	return score
}

// selectBestHealthProblemScanResult 从所有单天扫描结果中挑出本轮最值得修的一天。
//
// 职责边界：
// 1. 这里只比较“各天最佳候选”的收益，不拼接多天候选，也不改变候选内容。
// 2. 先按 benefit_score 选天；若收益相同，再退回到原有问题优先级与天序做稳定 tie-break。
// 3. 若第一名收益不足最小阈值，则返回 false，交给上层直接收口。
func selectBestHealthProblemScanResult(
	results []analyzeHealthProblemScanResult,
) (analyzeHealthProblemScanResult, bool) {
	if len(results) == 0 {
		return analyzeHealthProblemScanResult{}, false
	}

	best := analyzeHealthProblemScanResult{}
	bestSet := false
	for _, item := range results {
		if !meetsHealthCandidateBenefitThreshold(item.BestScore) {
			continue
		}
		if !bestSet || shouldPreferHealthScoreCandidate(
			item.BestScore,
			item.PriorityScore,
			item.Problem.DayIndex,
			best.BestScore,
			best.PriorityScore,
			best.Problem.DayIndex,
		) {
			best = item
			bestSet = true
		}
	}
	if !bestSet {
		return analyzeHealthProblemScanResult{}, false
	}
	return best, true
}

func meetsHealthCandidateBenefitThreshold(score int) bool {
	return score >= analyzeHealthMinBenefitScore
}

func shouldPreferHealthScoreCandidate(
	leftScore int,
	leftPriority int,
	leftDay int,
	rightScore int,
	rightPriority int,
	rightDay int,
) bool {
	switch {
	case leftScore > rightScore:
		return true
	case leftScore < rightScore:
		return false
	case leftPriority > rightPriority:
		return true
	case leftPriority < rightPriority:
		return false
	default:
		return leftDay < rightDay
	}
}

// buildHealthCandidatesForProblem 为主问题生成“可直接抄去调用写工具”的合法候选。
//
// 设计说明：
// 1. 候选空间完全由现有写工具的合法性约束定义，尤其复用“前驱/后继顺序边界”；
// 2. 后端会先在内存里模拟，再做一次 analyze_health 同口径复诊；
// 3. 只保留真正减轻问题的 top5，过滤掉平移、无增益和恶化候选。
func buildHealthCandidatesForProblem(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
	problem analyzeHealthProblem,
) []analyzeHealthCandidate {
	scan, ok := buildHealthProblemScanResult(state, snapshot, problem)
	if !ok {
		return nil
	}
	return scan.Candidates
}

// buildHealthProblemScanResult 生成单个 heavy_adjacent 问题天的候选扫描结果。
//
// 步骤化说明：
// 1. 先复用现有 move / swap 枚举与复诊过滤，保证只比较“原本就合法且确实变好”的候选。
// 2. 再按现有 score 规则排序并截取该天 top5 候选，保持返回给 LLM 的候选语义不变。
// 3. 额外保留该天最佳候选的 score，供上层做“先选天，再返回该天候选集”的全局比较。
func buildHealthProblemScanResult(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
	problem analyzeHealthProblem,
) (analyzeHealthProblemScanResult, bool) {
	if state == nil || problem.Kind != healthProblemHeavyAdjacent || problem.Pair == nil {
		return analyzeHealthProblemScanResult{}, false
	}

	ranked := make([]analyzeHealthCandidateRanked, 0, 16)
	ranked = append(ranked, enumerateSwapCandidates(state, snapshot, problem)...)
	ranked = append(ranked, enumerateMoveCandidates(state, snapshot, problem)...)
	if len(ranked) == 0 {
		return analyzeHealthProblemScanResult{}, false
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Score != ranked[j].Score {
			return ranked[i].Score > ranked[j].Score
		}
		leftTool := strings.TrimSpace(ranked[i].Candidate.Tool)
		rightTool := strings.TrimSpace(ranked[j].Candidate.Tool)
		if leftTool != rightTool {
			return leftTool < rightTool
		}
		return ranked[i].Candidate.CandidateID < ranked[j].Candidate.CandidateID
	})

	out := make([]analyzeHealthCandidate, 0, minInt(len(ranked), 5))
	seen := make(map[string]struct{}, len(ranked))
	for _, item := range ranked {
		key := item.Candidate.Tool + "::" + marshalCandidateArgumentsKey(item.Candidate.Arguments)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item.Candidate)
		if len(out) >= 5 {
			break
		}
	}
	if len(out) == 0 {
		return analyzeHealthProblemScanResult{}, false
	}

	return analyzeHealthProblemScanResult{
		Problem:         problem,
		Candidates:      out,
		BestScore:       ranked[0].Score,
		BestCandidateID: ranked[0].Candidate.CandidateID,
		PriorityScore:   scoreHeavyAdjacentProblemDay(snapshot.Rhythm, problem.DayIndex),
	}, true
}

func enumerateSwapCandidates(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
	problem analyzeHealthProblem,
) []analyzeHealthCandidateRanked {
	pair := problem.Pair
	if pair == nil {
		return nil
	}
	pool := collectSuggestedTaskItems(state)
	movable := extractSuggestedProblemTasks(pair)
	if len(movable) == 0 || len(pool) == 0 {
		return nil
	}

	out := make([]analyzeHealthCandidateRanked, 0, 12)
	checked := 0
	for _, anchor := range movable {
		for _, other := range pool {
			if anchor.StateID == other.StateID {
				continue
			}
			if other.TaskClassID <= 0 || anchor.TaskClassID <= 0 || other.TaskClassID == anchor.TaskClassID {
				continue
			}
			if taskDuration(anchor) != taskDuration(other) {
				continue
			}
			checked++
			candidate, score, ok := simulateHealthSwapCandidate(state, snapshot, problem, anchor, other)
			if ok {
				out = append(out, analyzeHealthCandidateRanked{
					Candidate: candidate,
					Score:     applyHealthCandidateRankingBias("swap", score),
				})
			}
			if checked >= 48 {
				return out
			}
		}
	}
	return out
}

func enumerateMoveCandidates(
	state *ScheduleState,
	snapshot analyzeHealthSnapshot,
	problem analyzeHealthProblem,
) []analyzeHealthCandidateRanked {
	pair := problem.Pair
	if pair == nil {
		return nil
	}
	movable := extractSuggestedProblemTasks(pair)
	if len(movable) == 0 {
		return nil
	}

	out := make([]analyzeHealthCandidateRanked, 0, 20)
	for _, task := range movable {
		placements := enumerateLegalMovePlacements(state, task, 24)
		for _, target := range placements {
			candidate, score, ok := simulateHealthMoveCandidate(state, snapshot, problem, task, target)
			if !ok {
				continue
			}
			out = append(out, analyzeHealthCandidateRanked{Candidate: candidate, Score: score})
		}
	}
	return out
}

func simulateHealthSwapCandidate(
	state *ScheduleState,
	baseline analyzeHealthSnapshot,
	problem analyzeHealthProblem,
	anchor ScheduleTask,
	other ScheduleTask,
) (analyzeHealthCandidate, int, bool) {
	clone := state.Clone()
	if clone == nil {
		return analyzeHealthCandidate{}, 0, false
	}
	result := Swap(clone, anchor.StateID, other.StateID)
	if strings.Contains(result, "失败") {
		return analyzeHealthCandidate{}, 0, false
	}

	after := buildAnalyzeHealthSnapshotFromState(clone)
	effect, score, afterDecision, ok := evaluateHealthCandidateOutcome(baseline, after, clone, problem, "swap", 4)
	if !ok {
		return analyzeHealthCandidate{}, 0, false
	}

	candidate := analyzeHealthCandidate{
		CandidateID: fmt.Sprintf("swap_%d_%d", anchor.StateID, other.StateID),
		Tool:        "swap",
		Arguments: map[string]any{
			"task_a": anchor.StateID,
			"task_b": other.StateID,
		},
		Summary: buildHealthCandidateSummary(
			fmt.Sprintf("交换 [%d]%s 与 [%d]%s", anchor.StateID, anchor.Name, other.StateID, other.Name),
			baseline,
			after,
			afterDecision,
		),
		Effect: effect,
		After:  buildHealthCandidateAfter(after, afterDecision),
	}
	return candidate, score, true
}

func simulateHealthMoveCandidate(
	state *ScheduleState,
	baseline analyzeHealthSnapshot,
	problem analyzeHealthProblem,
	task ScheduleTask,
	target TaskSlot,
) (analyzeHealthCandidate, int, bool) {
	clone := state.Clone()
	if clone == nil {
		return analyzeHealthCandidate{}, 0, false
	}
	result := Move(clone, task.StateID, target.Day, target.SlotStart)
	if strings.Contains(result, "失败") {
		return analyzeHealthCandidate{}, 0, false
	}

	after := buildAnalyzeHealthSnapshotFromState(clone)
	moveCost := absInt(target.Day-task.Slots[0].Day)*2 + absInt(target.SlotStart-task.Slots[0].SlotStart)
	effect, score, afterDecision, ok := evaluateHealthCandidateOutcome(baseline, after, clone, problem, "move", moveCost)
	if !ok {
		return analyzeHealthCandidate{}, 0, false
	}

	candidate := analyzeHealthCandidate{
		CandidateID: fmt.Sprintf("move_%d_%d_%d", task.StateID, target.Day, target.SlotStart),
		Tool:        "move",
		Arguments: map[string]any{
			"task_id":        task.StateID,
			"new_day":        target.Day,
			"new_slot_start": target.SlotStart,
		},
		Summary: buildHealthCandidateSummary(
			fmt.Sprintf("移动 [%d]%s 到第 %d 天第 %d 节", task.StateID, task.Name, target.Day, target.SlotStart),
			baseline,
			after,
			afterDecision,
		),
		Effect: effect,
		After:  buildHealthCandidateAfter(after, afterDecision),
	}
	return candidate, score, true
}

func enumerateLegalMovePlacements(state *ScheduleState, task ScheduleTask, limit int) []TaskSlot {
	if state == nil || len(task.Slots) == 0 || limit <= 0 {
		return nil
	}
	duration := taskDuration(task)
	if duration <= 0 {
		return nil
	}

	out := make([]TaskSlot, 0, limit)
	for day := 1; day <= state.Window.TotalDays; day++ {
		for _, gap := range findFreeRangesOnDay(state, day) {
			maxStart := gap.slotEnd - duration + 1
			for slotStart := gap.slotStart; slotStart <= maxStart; slotStart++ {
				target := TaskSlot{Day: day, SlotStart: slotStart, SlotEnd: slotStart + duration - 1}
				if sameTaskSlots(task.Slots, []TaskSlot{target}) {
					continue
				}
				if err := validateLocalOrderForSinglePlacement(state, task.StateID, []TaskSlot{target}); err != nil {
					continue
				}
				if conflict := findConflict(state, target.Day, target.SlotStart, target.SlotEnd, task.StateID); conflict != nil {
					continue
				}
				out = append(out, target)
				if len(out) >= limit {
					return out
				}
			}
		}
	}
	return out
}

func evaluateHealthCandidateOutcome(
	baseline analyzeHealthSnapshot,
	after analyzeHealthSnapshot,
	afterState *ScheduleState,
	problem analyzeHealthProblem,
	operation string,
	moveCost int,
) (string, int, analyzeHealthDecisionBase, bool) {
	afterDecision := buildAnalyzeHealthCandidateAfterBrief(afterState, after)
	effect, score, ok := evaluateHealthCandidateScoreOnly(
		baseline,
		after,
		problem,
		operation,
		moveCost,
	)
	return effect, score, afterDecision, ok
}

// evaluateHealthCandidateScoreOnly 只计算候选的收益与是否保留，不构造任何 after 视图。
//
// 职责边界：
// 1. 这里复用当前 active 候选过滤口径：只保留真正改善的候选，并返回排序所需 score。
// 2. 这里绝不依赖 afterDecision / brief helper，供 score-only 扫描层安全复用。
// 3. 这样正式扫描器与 after brief 都能共享同一套收益定义，同时避免递归闭环。
func evaluateHealthCandidateScoreOnly(
	baseline analyzeHealthSnapshot,
	after analyzeHealthSnapshot,
	problem analyzeHealthProblem,
	operation string,
	moveCost int,
) (string, int, bool) {
	baselineRepairable := countRepairableHeavyAdjacentDays(baseline.Rhythm)
	afterRepairable := countRepairableHeavyAdjacentDays(after.Rhythm)
	afterProblemStill := isRepairableHeavyAdjacentDay(after.Rhythm, problem.DayIndex)

	effect := healthCandidateEffectNoGain
	switch {
	case afterRepairable < baselineRepairable:
		if afterRepairable > 0 {
			effect = healthCandidateEffectPartialImprove
		} else {
			effect = healthCandidateEffectImprove
		}
	case afterRepairable > baselineRepairable:
		effect = healthCandidateEffectRegress
	case afterProblemStill:
		effect = healthCandidateEffectNoGain
	default:
		effect = healthCandidateEffectShift
	}
	if effect != healthCandidateEffectImprove && effect != healthCandidateEffectPartialImprove {
		return effect, 0, false
	}

	score := (baselineRepairable-afterRepairable)*1000 +
		(baseline.Rhythm.Overview.MaxSwitchCount-after.Rhythm.Overview.MaxSwitchCount)*20 +
		int((after.Rhythm.Overview.SameTypeTransitionRatio-baseline.Rhythm.Overview.SameTypeTransitionRatio)*100) -
		moveCost
	if strings.EqualFold(strings.TrimSpace(operation), "swap") {
		score += 10
	}
	return effect, score, true
}

func buildHealthCandidateSummary(
	actionText string,
	baseline analyzeHealthSnapshot,
	after analyzeHealthSnapshot,
	afterDecision analyzeHealthDecisionBase,
) string {
	return fmt.Sprintf(
		"%s；可修复的高认知相邻天数 %d -> %d，max_switch_count %d -> %d，same_type_ratio %.2f -> %.2f；复诊后主问题：%s",
		actionText,
		countRepairableHeavyAdjacentDays(baseline.Rhythm),
		countRepairableHeavyAdjacentDays(after.Rhythm),
		baseline.Rhythm.Overview.MaxSwitchCount,
		after.Rhythm.Overview.MaxSwitchCount,
		baseline.Rhythm.Overview.SameTypeTransitionRatio,
		after.Rhythm.Overview.SameTypeTransitionRatio,
		fallbackHealthProblemText(afterDecision.PrimaryProblem),
	)
}

func buildHealthCandidateAfter(after analyzeHealthSnapshot, decision analyzeHealthDecisionBase) analyzeHealthCandidateAfter {
	return analyzeHealthCandidateAfter{
		CanClose:             !decision.ShouldContinueOptimize,
		PrimaryProblem:       decision.PrimaryProblem,
		RecommendedOperation: decision.RecommendedOperation,
		HeavyAdjacentDays:    after.Rhythm.Overview.HeavyAdjacentDays,
		MaxSwitchCount:       after.Rhythm.Overview.MaxSwitchCount,
		SameTypeRatio:        after.Rhythm.Overview.SameTypeTransitionRatio,
	}
}

func buildHealthCloseCandidate(summary string, baseline analyzeHealthSnapshot, decisionBase analyzeHealthDecisionBase) analyzeHealthCandidate {
	return analyzeHealthCandidate{
		CandidateID: "close",
		Summary:     summary,
		Effect:      healthCandidateEffectClose,
		After:       buildHealthCandidateAfter(baseline, decisionBase),
	}
}

func countRepairableHeavyAdjacentDays(rhythm analyzeRhythmMetrics) int {
	count := 0
	for _, day := range rhythm.Days {
		if !day.HeavyAdjacent {
			continue
		}
		if shouldTreatHeavyAdjacencyAsAcceptable(rhythm, day) {
			continue
		}
		count++
	}
	return count
}

func isRepairableHeavyAdjacentDay(rhythm analyzeRhythmMetrics, dayIndex int) bool {
	for _, day := range rhythm.Days {
		if day.DayIndex != dayIndex {
			continue
		}
		return day.HeavyAdjacent && !shouldTreatHeavyAdjacencyAsAcceptable(rhythm, day)
	}
	return false
}

func findHeavyAdjacentPairOnDay(state *ScheduleState, dayIndex int) (*analyzeHeavyAdjacentPair, bool) {
	if state == nil || dayIndex <= 0 {
		return nil, false
	}
	tasks := collectPlacedTaskBlocksOnDay(state, dayIndex)
	if len(tasks) < 2 {
		return nil, false
	}
	for i := 1; i < len(tasks); i++ {
		left := tasks[i-1]
		right := tasks[i]
		if strings.TrimSpace(left.Category) == "" || strings.TrimSpace(right.Category) == "" {
			continue
		}
		if strings.TrimSpace(left.Category) == strings.TrimSpace(right.Category) {
			continue
		}
		if !isHighIntensityTaskForHealth(state, left) || !isHighIntensityTaskForHealth(state, right) {
			continue
		}
		pair := analyzeHeavyAdjacentPair{
			DayIndex: dayIndex,
			Left:     left,
			Right:    right,
		}
		return &pair, true
	}
	return nil, false
}

func collectPlacedTaskBlocksOnDay(state *ScheduleState, dayIndex int) []ScheduleTask {
	if state == nil || dayIndex <= 0 {
		return nil
	}
	out := make([]ScheduleTask, 0)
	for _, task := range state.Tasks {
		if len(task.Slots) == 0 || isCourseScheduleTask(task) {
			continue
		}
		for _, slot := range task.Slots {
			if slot.Day != dayIndex {
				continue
			}
			out = append(out, task)
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := out[i].Slots[0]
		right := out[j].Slots[0]
		if left.Day != right.Day {
			return left.Day < right.Day
		}
		if left.SlotStart != right.SlotStart {
			return left.SlotStart < right.SlotStart
		}
		if left.SlotEnd != right.SlotEnd {
			return left.SlotEnd < right.SlotEnd
		}
		return out[i].StateID < out[j].StateID
	})
	return out
}

func isHighIntensityTaskForHealth(state *ScheduleState, task ScheduleTask) bool {
	meta := findTaskClassMetaForTask(state, task)
	if meta == nil {
		return false
	}
	return isHighIntensityMeta(*meta)
}

func findTaskClassMetaForTask(state *ScheduleState, task ScheduleTask) *TaskClassMeta {
	if state == nil {
		return nil
	}
	if task.TaskClassID > 0 {
		for i := range state.TaskClasses {
			if state.TaskClasses[i].ID == task.TaskClassID {
				return &state.TaskClasses[i]
			}
		}
	}
	return findTaskClassMetaByName(state, task.Category)
}

func extractSuggestedProblemTasks(pair *analyzeHeavyAdjacentPair) []ScheduleTask {
	if pair == nil {
		return nil
	}
	out := make([]ScheduleTask, 0, 2)
	if IsSuggestedTask(pair.Left) {
		out = append(out, pair.Left)
	}
	if IsSuggestedTask(pair.Right) {
		out = append(out, pair.Right)
	}
	return out
}

func fallbackHealthProblemText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return "可直接收口"
	}
	return text
}

func marshalCandidateArgumentsKey(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}
	parts := make([]string, 0, len(args))
	for key, value := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", key, value))
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

func minInt(a, b int) int {
	if a <= b {
		return a
	}
	return b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
