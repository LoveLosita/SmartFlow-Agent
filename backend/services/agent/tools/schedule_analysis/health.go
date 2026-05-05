package schedule_analysis

import (
	"fmt"
	"strings"
)

// BuildAnalyzeHealthView 把 analyze_health 的原始 JSON observation 转成诊断卡片。
//
// 步骤化说明：
// 1. 只解析 observation 的现有 JSON 字段，不改变字段名、层级或内容；
// 2. 展示层优先读取 feasibility / decision / metrics，避免依赖自然语言摘要；
// 3. 解析失败或 success=false 时返回失败卡片，raw_text 仍保留原始 observation。
func BuildAnalyzeHealthView(input AnalyzeHealthViewInput) AnalysisResultView {
	payload, ok := parseObservationJSON(input.Observation)
	if !ok || !isSuccessPayload(payload) {
		return BuildFailureView(BuildFailureViewInput{
			ToolName:    "analyze_health",
			Observation: input.Observation,
			ArgFields:   input.ArgFields,
		})
	}

	metricsMap := readMap(payload, "metrics")
	rhythm := readMap(metricsMap, "rhythm")
	tightness := readMap(metricsMap, "tightness")
	profile := readMap(metricsMap, "profile")
	feasibility := readMap(payload, "feasibility")
	decision := readMap(payload, "decision")

	title := buildHealthTitle(feasibility, decision)
	subtitle := buildHealthSubtitle(feasibility, decision)
	metrics := buildHealthMetrics(rhythm, tightness, profile, feasibility)
	candidateItems := buildHealthCandidateItems(decision)
	issueItems := buildIssueItems(readList(payload, "issues"))

	sections := []map[string]any{
		BuildKVSection("裁决结论", buildHealthDecisionFields(feasibility, decision, metricsMap)),
		BuildKVSection("关键指标", buildHealthMetricFields(rhythm, tightness, profile, metricsMap)),
	}
	if len(issueItems) > 0 {
		sections = append(sections, BuildItemsSection("问题清单", issueItems))
	} else {
		sections = append(sections, BuildCalloutSection("问题清单", "当前没有结构化问题项。", "info", nil))
	}
	if len(candidateItems) > 0 {
		sections = append(sections, BuildItemsSection("候选操作", candidateItems))
	} else {
		sections = append(sections, BuildCalloutSection("候选操作", "当前没有可执行候选。", "info", nil))
	}
	sections = append(sections, buildHealthNextStepSection(feasibility, decision, candidateItems))
	appendSectionIfPresent(&sections, BuildArgsSection("分析参数", input.ArgFields))

	return BuildResultView(BuildResultViewInput{
		Status:         StatusDone,
		Title:          title,
		Subtitle:       subtitle,
		Metrics:        metrics,
		Items:          candidateItems,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: payload,
	})
}

func buildHealthTitle(feasibility map[string]any, decision map[string]any) string {
	if feasible, ok := readBool(feasibility, "is_feasible"); ok && !feasible {
		return "综合体检：当前约束不可行"
	}
	if shouldContinue, ok := readBool(decision, "should_continue_optimize"); ok && shouldContinue {
		return "综合体检：建议继续微调"
	}
	return "综合体检：可以收口"
}

func buildHealthSubtitle(feasibility map[string]any, decision map[string]any) string {
	if feasible, ok := readBool(feasibility, "is_feasible"); ok && !feasible {
		gap := readInt(feasibility, "capacity_gap")
		reason := readString(feasibility, "reason_code")
		if reason == "" {
			reason = "capacity_insufficient"
		}
		return fmt.Sprintf("容量仍缺 %d 节，原因：%s。", gap, reason)
	}
	if problem := readString(decision, "primary_problem"); problem != "" {
		return problem
	}
	return "当前没有发现需要继续处理的结构化问题。"
}

func buildHealthMetrics(rhythm, tightness, profile, feasibility map[string]any) []MetricField {
	metrics := []MetricField{
		BuildMetric("高认知相邻", fmt.Sprintf("%d 天", readInt(rhythm, "heavy_adjacent_days"))),
		BuildMetric("最大切换", fmt.Sprintf("%d 次", readInt(rhythm, "max_switch_count"))),
		BuildMetric("可局部移动", fmt.Sprintf("%d 项", readInt(tightness, "locally_movable_task_count"))),
		BuildMetric("紧度", fallbackLabel(readString(tightness, "tightness_level"), "未标注")),
	}
	if gap := readInt(feasibility, "capacity_gap"); gap > 0 {
		metrics = append(metrics, BuildMetric("容量缺口", fmt.Sprintf("%d 节", gap)))
		return metrics
	}
	if missing := readInt(profile, "missing_complete_profile_count"); missing > 0 {
		metrics = append(metrics, BuildMetric("画像缺失", fmt.Sprintf("%d 门", missing)))
	}
	return metrics
}

func buildHealthDecisionFields(feasibility map[string]any, decision map[string]any, metrics map[string]any) []KVField {
	shouldContinue, _ := readBool(decision, "should_continue_optimize")
	forced, _ := readBool(decision, "is_forced_imperfection")
	canClose, _ := readBool(metrics, "can_close")
	feasible, feasibleOK := readBool(feasibility, "is_feasible")
	feasibleText := "未返回"
	if feasibleOK {
		feasibleText = formatBoolCN(feasible)
	}

	return []KVField{
		BuildKVField("是否继续优化", formatBoolCN(shouldContinue)),
		BuildKVField("当前可收口", formatBoolCN(canClose)),
		BuildKVField("推荐动作", formatOperationCN(readString(decision, "recommended_operation"))),
		BuildKVField("主问题", fallbackLabel(readString(decision, "primary_problem"), "当前没有发现值得继续处理的局部认知问题")),
		BuildKVField("约束代价", formatBoolCN(forced)),
		BuildKVField("约束可行", feasibleText),
		BuildKVField("容量缺口", fmt.Sprintf("%d 节", readInt(feasibility, "capacity_gap"))),
		BuildKVField("可行性原因", fallbackLabel(readString(feasibility, "reason_code"), "未返回")),
	}
}

func buildHealthMetricFields(rhythm, tightness, profile, metrics map[string]any) []KVField {
	canClose, _ := readBool(metrics, "can_close")
	return []KVField{
		BuildKVField("认知块平衡", fmt.Sprintf("%d", readInt(rhythm, "block_balance"))),
		BuildKVField("偏碎天数", fmt.Sprintf("%d 天", readInt(rhythm, "fragmented_count"))),
		BuildKVField("偏压缩天数", fmt.Sprintf("%d 天", readInt(rhythm, "compressed_run_count"))),
		BuildKVField("平均每日切换", fmt.Sprintf("%.1f 次", readFloat(rhythm, "avg_switches_per_day"))),
		BuildKVField("同类型切换占比", formatPercent(readFloat(rhythm, "same_type_transition_ratio"))),
		BuildKVField("局部候选均值", fmt.Sprintf("%.1f 个", readFloat(tightness, "avg_local_alternative_slots"))),
		BuildKVField("跨任务类交换机会", fmt.Sprintf("%d 个", readInt(tightness, "cross_class_swap_options"))),
		BuildKVField("被迫高认知相邻", fmt.Sprintf("%d 天", readInt(tightness, "forced_heavy_adjacent_days"))),
		BuildKVField("语义画像缺失", fmt.Sprintf("%d 门", readInt(profile, "missing_complete_profile_count"))),
		BuildKVField("当前可收口", formatBoolCN(canClose)),
	}
}

func buildHealthCandidateItems(decision map[string]any) []ItemView {
	candidates := readList(decision, "candidates")
	if len(candidates) == 0 {
		return make([]ItemView, 0)
	}
	items := make([]ItemView, 0, len(candidates))
	for _, raw := range candidates {
		candidate, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		after := readMap(candidate, "after")
		canClose, _ := readBool(after, "can_close")
		tool := readString(candidate, "tool")
		effect := readString(candidate, "effect")
		title := readString(candidate, "summary")
		if title == "" {
			title = fallbackLabel(readString(candidate, "candidate_id"), "候选操作")
		}
		subtitle := readString(after, "primary_problem")
		if subtitle == "" {
			subtitle = fmt.Sprintf("效果：%s", formatEffectCN(effect))
		}

		tags := []string{formatOperationCN(tool), formatEffectCN(effect)}
		if canClose {
			tags = append(tags, "执行后可收口")
		}
		detailLines := []string{
			"候选 ID：" + fallbackLabel(readString(candidate, "candidate_id"), "未返回"),
			"参数：" + compactJSON(candidate["arguments"]),
			fmt.Sprintf("执行后高认知相邻：%d 天", readInt(after, "heavy_adjacent_days")),
			fmt.Sprintf("执行后最大切换：%d 次", readInt(after, "max_switch_count")),
			"执行后同类型切换占比：" + formatPercent(readFloat(after, "same_type_transition_ratio")),
		}
		items = append(items, BuildItem(title, subtitle, tags, detailLines, candidate))
	}
	return items
}

func buildIssueItems(rows []any) []ItemView {
	if len(rows) == 0 {
		return make([]ItemView, 0)
	}
	items := make([]ItemView, 0, len(rows))
	for _, raw := range rows {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		trigger := readMap(issue, "trigger")
		severity := readString(issue, "severity")
		dimension := readString(issue, "dimension")
		title := describeIssue(issue)
		detailLines := make([]string, 0, 3)
		if metric := readString(trigger, "metric"); metric != "" {
			detailLines = append(detailLines, fmt.Sprintf("触发指标：%s %s %.2f，实际 %.2f", metric, readString(trigger, "operator"), readFloat(trigger, "threshold"), readFloat(trigger, "actual")))
		}
		detailLines = append(detailLines, "问题 ID："+fallbackLabel(readString(issue, "issue_id"), "未返回"))
		items = append(items, BuildItem(
			title,
			fmt.Sprintf("%s，%s", fallbackLabel(dimension, "未标注维度"), formatSeverityCN(severity)),
			[]string{formatSeverityCN(severity), fallbackLabel(dimension, "未标注维度")},
			detailLines,
			issue,
		))
	}
	return items
}

func buildHealthNextStepSection(feasibility map[string]any, decision map[string]any, candidateItems []ItemView) map[string]any {
	if feasible, ok := readBool(feasibility, "is_feasible"); ok && !feasible {
		return BuildCalloutSection(
			"建议后续动作",
			"当前先不要继续写操作，应先与用户协商时间窗、约束或任务范围。",
			"warning",
			[]string{"可选方向：扩展时间窗、放宽排除约束、缩减任务量，或确认接受风险收口。"},
		)
	}
	if shouldContinue, ok := readBool(decision, "should_continue_optimize"); ok && shouldContinue {
		return BuildCalloutSection(
			"建议后续动作",
			"优先从候选操作里选择收益明确的一项执行。",
			"info",
			[]string{fmt.Sprintf("当前共有 %d 个候选项；执行后建议再次调用 analyze_health 复诊。", len(candidateItems))},
		)
	}
	return BuildCalloutSection(
		"建议后续动作",
		"当前可以收口；如用户仍要求微调，再按具体偏好追加读取或局部调整。",
		"info",
		nil,
	)
}

func describeIssue(issue map[string]any) string {
	issueID := readString(issue, "issue_id")
	dimension := readString(issue, "dimension")
	switch {
	case strings.Contains(issueID, "feasibility"):
		return "容量可行性不足"
	case strings.Contains(issueID, "semantic_profile"):
		return "任务类语义画像不完整"
	case strings.Contains(issueID, "heavy_adjacent"):
		return "存在高认知任务相邻"
	case strings.Contains(issueID, "switch"):
		return "单日任务切换偏多"
	case strings.Contains(issueID, "long_block"):
		return "同类任务连续块偏长"
	case strings.Contains(issueID, "info"):
		return "节奏整体提示"
	default:
		return fallbackLabel(dimension, "诊断问题")
	}
}

func fallbackLabel(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(value)
}
