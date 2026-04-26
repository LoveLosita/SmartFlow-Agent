package schedule

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	analyzeSeverityCritical = "critical"
	analyzeSeverityWarning  = "warning"
	analyzeSeverityInfo     = "info"
)

type analyzeMetricSchemaItem struct {
	Description string `json:"description"`
	Unit        string `json:"unit,omitempty"`
	Direction   string `json:"direction,omitempty"`
}

type analyzeIssueTrigger struct {
	Metric    string  `json:"metric"`
	Operator  string  `json:"operator"`
	Threshold float64 `json:"threshold"`
	Actual    float64 `json:"actual"`
}

type analyzeIssueItem struct {
	IssueID   string               `json:"issue_id"`
	Dimension string               `json:"dimension"`
	Severity  string               `json:"severity"`
	Trigger   *analyzeIssueTrigger `json:"trigger,omitempty"`
}

type analyzeCandidateScope struct {
	DayRange   []int    `json:"day_range"`
	Categories []string `json:"categories"`
	TaskPool   string   `json:"task_pool"`
}

type analyzeNextAction struct {
	ActionID            string                `json:"action_id"`
	Priority            int                   `json:"priority"`
	IntentCode          string                `json:"intent_code"`
	TargetFilter        map[string]any        `json:"target_filter"`
	SlotFilter          map[string]any        `json:"slot_filter"`
	CandidateScope      analyzeCandidateScope `json:"candidate_scope"`
	RequiredReads       []string              `json:"required_reads"`
	SuccessCriteria     map[string]any        `json:"success_criteria"`
	CandidateWriteTools []string              `json:"candidate_write_tools"`
}

type analyzeFeasibility struct {
	IsFeasible  bool   `json:"is_feasible"`
	CapacityGap int    `json:"capacity_gap"`
	ReasonCode  string `json:"reason_code"`
}

type analyzeEnvelope struct {
	Tool         string                             `json:"tool"`
	Success      bool                               `json:"success"`
	MetricSchema map[string]analyzeMetricSchemaItem `json:"metric_schema"`
	Metrics      any                                `json:"metrics"`
	Issues       []analyzeIssueItem                 `json:"issues"`
	NextActions  []analyzeNextAction                `json:"next_actions"`
	Feasibility  *analyzeFeasibility                `json:"feasibility,omitempty"`
	Decision     *analyzeHealthDecision             `json:"decision,omitempty"`
	Error        string                             `json:"error"`
	ErrorCode    string                             `json:"error_code"`
}

type analyzeSubjectItem struct {
	Category           string `json:"category"`
	TaskCount          int    `json:"task_count"`
	PlacedCount        int    `json:"placed_count"`
	PendingCount       int    `json:"pending_count"`
	SubjectType        string `json:"subject_type,omitempty"`
	DifficultyLevel    string `json:"difficulty_level,omitempty"`
	CognitiveIntensity string `json:"cognitive_intensity,omitempty"`
}

type analyzeContextDay struct {
	DayIndex      int      `json:"day_index"`
	SwitchCount   int      `json:"switch_count"`
	Sequence      []string `json:"sequence"`
	MaxBlock      int      `json:"max_block"`
	Fragmentation float64  `json:"fragmentation"`
	HeavyAdjacent bool     `json:"heavy_adjacent"`
}

type analyzeContextOverall struct {
	AvgSwitchesPerDay     float64 `json:"avg_switches_per_day"`
	MaxSwitchDay          int     `json:"max_switch_day"`
	MaxSwitchCount        int     `json:"max_switch_count"`
	AvgBlockSize          float64 `json:"avg_block_size"`
	LongestSameSubjectRun int     `json:"longest_same_subject_run"`
}

type analyzeRhythmOverview struct {
	AvgSwitchesPerDay       float64 `json:"avg_switches_per_day"`
	MaxSwitchDay            int     `json:"max_switch_day"`
	MaxSwitchCount          int     `json:"max_switch_count"`
	AvgBlockSize            float64 `json:"avg_block_size"`
	LongestSameSubjectRun   int     `json:"longest_same_subject_run"`
	HeavyAdjacentDays       int     `json:"heavy_adjacent_days"`
	HighIntensityDays       int     `json:"high_intensity_days"`
	LongHighIntensityDays   int     `json:"long_high_intensity_days"`
	FragmentedCount         int     `json:"fragmented_count"`
	CompressedRunCount      int     `json:"compressed_run_count"`
	BlockBalance            int     `json:"block_balance"`
	SameTypeTransitionRatio float64 `json:"same_type_transition_ratio"`
}

type analyzeRhythmMetrics struct {
	Overview analyzeRhythmOverview `json:"overview"`
	Subjects []analyzeSubjectItem  `json:"subjects"`
	Days     []analyzeContextDay   `json:"days"`
}

type analyzeSlackMetrics struct {
	MovableTaskCount      int     `json:"movable_task_count"`
	RigidTaskCount        int     `json:"rigid_task_count"`
	AvgAlternativeSlots   float64 `json:"avg_alternative_slots"`
	CrossClassSwapOptions int     `json:"cross_class_swap_options"`
	AdjustabilityLevel    string  `json:"adjustability_level"`
	PreferSwap            bool    `json:"prefer_swap"`
}

type analyzeTightnessMetrics struct {
	LocallyMovableTaskCount  int     `json:"locally_movable_task_count"`
	AvgLocalAlternativeSlots float64 `json:"avg_local_alternative_slots"`
	CrossClassSwapOptions    int     `json:"cross_class_swap_options"`
	ForcedHeavyAdjacentDays  int     `json:"forced_heavy_adjacent_days"`
	TightnessLevel           string  `json:"tightness_level"`
}

type analyzeSemanticProfileMetrics struct {
	TotalSubjects               int `json:"total_subjects"`
	MissingSubjectTypeCount     int `json:"missing_subject_type_count"`
	MissingDifficultyCount      int `json:"missing_difficulty_count"`
	MissingCognitiveCount       int `json:"missing_cognitive_count"`
	MissingCompleteProfileCount int `json:"missing_complete_profile_count"`
}

type analyzeProblemScope struct {
	DayRange []int `json:"day_range,omitempty"`
	TaskIDs  []int `json:"task_ids,omitempty"`
}

type analyzeHealthDecision struct {
	ShouldContinueOptimize bool                     `json:"should_continue_optimize"`
	PrimaryProblem         string                   `json:"primary_problem"`
	ProblemScope           *analyzeProblemScope     `json:"problem_scope,omitempty"`
	IsForcedImperfection   bool                     `json:"is_forced_imperfection"`
	RecommendedOperation   string                   `json:"recommended_operation"`
	ImprovementSignal      string                   `json:"improvement_signal"`
	Candidates             []analyzeHealthCandidate `json:"candidates,omitempty"`
}

type analyzeHealthMetrics struct {
	Rhythm    *analyzeRhythmOverview         `json:"rhythm,omitempty"`
	Tightness *analyzeTightnessMetrics       `json:"tightness,omitempty"`
	Profile   *analyzeSemanticProfileMetrics `json:"profile,omitempty"`
	CanClose  bool                           `json:"can_close"`
}

// AnalyzeLoad 已退出主动优化主链路。
func AnalyzeLoad(state *ScheduleState, args map[string]any) string {
	return encodeAnalyzeFailure("analyze_load", "deprecated", "analyze_load 已退出主动优化链路")
}

// AnalyzeSubjects 已被 analyze_rhythm 吸收。
func AnalyzeSubjects(state *ScheduleState, args map[string]any) string {
	return encodeAnalyzeFailure("analyze_subjects", "deprecated", "analyze_subjects 已被 analyze_rhythm 吸收")
}

// AnalyzeContext 已被 analyze_rhythm 吸收。
func AnalyzeContext(state *ScheduleState, args map[string]any) string {
	return encodeAnalyzeFailure("analyze_context", "deprecated", "analyze_context 已被 analyze_rhythm 吸收")
}

// AnalyzeTolerance 已退出主动优化主链路。
func AnalyzeTolerance(state *ScheduleState, args map[string]any) string {
	return encodeAnalyzeFailure("analyze_tolerance", "deprecated", "analyze_tolerance 已退出主动优化链路")
}

// AnalyzeRhythm 输出认知节奏层面的结构化观察。
func AnalyzeRhythm(state *ScheduleState, args map[string]any) string {
	if state == nil {
		return encodeAnalyzeFailure("analyze_rhythm", "state_empty", "日程状态为空")
	}
	allowed := []string{"category", "include_pending", "detail", "hard_categories"}
	if err := validateToolArgsStrict(args, allowed); err != nil {
		return encodeAnalyzeFailure("analyze_rhythm", "invalid_args", err.Error())
	}

	includePending := readBoolAnyWithDefault(args, true, "include_pending")
	categoryFilter := strings.TrimSpace(readStringAny(args, "category"))

	subjects := computeAnalyzeSubjectMetricsV2(state, includePending, categoryFilter)
	days := computeAnalyzeContextDaysV2(state)
	overview := computeAnalyzeRhythmOverviewV2(subjects, days)
	metrics := analyzeRhythmMetrics{
		Overview: overview,
		Subjects: subjects,
		Days:     days,
	}
	issues, actions := buildRhythmIssuesAndActionsV2(metrics)

	return mustEncodeAnalyzeEnvelope(analyzeEnvelope{
		Tool:         "analyze_rhythm",
		Success:      true,
		MetricSchema: rhythmMetricSchemaV2(),
		Metrics:      metrics,
		Issues:       issues,
		NextActions:  actions,
		Error:        "",
		ErrorCode:    "",
	})
}

// AnalyzeHealth 输出主动优化唯一总入口。
func AnalyzeHealth(state *ScheduleState, args map[string]any) string {
	if state == nil {
		return encodeAnalyzeFailure("analyze_health", "state_empty", "日程状态为空")
	}
	allowed := []string{"dimensions", "threshold", "detail"}
	if err := validateToolArgsStrict(args, allowed); err != nil {
		return encodeAnalyzeFailure("analyze_health", "invalid_args", err.Error())
	}
	if len(normalizeHealthDimensionsV3(parseAnalyzeStringSlice(args["dimensions"]))) == 0 {
		return encodeAnalyzeFailure("analyze_health", "invalid_args", "dimensions 全部非法")
	}

	snapshot := buildAnalyzeHealthSnapshotFromState(state)
	rhythm := snapshot.Rhythm.Overview
	rhythmMetrics := snapshot.Rhythm
	tightness := snapshot.Tightness
	profile := snapshot.Profile
	feasibility := snapshot.Feasibility

	issues := make([]analyzeIssueItem, 0)
	rhythmIssues, _ := buildRhythmIssuesAndActionsV2(rhythmMetrics)
	issues = append(issues, rhythmIssues...)

	profileIssues := buildSemanticProfileIssues(profile)
	issues = append(issues, profileIssues...)

	if !feasibility.IsFeasible {
		issues = append(issues, analyzeIssueItem{
			IssueID:   "issue_feasibility_capacity_gap",
			Dimension: "feasibility",
			Severity:  analyzeSeverityCritical,
			Trigger: &analyzeIssueTrigger{
				Metric:    "capacity_gap",
				Operator:  ">",
				Threshold: 0,
				Actual:    float64(feasibility.CapacityGap),
			},
		})
	}

	sort.SliceStable(issues, func(i, j int) bool {
		return analyzeSeverityRank(issues[i].Severity) < analyzeSeverityRank(issues[j].Severity)
	})
	decision := buildAnalyzeHealthDecisionV2(state, snapshot)

	metrics := analyzeHealthMetrics{
		Rhythm:    &rhythm,
		Tightness: &tightness,
		Profile:   &profile,
		CanClose:  !decision.ShouldContinueOptimize,
	}
	return mustEncodeAnalyzeEnvelope(analyzeEnvelope{
		Tool:         "analyze_health",
		Success:      true,
		MetricSchema: healthMetricSchemaV4(),
		Metrics:      metrics,
		Issues:       issues,
		NextActions:  []analyzeNextAction{},
		Feasibility:  &feasibility,
		Decision:     &decision,
		Error:        "",
		ErrorCode:    "",
	})
}

func computeAnalyzeSubjectMetricsV2(state *ScheduleState, includePending bool, categoryFilter string) []analyzeSubjectItem {
	type counter struct {
		taskCount    int
		placedCount  int
		pendingCount int
	}
	counterByCategory := make(map[string]*counter)
	for _, task := range state.Tasks {
		if task.Source != "task_item" || strings.TrimSpace(task.Category) == "" {
			continue
		}
		if categoryFilter != "" && strings.TrimSpace(task.Category) != categoryFilter {
			continue
		}
		if !includePending && IsPendingTask(task) {
			continue
		}
		entry := counterByCategory[task.Category]
		if entry == nil {
			entry = &counter{}
			counterByCategory[task.Category] = entry
		}
		entry.taskCount++
		if IsPendingTask(task) {
			entry.pendingCount++
		}
		if IsSuggestedTask(task) || IsExistingTask(task) {
			entry.placedCount++
		}
	}

	out := make([]analyzeSubjectItem, 0, len(counterByCategory))
	for category, item := range counterByCategory {
		meta := findTaskClassMetaByName(state, category)
		out = append(out, analyzeSubjectItem{
			Category:           category,
			TaskCount:          item.taskCount,
			PlacedCount:        item.placedCount,
			PendingCount:       item.pendingCount,
			SubjectType:        metaValue(meta, func(m *TaskClassMeta) string { return m.SubjectType }),
			DifficultyLevel:    metaValue(meta, func(m *TaskClassMeta) string { return m.DifficultyLevel }),
			CognitiveIntensity: metaValue(meta, func(m *TaskClassMeta) string { return m.CognitiveIntensity }),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Category < out[j].Category
	})
	return out
}

func computeAnalyzeContextDaysV2(state *ScheduleState) []analyzeContextDay {
	out := make([]analyzeContextDay, 0, state.Window.TotalDays)
	highIntensityCategories := make(map[string]struct{})
	for _, meta := range state.TaskClasses {
		if isHighIntensityMeta(meta) {
			highIntensityCategories[strings.TrimSpace(meta.Name)] = struct{}{}
		}
	}

	for day := 1; day <= state.Window.TotalDays; day++ {
		sequence := buildContextDaySequenceV2(state, day)
		switchCount := 0
		maxBlock := 0
		currentBlock := 0
		prev := ""
		heavyAdjacent := false

		for _, category := range sequence {
			if prev != "" && prev != category {
				switchCount++
				_, prevHigh := highIntensityCategories[prev]
				_, currHigh := highIntensityCategories[category]
				if prevHigh && currHigh {
					heavyAdjacent = true
				}
			}
			if category == prev {
				currentBlock++
			} else {
				currentBlock = 1
				prev = category
			}
			if currentBlock > maxBlock {
				maxBlock = currentBlock
			}
		}

		fragmentation := 0.0
		if len(sequence) > 1 {
			fragmentation = safeDivideFloat(float64(switchCount), float64(len(sequence)-1))
		}
		out = append(out, analyzeContextDay{
			DayIndex:      day,
			SwitchCount:   switchCount,
			Sequence:      sequence,
			MaxBlock:      maxBlock,
			Fragmentation: fragmentation,
			HeavyAdjacent: heavyAdjacent,
		})
	}
	return out
}

func computeAnalyzeRhythmOverviewV2(subjects []analyzeSubjectItem, days []analyzeContextDay) analyzeRhythmOverview {
	overview := analyzeRhythmOverview{}
	totalSwitches := 0
	totalBlocks := 0
	totalBlockLength := 0
	totalTransitions := 0
	sameTypeTransitions := 0
	subjectTypeByCategory := make(map[string]string, len(subjects))
	highIntensityByCategory := make(map[string]bool, len(subjects))
	for _, subject := range subjects {
		subjectTypeByCategory[subject.Category] = subject.SubjectType
		highIntensityByCategory[subject.Category] = isHighIntensitySubject(subject)
	}

	for _, day := range days {
		totalSwitches += day.SwitchCount
		if day.SwitchCount > overview.MaxSwitchCount {
			overview.MaxSwitchCount = day.SwitchCount
			overview.MaxSwitchDay = day.DayIndex
		}
		if day.HeavyAdjacent {
			overview.HeavyAdjacentDays++
		}
		if isFragmentedRhythmDay(day) {
			overview.FragmentedCount++
		}
		if day.MaxBlock > overview.LongestSameSubjectRun {
			overview.LongestSameSubjectRun = day.MaxBlock
		}

		currentHighRun := 0
		maxHighRun := 0
		hasHighIntensity := false
		prev := ""
		for _, category := range day.Sequence {
			totalBlocks++
			totalBlockLength++
			if highIntensityByCategory[category] {
				hasHighIntensity = true
				currentHighRun++
				if currentHighRun > maxHighRun {
					maxHighRun = currentHighRun
				}
			} else {
				currentHighRun = 0
			}
			if prev != "" {
				totalTransitions++
				if sameSemanticType(subjectTypeByCategory[prev], subjectTypeByCategory[category]) {
					sameTypeTransitions++
				}
			}
			prev = category
		}
		if hasHighIntensity {
			overview.HighIntensityDays++
		}
		if maxHighRun >= 4 {
			overview.LongHighIntensityDays++
		}
		if isCompressedRhythmDay(day, maxHighRun) {
			overview.CompressedRunCount++
		}
	}

	overview.AvgSwitchesPerDay = safeDivideFloat(float64(totalSwitches), float64(maxInt(len(days), 1)))
	overview.AvgBlockSize = safeDivideFloat(float64(totalBlockLength), float64(maxInt(totalBlocks, 1)))
	overview.BlockBalance = overview.FragmentedCount - overview.CompressedRunCount
	overview.SameTypeTransitionRatio = safeDivideFloat(float64(sameTypeTransitions), float64(maxInt(totalTransitions, 1)))
	return overview
}

// isFragmentedRhythmDay 判断某一天是否更像“认知块切得过碎”。
//
// 职责边界：
// 1. 这里只复用当前 analyze_health 已有的偏碎观察阈值，保证 block_balance 和 issue 口径一致。
// 2. 不负责驱动新的候选类型；当前候选闭环仍然只允许 heavy_adjacent。
// 3. 只要达到 warning 级碎片化观察条件，就把这一天记入 fragmented_count。
func isFragmentedRhythmDay(day analyzeContextDay) bool {
	return day.SwitchCount >= 3 || day.Fragmentation >= 0.55
}

// isCompressedRhythmDay 判断某一天是否更像“认知块过长或过于压缩”。
//
// 职责边界：
// 1. 这里只做统一观测：把“长同科目块”和“高强度连续过长”都视为 compressed 信号。
// 2. 不负责生成 long_block / compressed 的候选动作；这次只补统一指标。
// 3. 同一天即使同时命中两个信号，也只记 1 次，避免 block_balance 被重复放大。
func isCompressedRhythmDay(day analyzeContextDay, maxHighRun int) bool {
	return day.MaxBlock >= 5 || maxHighRun >= 4
}

func buildRhythmIssuesAndActionsV2(metrics analyzeRhythmMetrics) ([]analyzeIssueItem, []analyzeNextAction) {
	issues := make([]analyzeIssueItem, 0)
	actions := make([]analyzeNextAction, 0)
	for _, day := range metrics.Days {
		if day.SwitchCount >= 5 && day.Fragmentation >= 0.75 {
			issues = append(issues, analyzeIssueItem{
				IssueID:   fmt.Sprintf("issue_rhythm_switch_day_%d", day.DayIndex),
				Dimension: "rhythm",
				Severity:  analyzeSeverityCritical,
				Trigger: &analyzeIssueTrigger{
					Metric:    "switch_count",
					Operator:  ">=",
					Threshold: 5,
					Actual:    float64(day.SwitchCount),
				},
			})
			actions = append(actions, analyzeNextAction{
				ActionID:   fmt.Sprintf("na_rhythm_reduce_switch_day_%d", day.DayIndex),
				Priority:   1,
				IntentCode: "reduce_switch",
				TargetFilter: map[string]any{
					"status": "suggested",
				},
				SlotFilter: map[string]any{
					"day": day.DayIndex,
				},
				CandidateScope: analyzeCandidateScope{
					DayRange:   []int{day.DayIndex},
					Categories: []string{},
					TaskPool:   "placed",
				},
				RequiredReads:       []string{"query_range", "query_target_tasks"},
				SuccessCriteria:     map[string]any{"switch_count<": 5},
				CandidateWriteTools: []string{"swap", "move"},
			})
		} else if day.SwitchCount >= 3 || day.Fragmentation >= 0.55 {
			issues = append(issues, analyzeIssueItem{
				IssueID:   fmt.Sprintf("issue_rhythm_switch_warn_day_%d", day.DayIndex),
				Dimension: "rhythm",
				Severity:  analyzeSeverityWarning,
			})
		}

		if day.HeavyAdjacent {
			issues = append(issues, analyzeIssueItem{
				IssueID:   fmt.Sprintf("issue_rhythm_heavy_adjacent_day_%d", day.DayIndex),
				Dimension: "rhythm",
				Severity:  analyzeSeverityWarning,
			})
			actions = append(actions, analyzeNextAction{
				ActionID:   fmt.Sprintf("na_rhythm_reorder_day_%d", day.DayIndex),
				Priority:   2,
				IntentCode: "smooth_rhythm",
				TargetFilter: map[string]any{
					"status": "suggested",
				},
				SlotFilter: map[string]any{
					"day": day.DayIndex,
				},
				CandidateScope: analyzeCandidateScope{
					DayRange:   []int{day.DayIndex},
					Categories: []string{},
					TaskPool:   "placed",
				},
				RequiredReads:       []string{"query_range", "query_target_tasks"},
				SuccessCriteria:     map[string]any{"heavy_adjacent": false},
				CandidateWriteTools: []string{"swap", "move"},
			})
		}

		if day.MaxBlock >= 5 {
			issues = append(issues, analyzeIssueItem{
				IssueID:   fmt.Sprintf("issue_rhythm_long_block_day_%d", day.DayIndex),
				Dimension: "rhythm",
				Severity:  analyzeSeverityWarning,
			})
		}
	}
	if len(issues) == 0 {
		issues = append(issues, analyzeIssueItem{
			IssueID:   "issue_rhythm_info",
			Dimension: "rhythm",
			Severity:  analyzeSeverityInfo,
		})
	}
	return issues, actions
}

func computeAnalyzeSlackMetrics(state *ScheduleState) analyzeSlackMetrics {
	metrics := analyzeSlackMetrics{AdjustabilityLevel: "low"}
	if state == nil {
		return metrics
	}
	suggested := collectSuggestedTaskItems(state)
	if len(suggested) == 0 {
		return metrics
	}

	totalAlternatives := 0
	for _, task := range suggested {
		alternatives := countAlternativePlacements(state, task, 6)
		if alternatives > 0 {
			metrics.MovableTaskCount++
			totalAlternatives += alternatives
		} else {
			metrics.RigidTaskCount++
		}
	}
	metrics.AvgAlternativeSlots = safeDivideFloat(float64(totalAlternatives), float64(maxInt(metrics.MovableTaskCount, 1)))
	metrics.CrossClassSwapOptions = countCrossClassSwapOptions(state, suggested, 24)

	switch {
	case metrics.MovableTaskCount >= 3 && metrics.AvgAlternativeSlots >= 2.0:
		metrics.AdjustabilityLevel = "high"
	case metrics.MovableTaskCount >= 1 || metrics.CrossClassSwapOptions > 0:
		metrics.AdjustabilityLevel = "medium"
	default:
		metrics.AdjustabilityLevel = "low"
	}
	metrics.PreferSwap = metrics.AdjustabilityLevel == "low" || metrics.CrossClassSwapOptions > 0
	return metrics
}

// computeAnalyzeTightnessMetrics 评估“当前是否还值得继续优化”。
//
// 设计说明：
// 1. 这里不再问“全窗口理论上还能不能挪”，而是问“在写工具顺序约束下，还剩多少合法候选”；
// 2. 合法性口径直接复用写工具的前驱/后继顺序边界，不再人为限定 day±1；
// 3. forced_heavy_adjacent_days 用来识别“即使有问题，也更像时间窗过紧下的代价”。
func computeAnalyzeTightnessMetrics(state *ScheduleState, rhythm analyzeRhythmMetrics) analyzeTightnessMetrics {
	metrics := analyzeTightnessMetrics{TightnessLevel: "locked"}
	if state == nil {
		return metrics
	}

	// 1. 主动优化只关心“当前问题域附近还有没有低代价修法”，
	//    不能再用全窗口可动任务数去放大“还可以继续折腾”的错觉。
	// 2. 若当前没有明显问题域，则退化为 suggested 全量，保证粗排初次诊断仍有结果。
	// 3. focusDays 会优先取 heavy_adjacent / 高切换 / 长连续块出现的天，并补前后 1 天作为局部缓冲区。
	suggested := filterSuggestedTasksByFocusDays(state, selectProblemFocusDays(rhythm))
	if len(suggested) == 0 {
		suggested = collectSuggestedTaskItems(state)
	}
	if len(suggested) == 0 {
		return metrics
	}

	totalAlternatives := 0
	for _, task := range suggested {
		alternatives := countLocalAlternativePlacements(state, task, 1, 4)
		if alternatives > 0 {
			metrics.LocallyMovableTaskCount++
			totalAlternatives += alternatives
		}
	}
	metrics.AvgLocalAlternativeSlots = safeDivideFloat(
		float64(totalAlternatives),
		float64(maxInt(metrics.LocallyMovableTaskCount, 1)),
	)
	metrics.CrossClassSwapOptions = countCrossClassSwapOptions(state, suggested, 12)
	for _, day := range rhythm.Days {
		if day.HeavyAdjacent && !hasRepairOpportunityOnDay(state, day.DayIndex) {
			metrics.ForcedHeavyAdjacentDays++
		}
	}

	switch {
	case metrics.LocallyMovableTaskCount >= 4 && (metrics.AvgLocalAlternativeSlots >= 2.0 || metrics.CrossClassSwapOptions >= 2):
		metrics.TightnessLevel = "loose"
	case metrics.LocallyMovableTaskCount == 0 && metrics.CrossClassSwapOptions == 0:
		metrics.TightnessLevel = "locked"
	default:
		metrics.TightnessLevel = "tight"
	}
	return metrics
}

func selectProblemFocusDays(rhythm analyzeRhythmMetrics) []int {
	seen := map[int]struct{}{}
	out := make([]int, 0, 12)
	appendDay := func(day int) {
		if day <= 0 {
			return
		}
		if _, ok := seen[day]; ok {
			return
		}
		seen[day] = struct{}{}
		out = append(out, day)
	}

	// 1. 高认知相邻优先级最高，因为这是主动优化当前最关心的认知负荷问题。
	// 2. 其次是高切换高碎片，再其次是超长连续块。
	// 3. 每个问题日都补前后 1 天，让局部 move/swap 的可行空间评估更贴近真实操作面。
	for _, day := range rhythm.Days {
		if day.HeavyAdjacent {
			appendDay(day.DayIndex - 1)
			appendDay(day.DayIndex)
			appendDay(day.DayIndex + 1)
		}
	}
	for _, day := range rhythm.Days {
		if day.SwitchCount >= 5 && day.Fragmentation >= 0.75 {
			appendDay(day.DayIndex - 1)
			appendDay(day.DayIndex)
			appendDay(day.DayIndex + 1)
		}
	}
	for _, day := range rhythm.Days {
		if day.MaxBlock >= 5 {
			appendDay(day.DayIndex - 1)
			appendDay(day.DayIndex)
			appendDay(day.DayIndex + 1)
		}
	}
	sort.Ints(out)
	return out
}

func filterSuggestedTasksByFocusDays(state *ScheduleState, focusDays []int) []ScheduleTask {
	if state == nil || len(focusDays) == 0 {
		return nil
	}
	daySet := make(map[int]struct{}, len(focusDays))
	for _, day := range focusDays {
		if day > 0 {
			daySet[day] = struct{}{}
		}
	}
	out := make([]ScheduleTask, 0)
	for _, task := range collectSuggestedTaskItems(state) {
		if len(task.Slots) == 0 {
			continue
		}
		if _, ok := daySet[task.Slots[0].Day]; !ok {
			continue
		}
		out = append(out, task)
	}
	return out
}

func collectSuggestedTaskItems(state *ScheduleState) []ScheduleTask {
	out := make([]ScheduleTask, 0)
	for _, task := range state.Tasks {
		if !IsSuggestedTask(task) || len(task.Slots) == 0 || task.Source != "task_item" {
			continue
		}
		out = append(out, task)
	}
	return out
}

func countLocalAlternativePlacements(state *ScheduleState, task ScheduleTask, dayRadius int, limit int) int {
	if state == nil || len(task.Slots) == 0 {
		return 0
	}
	_ = dayRadius
	duration := taskDuration(task)
	if duration <= 0 {
		return 0
	}
	count := 0
	for day := 1; day <= state.Window.TotalDays; day++ {
		for _, gap := range findFreeRangesOnDay(state, day) {
			maxStart := gap.slotEnd - duration + 1
			for slotStart := gap.slotStart; slotStart <= maxStart; slotStart++ {
				target := []TaskSlot{{Day: day, SlotStart: slotStart, SlotEnd: slotStart + duration - 1}}
				if sameTaskSlots(task.Slots, target) {
					continue
				}
				if err := validateLocalOrderForSinglePlacement(state, task.StateID, target); err != nil {
					continue
				}
				count++
				if count >= limit {
					return count
				}
			}
		}
	}
	return count
}

func localCandidateDays(totalDays int, currentDay int, dayRadius int) []int {
	if totalDays <= 0 || currentDay <= 0 {
		return nil
	}
	out := make([]int, 0, dayRadius*2+1)
	for day := currentDay - dayRadius; day <= currentDay+dayRadius; day++ {
		if day < 1 || day > totalDays {
			continue
		}
		out = append(out, day)
	}
	return out
}

func hasRepairOpportunityOnDay(state *ScheduleState, dayIndex int) bool {
	if state == nil || dayIndex <= 0 {
		return false
	}
	dayTasks := make([]ScheduleTask, 0)
	for _, task := range collectSuggestedTaskItems(state) {
		if len(task.Slots) == 0 || task.Slots[0].Day != dayIndex {
			continue
		}
		dayTasks = append(dayTasks, task)
		if countLocalAlternativePlacements(state, task, 1, 1) > 0 {
			return true
		}
	}
	return countCrossClassSwapOptions(state, dayTasks, 12) > 0
}

func countAlternativePlacements(state *ScheduleState, task ScheduleTask, limit int) int {
	if state == nil || len(task.Slots) == 0 {
		return 0
	}
	duration := taskDuration(task)
	if duration <= 0 {
		return 0
	}
	count := 0
	for day := 1; day <= state.Window.TotalDays; day++ {
		for _, gap := range findFreeRangesOnDay(state, day) {
			maxStart := gap.slotEnd - duration + 1
			for slotStart := gap.slotStart; slotStart <= maxStart; slotStart++ {
				target := []TaskSlot{{Day: day, SlotStart: slotStart, SlotEnd: slotStart + duration - 1}}
				if sameTaskSlots(task.Slots, target) {
					continue
				}
				if err := validateLocalOrderForSinglePlacement(state, task.StateID, target); err != nil {
					continue
				}
				count++
				if count >= limit {
					return count
				}
			}
		}
	}
	return count
}

func countCrossClassSwapOptions(state *ScheduleState, tasks []ScheduleTask, pairLimit int) int {
	if state == nil || len(tasks) < 2 {
		return 0
	}
	count := 0
	checked := 0
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if tasks[i].TaskClassID <= 0 || tasks[j].TaskClassID <= 0 || tasks[i].TaskClassID == tasks[j].TaskClassID {
				continue
			}
			checked++
			if canSwapTasksForSlack(state, tasks[i], tasks[j]) {
				count++
			}
			if checked >= pairLimit {
				return count
			}
		}
	}
	return count
}

func canSwapTasksForSlack(state *ScheduleState, taskA, taskB ScheduleTask) bool {
	if len(taskA.Slots) == 0 || len(taskB.Slots) == 0 {
		return false
	}
	return validateLocalOrderBatchPlacement(state, map[int][]TaskSlot{
		taskA.StateID: cloneScheduleTaskSlots(taskB.Slots),
		taskB.StateID: cloneScheduleTaskSlots(taskA.Slots),
	}) == nil
}

func sameTaskSlots(left, right []TaskSlot) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func buildSlackIssuesAndActions(metrics analyzeSlackMetrics) ([]analyzeIssueItem, []analyzeNextAction) {
	issues := make([]analyzeIssueItem, 0, 1)
	actions := make([]analyzeNextAction, 0, 1)

	switch metrics.AdjustabilityLevel {
	case "low":
		issues = append(issues, analyzeIssueItem{
			IssueID:   "issue_slack_low",
			Dimension: "slack",
			Severity:  analyzeSeverityInfo,
		})
		if metrics.CrossClassSwapOptions > 0 {
			actions = append(actions, analyzeNextAction{
				ActionID:   "na_slack_prefer_swap",
				Priority:   1,
				IntentCode: "prefer_swap",
				TargetFilter: map[string]any{
					"status":               "suggested",
					"different_task_class": true,
				},
				SlotFilter: map[string]any{},
				CandidateScope: analyzeCandidateScope{
					DayRange:   []int{},
					Categories: []string{},
					TaskPool:   "placed",
				},
				RequiredReads:       []string{"query_range", "query_target_tasks"},
				SuccessCriteria:     map[string]any{"cross_class_swap_options>": 0},
				CandidateWriteTools: []string{"swap"},
			})
		}
	case "medium":
		issues = append(issues, analyzeIssueItem{
			IssueID:   "issue_slack_medium",
			Dimension: "slack",
			Severity:  analyzeSeverityInfo,
		})
	default:
		issues = append(issues, analyzeIssueItem{
			IssueID:   "issue_slack_info",
			Dimension: "slack",
			Severity:  analyzeSeverityInfo,
		})
	}
	return issues, actions
}

func computeSemanticProfileMetrics(subjects []analyzeSubjectItem) analyzeSemanticProfileMetrics {
	metrics := analyzeSemanticProfileMetrics{TotalSubjects: len(subjects)}
	for _, subject := range subjects {
		missing := false
		if strings.TrimSpace(subject.SubjectType) == "" {
			metrics.MissingSubjectTypeCount++
			missing = true
		}
		if strings.TrimSpace(subject.DifficultyLevel) == "" {
			metrics.MissingDifficultyCount++
			missing = true
		}
		if strings.TrimSpace(subject.CognitiveIntensity) == "" {
			metrics.MissingCognitiveCount++
			missing = true
		}
		if missing {
			metrics.MissingCompleteProfileCount++
		}
	}
	return metrics
}

func buildSemanticProfileIssues(metrics analyzeSemanticProfileMetrics) []analyzeIssueItem {
	if metrics.MissingCompleteProfileCount <= 0 {
		return nil
	}
	return []analyzeIssueItem{{
		IssueID:   "issue_semantic_profile_missing",
		Dimension: "semantic_profile",
		Severity:  analyzeSeverityWarning,
		Trigger: &analyzeIssueTrigger{
			Metric:    "missing_complete_profile_count",
			Operator:  ">",
			Threshold: 0,
			Actual:    float64(metrics.MissingCompleteProfileCount),
		},
	}}
}

func buildAnalyzeHealthDecision(
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

	// 1. 只有“高认知相邻”这类当前 P1 真正能靠确定性候选修复的问题，才进入候选枚举。
	// 2. 若所有合法候选都只是平移/无增益/恶化，则直接回到 close，避免把 LLM 逼成苦力工。
	// 3. close 永远保留为兜底选项，让 LLM 可以自然收口，而不是为了完成任务感继续乱挪。
	problem, ok := pickPrimaryHealthProblem(state, snapshot)
	if !ok || problem.Kind != healthProblemHeavyAdjacent || problem.Pair == nil {
		decision.Candidates = []analyzeHealthCandidate{
			buildHealthCloseCandidate("保持当前安排并收口：当前没有可继续处理的候选认知问题。", snapshot, base),
		}
		decision.ShouldContinueOptimize = false
		decision.RecommendedOperation = "close"
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

	beneficial := buildHealthCandidatesForProblem(state, snapshot, problem)
	if len(beneficial) == 0 {
		decision.Candidates = []analyzeHealthCandidate{
			buildHealthCloseCandidate("保持当前安排并收口：当前所有合法 move / swap 都只会平移、无增益或恶化问题。", snapshot, base),
		}
		decision.ShouldContinueOptimize = false
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

	decision.Candidates = append(decision.Candidates, beneficial...)
	decision.Candidates = append(decision.Candidates,
		buildHealthCloseCandidate("如果不想继续挪动，也可以保持当前安排并直接收口。", snapshot, base),
	)
	decision.ShouldContinueOptimize = true
	decision.RecommendedOperation = strings.TrimSpace(beneficial[0].Tool)
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

func pickPrimaryRhythmProblem(
	rhythm analyzeRhythmMetrics,
	tightness analyzeTightnessMetrics,
) (summary string, scope *analyzeProblemScope, operation string, ok bool) {
	type rhythmCandidate struct {
		score      int
		summary    string
		scope      *analyzeProblemScope
		preferSwap bool
	}

	candidates := make([]rhythmCandidate, 0, len(rhythm.Days)*2)
	for _, day := range rhythm.Days {
		if day.HeavyAdjacent && !shouldTreatHeavyAdjacencyAsAcceptable(rhythm, day) {
			score := 300 + day.SwitchCount*8 + int(day.Fragmentation*20)
			candidates = append(candidates, rhythmCandidate{
				score:      score,
				summary:    fmt.Sprintf("第 %d 天存在高认知强度任务相邻，学起来会发紧", day.DayIndex),
				scope:      &analyzeProblemScope{DayRange: []int{day.DayIndex}},
				preferSwap: true,
			})
		}
		if day.SwitchCount >= 5 && day.Fragmentation >= 0.75 {
			score := 220 + day.SwitchCount*10 + int(day.Fragmentation*100)
			candidates = append(candidates, rhythmCandidate{
				score:      score,
				summary:    fmt.Sprintf("第 %d 天切换次数偏多，学习节奏明显发碎", day.DayIndex),
				scope:      &analyzeProblemScope{DayRange: []int{day.DayIndex}},
				preferSwap: false,
			})
		}
		if day.MaxBlock >= 5 {
			score := 140 + day.MaxBlock*10
			candidates = append(candidates, rhythmCandidate{
				score:      score,
				summary:    fmt.Sprintf("第 %d 天连续同科目学习块过长，节奏略显单一", day.DayIndex),
				scope:      &analyzeProblemScope{DayRange: []int{day.DayIndex}},
				preferSwap: false,
			})
		}
	}
	if len(candidates) == 0 {
		return "", nil, "close", false
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		leftDay := 1 << 30
		rightDay := 1 << 30
		if candidates[i].scope != nil && len(candidates[i].scope.DayRange) > 0 {
			leftDay = candidates[i].scope.DayRange[0]
		}
		if candidates[j].scope != nil && len(candidates[j].scope.DayRange) > 0 {
			rightDay = candidates[j].scope.DayRange[0]
		}
		return leftDay < rightDay
	})
	best := candidates[0]
	operation = chooseHealthOperation(tightness, best.preferSwap)
	return best.summary, best.scope, operation, true
}

func chooseHealthOperation(tightness analyzeTightnessMetrics, preferSwap bool) string {
	switch {
	case tightness.TightnessLevel == "locked":
		return "close"
	case preferSwap && tightness.CrossClassSwapOptions > 0:
		return "swap"
	case tightness.LocallyMovableTaskCount > 0:
		return "move"
	case tightness.CrossClassSwapOptions > 0:
		return "swap"
	default:
		return "close"
	}
}

func shouldTreatHeavyAdjacencyAsAcceptable(rhythm analyzeRhythmMetrics, day analyzeContextDay) bool {
	// 1. 若整体切换本来就少、同类型切换占比很高，说明当前节奏更像“同类硬课顺着学”，
	//    这类情况不该因为“高认知相邻”四个字就被反复优化。
	// 2. 这里只做保守放宽：必须同时满足整体平稳 + 当天不碎，才把该问题视为可接受。
	// 3. 这样可以减少“把问题从第 3 天搬到第 2 天”的空转行为。
	return rhythm.Overview.SameTypeTransitionRatio >= 0.80 &&
		rhythm.Overview.AvgSwitchesPerDay <= 1.0 &&
		rhythm.Overview.MaxSwitchCount <= 3 &&
		day.SwitchCount <= 2 &&
		day.Fragmentation <= 0.45
}

func buildHealthImprovementSignal(
	rhythm analyzeRhythmMetrics,
	tightness analyzeTightnessMetrics,
	scope *analyzeProblemScope,
	operation string,
	profile analyzeSemanticProfileMetrics,
	feasibility analyzeFeasibility,
) string {
	// 1. 这里故意不写具体 day_index，避免“问题只是从第 3 天漂到第 2 天”时被误判成有进展。
	// 2. 信号只保留主动优化真正关心的局部形态：问题域大小、可修空间、全局节奏代价。
	// 3. execute 节点会用这个信号判断“连续两轮是否实质停滞”，因此格式要稳定。
	problemDays := 0
	if scope != nil {
		problemDays = len(scope.DayRange)
	}
	return fmt.Sprintf(
		"problem_days=%d|heavy_adjacent_days=%d|max_switch_count=%d|same_type_ratio=%.2f|non_forced_heavy_days=%d|local_moves=%d|swap_options=%d|tightness=%s|operation=%s|missing_profile=%d|capacity_gap=%d",
		problemDays,
		rhythm.Overview.HeavyAdjacentDays,
		rhythm.Overview.MaxSwitchCount,
		rhythm.Overview.SameTypeTransitionRatio,
		maxInt(rhythm.Overview.HeavyAdjacentDays-tightness.ForcedHeavyAdjacentDays, 0),
		tightness.LocallyMovableTaskCount,
		tightness.CrossClassSwapOptions,
		tightness.TightnessLevel,
		strings.TrimSpace(operation),
		profile.MissingCompleteProfileCount,
		feasibility.CapacityGap,
	)
}

func computeHealthFeasibilityV2(state *ScheduleState) analyzeFeasibility {
	required := 0
	feasible := 0
	for _, task := range state.Tasks {
		if IsPendingTask(task) {
			required += maxInt(task.Duration, 0)
		}
	}
	for day := 1; day <= state.Window.TotalDays; day++ {
		for _, gap := range findFreeRangesOnDay(state, day) {
			feasible += gap.slotEnd - gap.slotStart + 1
		}
	}
	capacityGap := required - feasible
	if capacityGap <= 0 {
		return analyzeFeasibility{IsFeasible: true, CapacityGap: 0, ReasonCode: "enough_capacity"}
	}
	return analyzeFeasibility{IsFeasible: false, CapacityGap: capacityGap, ReasonCode: "capacity_insufficient"}
}

func buildContextDaySequenceV2(state *ScheduleState, day int) []string {
	sequence := make([]string, 0)
	for slot := 1; slot <= 12; slot++ {
		category := subjectAtSlotV2(state, day, slot)
		if category == "" {
			continue
		}
		sequence = append(sequence, category)
	}
	return sequence
}

func subjectAtSlotV2(state *ScheduleState, day, slot int) string {
	best := ""
	bestPriority := -1
	for _, task := range state.Tasks {
		if len(task.Slots) == 0 || isCourseScheduleTask(task) {
			continue
		}
		for _, ts := range task.Slots {
			if ts.Day != day || slot < ts.SlotStart || slot > ts.SlotEnd {
				continue
			}
			priority := 1
			if task.Source == "task_item" {
				priority = 2
			}
			if IsSuggestedTask(task) {
				priority = 3
			}
			if priority > bestPriority {
				bestPriority = priority
				best = strings.TrimSpace(task.Category)
			}
		}
	}
	return best
}

func findTaskClassMetaByName(state *ScheduleState, name string) *TaskClassMeta {
	if state == nil {
		return nil
	}
	for i := range state.TaskClasses {
		if strings.TrimSpace(state.TaskClasses[i].Name) == strings.TrimSpace(name) {
			return &state.TaskClasses[i]
		}
	}
	return nil
}

func metaValue(meta *TaskClassMeta, getter func(*TaskClassMeta) string) string {
	if meta == nil || getter == nil {
		return ""
	}
	return strings.TrimSpace(getter(meta))
}

func isHighIntensityMeta(meta TaskClassMeta) bool {
	return strings.EqualFold(strings.TrimSpace(meta.CognitiveIntensity), "high") ||
		strings.EqualFold(strings.TrimSpace(meta.DifficultyLevel), "high")
}

func isHighIntensitySubject(subject analyzeSubjectItem) bool {
	return strings.EqualFold(strings.TrimSpace(subject.CognitiveIntensity), "high") ||
		strings.EqualFold(strings.TrimSpace(subject.DifficultyLevel), "high")
}

func sameSemanticType(left, right string) bool {
	left = strings.TrimSpace(strings.ToLower(left))
	right = strings.TrimSpace(strings.ToLower(right))
	if left == "" || right == "" {
		return false
	}
	return left == right
}

func rhythmMetricSchemaV2() map[string]analyzeMetricSchemaItem {
	return map[string]analyzeMetricSchemaItem{
		"overview.avg_switches_per_day":       {Description: "平均每天切换次数", Unit: "count", Direction: "higher_is_more_switching"},
		"overview.max_switch_count":           {Description: "单日最大切换次数", Unit: "count", Direction: "higher_is_worse"},
		"overview.longest_same_subject_run":   {Description: "单日最长连续同科块长度", Unit: "slots", Direction: "higher_is_more_monotone"},
		"overview.heavy_adjacent_days":        {Description: "存在高强度相邻的天数", Unit: "days", Direction: "higher_is_worse"},
		"overview.long_high_intensity_days":   {Description: "高强度连续过长的天数", Unit: "days", Direction: "higher_is_worse"},
		"overview.same_type_transition_ratio": {Description: "同类型切换占比", Unit: "0-1", Direction: "higher_is_smoother"},
		"days.switch_count":                   {Description: "单日切换次数", Unit: "count", Direction: "higher_is_more_switching"},
		"days.fragmentation":                  {Description: "单日碎片化程度", Unit: "0-1", Direction: "higher_is_more_fragmented"},
		"days.max_block":                      {Description: "单日最长连续块", Unit: "slots", Direction: "higher_is_more_monotone"},
		"days.heavy_adjacent":                 {Description: "该天是否存在高强度相邻", Direction: "true_is_worse"},
	}
}

func healthMetricSchemaV2() map[string]analyzeMetricSchemaItem {
	return map[string]analyzeMetricSchemaItem{
		"rhythm.avg_switches_per_day":       {Description: "平均每天切换次数", Unit: "count", Direction: "higher_is_more_switching"},
		"rhythm.max_switch_count":           {Description: "单日最大切换次数", Unit: "count", Direction: "higher_is_worse"},
		"rhythm.heavy_adjacent_days":        {Description: "存在高强度相邻的天数", Unit: "days", Direction: "higher_is_worse"},
		"rhythm.long_high_intensity_days":   {Description: "高强度连续过长的天数", Unit: "days", Direction: "higher_is_worse"},
		"rhythm.same_type_transition_ratio": {Description: "同类型切换占比", Unit: "0-1", Direction: "higher_is_smoother"},
		"slack.movable_task_count":          {Description: "仍有候选落点的任务数", Unit: "count", Direction: "higher_is_more_adjustable"},
		"slack.cross_class_swap_options":    {Description: "跨任务类可交换机会数", Unit: "count", Direction: "higher_is_more_adjustable"},
		"slack.adjustability_level":         {Description: "当前可调整空间等级", Direction: "high_is_looser"},
		"can_close":                         {Description: "当前是否可收口", Direction: "true_is_ready"},
		"feasibility.is_feasible":           {Description: "当前约束下是否可行", Direction: "true_is_feasible"},
	}
}

func healthMetricSchemaV3() map[string]analyzeMetricSchemaItem {
	return map[string]analyzeMetricSchemaItem{
		"rhythm.avg_switches_per_day":            {Description: "平均每天切换次数", Unit: "count", Direction: "higher_is_more_switching"},
		"rhythm.max_switch_count":                {Description: "单日最大切换次数", Unit: "count", Direction: "higher_is_worse"},
		"rhythm.heavy_adjacent_days":             {Description: "存在高强度相邻的天数", Unit: "days", Direction: "higher_is_worse"},
		"rhythm.long_high_intensity_days":        {Description: "高强度连续过长的天数", Unit: "days", Direction: "higher_is_worse"},
		"rhythm.same_type_transition_ratio":      {Description: "同类型切换占比", Unit: "0-1", Direction: "higher_is_smoother"},
		"slack.movable_task_count":               {Description: "仍有候选落点的任务数", Unit: "count", Direction: "higher_is_more_adjustable"},
		"slack.cross_class_swap_options":         {Description: "跨任务类可交换机会数", Unit: "count", Direction: "higher_is_more_adjustable"},
		"slack.adjustability_level":              {Description: "当前可调整空间等级", Direction: "high_is_looser"},
		"profile.missing_subject_type_count":     {Description: "缺少 subject_type 的科目数", Unit: "count", Direction: "higher_is_worse"},
		"profile.missing_difficulty_count":       {Description: "缺少 difficulty_level 的科目数", Unit: "count", Direction: "higher_is_worse"},
		"profile.missing_cognitive_count":        {Description: "缺少 cognitive_intensity 的科目数", Unit: "count", Direction: "higher_is_worse"},
		"profile.missing_complete_profile_count": {Description: "语义画像不完整的科目数", Unit: "count", Direction: "higher_is_worse"},
		"can_close":                              {Description: "当前是否可收口", Direction: "true_is_ready"},
		"feasibility.is_feasible":                {Description: "当前约束下是否可行", Direction: "true_is_feasible"},
	}
}

func normalizeHealthDimensionsV2(raw []string) []string {
	if len(raw) == 0 {
		return []string{"rhythm"}
	}
	allowed := map[string]struct{}{
		"rhythm": {},
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		key := strings.ToLower(strings.TrimSpace(item))
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func parseAnalyzeStringSlice(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
				out = append(out, strings.TrimSpace(text))
			}
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []string{strings.TrimSpace(typed)}
	default:
		return nil
	}
}

func analyzeSeverityRank(level string) int {
	switch level {
	case analyzeSeverityCritical:
		return 0
	case analyzeSeverityWarning:
		return 1
	default:
		return 2
	}
}

func maxInt(a, b int) int {
	if a >= b {
		return a
	}
	return b
}

func safeDivideFloat(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func deduplicateAndSortActions(actions []analyzeNextAction) []analyzeNextAction {
	if len(actions) == 0 {
		return actions
	}
	seen := make(map[string]struct{}, len(actions))
	out := make([]analyzeNextAction, 0, len(actions))
	for _, action := range actions {
		key := action.IntentCode + "::" + action.ActionID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, action)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Priority == out[j].Priority {
			return out[i].ActionID < out[j].ActionID
		}
		return out[i].Priority < out[j].Priority
	})
	return out
}

func healthMetricSchemaV4() map[string]analyzeMetricSchemaItem {
	return map[string]analyzeMetricSchemaItem{
		"rhythm.block_balance":                   {Description: "认知块平衡度；大于 0 更偏碎，小于 0 更偏连续或偏压缩", Unit: "score", Direction: "positive_is_more_fragmented_negative_is_more_compressed"},
		"rhythm.compressed_run_count":            {Description: "偏连续或偏压缩的天数", Unit: "days", Direction: "higher_is_more_compressed"},
		"rhythm.fragmented_count":                {Description: "偏碎的天数", Unit: "days", Direction: "higher_is_more_fragmented"},
		"rhythm.avg_switches_per_day":            {Description: "平均每天切换次数", Unit: "count", Direction: "higher_is_more_switching"},
		"rhythm.max_switch_count":                {Description: "单日最大切换次数", Unit: "count", Direction: "higher_is_worse"},
		"rhythm.heavy_adjacent_days":             {Description: "存在高认知相邻的天数", Unit: "days", Direction: "higher_is_worse"},
		"rhythm.long_high_intensity_days":        {Description: "高强度连续过长的天数", Unit: "days", Direction: "higher_is_worse"},
		"rhythm.same_type_transition_ratio":      {Description: "同类型切换占比", Unit: "0-1", Direction: "higher_is_smoother"},
		"tightness.locally_movable_task_count":   {Description: "仍有近距离合法调整空间的任务数", Unit: "count", Direction: "higher_is_looser"},
		"tightness.avg_local_alternative_slots":  {Description: "局部候选落点均值", Unit: "count", Direction: "higher_is_looser"},
		"tightness.cross_class_swap_options":     {Description: "局部跨任务类可交换机会数", Unit: "count", Direction: "higher_is_looser"},
		"tightness.forced_heavy_adjacent_days":   {Description: "更像被迫保留的高认知相邻天数", Unit: "days", Direction: "higher_is_more_forced"},
		"tightness.tightness_level":              {Description: "当前优化空间等级", Direction: "loose_to_locked"},
		"profile.missing_subject_type_count":     {Description: "缺少 subject_type 的科目数", Unit: "count", Direction: "higher_is_worse"},
		"profile.missing_difficulty_count":       {Description: "缺少 difficulty_level 的科目数", Unit: "count", Direction: "higher_is_worse"},
		"profile.missing_cognitive_count":        {Description: "缺少 cognitive_intensity 的科目数", Unit: "count", Direction: "higher_is_worse"},
		"profile.missing_complete_profile_count": {Description: "语义画像不完整的科目数", Unit: "count", Direction: "higher_is_worse"},
		"decision.should_continue_optimize":      {Description: "当前是否还值得继续主动优化", Direction: "true_is_continue"},
		"decision.is_forced_imperfection":        {Description: "剩余问题是否更像约束代价", Direction: "true_is_forced"},
		"decision.recommended_operation":         {Description: "推荐优先考虑的动作类型", Direction: "swap_move_close"},
		"can_close":                              {Description: "当前是否可收口", Direction: "true_is_ready"},
		"feasibility.is_feasible":                {Description: "当前约束下是否可行", Direction: "true_is_feasible"},
	}
}

func normalizeHealthDimensionsV3(raw []string) []string {
	if len(raw) == 0 {
		return []string{"rhythm", "tightness", "semantic_profile"}
	}
	allowed := map[string]struct{}{
		"rhythm":           {},
		"tightness":        {},
		"semantic_profile": {},
	}
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, item := range raw {
		key := strings.ToLower(strings.TrimSpace(item))
		if _, ok := allowed[key]; !ok {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	return out
}

func mustEncodeAnalyzeEnvelope(envelope analyzeEnvelope) string {
	raw, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Sprintf(`{"tool":"%s","success":false,"metric_schema":{},"metrics":{},"issues":[],"next_actions":[],"error":"encode analyze result failed","error_code":"encode_failed"}`, envelope.Tool)
	}
	return string(raw)
}

func encodeAnalyzeFailure(tool, code, errText string) string {
	return mustEncodeAnalyzeEnvelope(analyzeEnvelope{
		Tool:         tool,
		Success:      false,
		MetricSchema: map[string]analyzeMetricSchemaItem{},
		Metrics:      map[string]any{},
		Issues:       []analyzeIssueItem{},
		NextActions:  []analyzeNextAction{},
		Error:        errText,
		ErrorCode:    code,
	})
}
