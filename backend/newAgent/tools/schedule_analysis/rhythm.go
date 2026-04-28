package schedule_analysis

import (
	"fmt"
	"strings"
)

// BuildAnalyzeRhythmView 把 analyze_rhythm 的原始 JSON observation 转成诊断卡片。
//
// 步骤化说明：
// 1. 只读取现有 metrics / issues / next_actions，不改变 observation JSON；
// 2. collapsed 聚焦节律结论和关键指标，expanded 展开问题日、问题清单和建议动作；
// 3. detail / hard_categories 等参数只在父包参数区回显，不在这里声明它们已影响算法。
func BuildAnalyzeRhythmView(input AnalyzeRhythmViewInput) AnalysisResultView {
	payload, ok := parseObservationJSON(input.Observation)
	if !ok || !isSuccessPayload(payload) {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "analyze_rhythm",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	metricsMap := readMap(payload, "metrics")
	overview := readMap(metricsMap, "overview")
	days := readList(metricsMap, "days")
	issues := readList(payload, "issues")
	actions := readList(payload, "next_actions")

	actionItems := buildRhythmActionItems(actions)
	problemDayItems := buildRhythmProblemDayItems(days)
	issueItems := buildIssueItems(issues)

	sections := []map[string]any{
		BuildKVSection("节律概览", buildRhythmOverviewFields(overview)),
	}
	if len(problemDayItems) > 0 {
		sections = append(sections, BuildItemsSection("问题日", problemDayItems))
	} else {
		sections = append(sections, BuildCalloutSection("问题日", "当前没有命中的高风险问题日。", "info", nil))
	}
	if len(issueItems) > 0 {
		sections = append(sections, BuildItemsSection("问题清单", issueItems))
	}
	if len(actionItems) > 0 {
		sections = append(sections, BuildItemsSection("建议动作", actionItems))
	} else {
		sections = append(sections, BuildCalloutSection("建议动作", "当前节律诊断没有返回候选动作。", "info", nil))
	}
	appendSectionIfPresent(&sections, BuildArgsSection("分析参数", input.ArgFields))

	return BuildResultView(BuildResultViewInput{
		Status:         StatusDone,
		Title:          buildRhythmTitle(issues),
		Subtitle:       buildRhythmSubtitle(issues, overview),
		Metrics:        buildRhythmMetrics(overview),
		Items:          actionItems,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: payload,
	})
}

func buildRhythmTitle(issues []any) string {
	severity := highestSeverity(issues)
	switch severity {
	case "critical":
		return "学习节律分析：存在高风险"
	case "warning":
		return "学习节律分析：有待微调"
	default:
		return "学习节律分析：整体平稳"
	}
}

func buildRhythmSubtitle(issues []any, overview map[string]any) string {
	if issue := firstHighPriorityIssue(issues); issue != nil {
		return describeIssue(issue)
	}
	maxDay := readInt(overview, "max_switch_day")
	maxSwitch := readInt(overview, "max_switch_count")
	if maxDay > 0 {
		return fmt.Sprintf("最大切换出现在第 %d 天，共 %d 次。", maxDay, maxSwitch)
	}
	return "当前学习节律没有明显异常信号。"
}

func buildRhythmMetrics(overview map[string]any) []MetricField {
	return []MetricField{
		BuildMetric("平均切换", fmt.Sprintf("%.1f 次/天", readFloat(overview, "avg_switches_per_day"))),
		BuildMetric("最大切换", fmt.Sprintf("%d 次", readInt(overview, "max_switch_count"))),
		BuildMetric("高认知相邻", fmt.Sprintf("%d 天", readInt(overview, "heavy_adjacent_days"))),
		BuildMetric("块平衡", fmt.Sprintf("%d", readInt(overview, "block_balance"))),
		BuildMetric("同类型占比", formatPercent(readFloat(overview, "same_type_transition_ratio"))),
	}
}

func buildRhythmOverviewFields(overview map[string]any) []KVField {
	return []KVField{
		BuildKVField("平均每日切换", fmt.Sprintf("%.1f 次", readFloat(overview, "avg_switches_per_day"))),
		BuildKVField("最大切换日", formatDayIndex(readInt(overview, "max_switch_day"))),
		BuildKVField("最大切换次数", fmt.Sprintf("%d 次", readInt(overview, "max_switch_count"))),
		BuildKVField("平均块长度", fmt.Sprintf("%.1f 节", readFloat(overview, "avg_block_size"))),
		BuildKVField("最长同科连续", fmt.Sprintf("%d 节", readInt(overview, "longest_same_subject_run"))),
		BuildKVField("高认知相邻", fmt.Sprintf("%d 天", readInt(overview, "heavy_adjacent_days"))),
		BuildKVField("高强度连续过长", fmt.Sprintf("%d 天", readInt(overview, "long_high_intensity_days"))),
		BuildKVField("偏碎天数", fmt.Sprintf("%d 天", readInt(overview, "fragmented_count"))),
		BuildKVField("偏压缩天数", fmt.Sprintf("%d 天", readInt(overview, "compressed_run_count"))),
		BuildKVField("同类型切换占比", formatPercent(readFloat(overview, "same_type_transition_ratio"))),
	}
}

func buildRhythmProblemDayItems(days []any) []ItemView {
	if len(days) == 0 {
		return make([]ItemView, 0)
	}
	items := make([]ItemView, 0)
	for _, raw := range days {
		day, ok := raw.(map[string]any)
		if !ok || !isProblemRhythmDay(day) {
			continue
		}
		dayIndex := readInt(day, "day_index")
		switchCount := readInt(day, "switch_count")
		fragmentation := readFloat(day, "fragmentation")
		maxBlock := readInt(day, "max_block")
		heavyAdjacent, _ := readBool(day, "heavy_adjacent")
		tags := []string{}
		if switchCount >= 3 || fragmentation >= 0.55 {
			tags = append(tags, "偏碎")
		}
		if heavyAdjacent {
			tags = append(tags, "高认知相邻")
		}
		if maxBlock >= 5 {
			tags = append(tags, "连续块偏长")
		}
		detailLines := []string{
			fmt.Sprintf("切换次数：%d 次", switchCount),
			"碎片化程度：" + formatFloat(fragmentation),
			fmt.Sprintf("最长连续块：%d 节", maxBlock),
			"科目序列：" + formatSequence(readList(day, "sequence")),
		}
		items = append(items, BuildItem(
			formatDayIndex(dayIndex),
			fmt.Sprintf("切换 %d 次，最长连续 %d 节", switchCount, maxBlock),
			tags,
			detailLines,
			day,
		))
	}
	return items
}

func buildRhythmActionItems(actions []any) []ItemView {
	if len(actions) == 0 {
		return make([]ItemView, 0)
	}
	items := make([]ItemView, 0, len(actions))
	for _, raw := range actions {
		action, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		scope := readMap(action, "candidate_scope")
		title := formatIntentCN(readString(action, "intent_code"))
		if title == "" {
			title = fallbackLabel(readString(action, "action_id"), "建议动作")
		}
		reads := formatStringList(readList(action, "required_reads"))
		writes := formatStringList(readList(action, "candidate_write_tools"))
		tags := []string{
			fmt.Sprintf("优先级 %d", readInt(action, "priority")),
			fallbackLabel(writes, "无写工具"),
		}
		detailLines := []string{
			"需要读取：" + fallbackLabel(reads, "无"),
			"候选写工具：" + fallbackLabel(writes, "无"),
			"作用范围：" + formatCandidateScope(scope),
			"成功标准：" + compactJSON(action["success_criteria"]),
		}
		items = append(items, BuildItem(
			title,
			fmt.Sprintf("先读 %s，再考虑 %s", fallbackLabel(reads, "相关事实"), fallbackLabel(writes, "局部调整")),
			tags,
			detailLines,
			action,
		))
	}
	return items
}

func highestSeverity(issues []any) string {
	best := "info"
	bestRank := severityRank(best)
	for _, raw := range issues {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		severity := readString(issue, "severity")
		if rank := severityRank(severity); rank < bestRank {
			best = severity
			bestRank = rank
		}
	}
	return strings.ToLower(strings.TrimSpace(best))
}

func firstHighPriorityIssue(issues []any) map[string]any {
	var best map[string]any
	bestRank := 99
	for _, raw := range issues {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rank := severityRank(readString(issue, "severity"))
		if best == nil || rank < bestRank {
			best = issue
			bestRank = rank
		}
	}
	return best
}

func isProblemRhythmDay(day map[string]any) bool {
	heavyAdjacent, _ := readBool(day, "heavy_adjacent")
	return readInt(day, "switch_count") >= 3 ||
		readFloat(day, "fragmentation") >= 0.55 ||
		readInt(day, "max_block") >= 5 ||
		heavyAdjacent
}

func formatDayIndex(day int) string {
	if day <= 0 {
		return "未知日期"
	}
	return fmt.Sprintf("第 %d 天", day)
}

func formatSequence(rows []any) string {
	if len(rows) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(rows))
	for _, raw := range rows {
		text := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, " -> ")
}

func formatStringList(rows []any) string {
	if len(rows) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rows))
	for _, raw := range rows {
		text := strings.TrimSpace(fmt.Sprintf("%v", raw))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, "、")
}

func formatCandidateScope(scope map[string]any) string {
	if len(scope) == 0 {
		return "未返回"
	}
	parts := make([]string, 0, 3)
	if days := formatNumberList(readList(scope, "day_range"), "第 %d 天"); days != "" {
		parts = append(parts, "日期："+days)
	}
	if categories := formatStringList(readList(scope, "categories")); categories != "" {
		parts = append(parts, "类别："+categories)
	}
	if pool := readString(scope, "task_pool"); pool != "" {
		parts = append(parts, "任务池："+pool)
	}
	if len(parts) == 0 {
		return compactJSON(scope)
	}
	return strings.Join(parts, "；")
}

func formatNumberList(rows []any, pattern string) string {
	if len(rows) == 0 {
		return ""
	}
	parts := make([]string, 0, len(rows))
	for _, raw := range rows {
		number := 0
		switch typed := raw.(type) {
		case float64:
			number = int(typed)
		case int:
			number = typed
		default:
			continue
		}
		parts = append(parts, fmt.Sprintf(pattern, number))
	}
	return strings.Join(parts, "、")
}

func formatIntentCN(intent string) string {
	switch strings.TrimSpace(intent) {
	case "reduce_switch":
		return "减少同日切换"
	case "smooth_rhythm":
		return "平滑高认知相邻"
	case "prefer_swap":
		return "优先寻找交换机会"
	default:
		return strings.TrimSpace(intent)
	}
}
