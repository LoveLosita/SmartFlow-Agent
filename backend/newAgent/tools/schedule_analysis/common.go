package schedule_analysis

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BuildResultView 统一封装 schedule.analysis_result 结构。
//
// 职责边界：
// 1. 负责把已经计算好的折叠态/展开态内容组装成标准视图；
// 2. 负责在子包内补齐 status / status_label，避免依赖父包常量；
// 3. 不负责 ToolExecutionResult 外层协议，也不改写 observation 原文。
func BuildResultView(input BuildResultViewInput) AnalysisResultView {
	status := normalizeStatus(input.Status)
	if status == "" {
		status = StatusDone
	}

	collapsed := map[string]any{
		"title":        strings.TrimSpace(input.Title),
		"subtitle":     strings.TrimSpace(input.Subtitle),
		"status":       status,
		"status_label": resolveStatusLabelCN(status),
		"metrics":      metricListToMaps(input.Metrics),
	}
	expanded := map[string]any{
		"items":    itemListToMaps(input.Items),
		"sections": cloneSectionList(input.Sections),
		"raw_text": input.Observation,
	}
	if len(input.MachinePayload) > 0 {
		expanded["machine_payload"] = cloneAnyMap(input.MachinePayload)
	}

	return AnalysisResultView{
		ViewType:  ViewTypeAnalysisResult,
		Version:   ViewVersionAnalysisResult,
		Collapsed: collapsed,
		Expanded:  expanded,
	}
}

// BuildFailureView 统一生成 analysis 工具失败卡片视图。
//
// 职责边界：
// 1. 只从 observation 中提炼失败文案和参数回显；
// 2. 不负责判断失败条件，调用方需要先确认 observation 失败；
// 3. raw_text 仍保留原始 observation，方便 debug 与下游排查。
func BuildFailureView(input BuildFailureViewInput) AnalysisResultView {
	status := normalizeStatus(input.Status)
	if status == "" {
		status = StatusFailed
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = fmt.Sprintf("%s失败", resolveToolLabelCN(input.ToolName))
	}
	subtitle := strings.TrimSpace(input.Subtitle)
	if subtitle == "" {
		subtitle = failureText(input.Observation, "诊断分析失败，请检查当前日程状态后重试。")
	}

	sections := []map[string]any{
		BuildCalloutSection("执行失败", subtitle, "danger", []string{subtitle}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("分析参数", input.ArgFields))
	return BuildResultView(BuildResultViewInput{
		Status:      status,
		Title:       title,
		Subtitle:    subtitle,
		Sections:    sections,
		Observation: input.Observation,
	})
}

func BuildMetric(label string, value string) MetricField {
	return MetricField{Label: strings.TrimSpace(label), Value: strings.TrimSpace(value)}
}

func BuildKVField(label string, value string) KVField {
	return KVField{Label: strings.TrimSpace(label), Value: strings.TrimSpace(value)}
}

func BuildItem(title string, subtitle string, tags []string, detailLines []string, meta map[string]any) ItemView {
	return ItemView{
		Title:       strings.TrimSpace(title),
		Subtitle:    strings.TrimSpace(subtitle),
		Tags:        normalizeStringSlice(tags),
		DetailLines: normalizeStringSlice(detailLines),
		Meta:        cloneAnyMap(meta),
	}
}

func BuildItemsSection(title string, items []ItemView) map[string]any {
	return map[string]any{
		"type":  "items",
		"title": strings.TrimSpace(title),
		"items": itemListToMaps(items),
	}
}

func BuildKVSection(title string, fields []KVField) map[string]any {
	rows := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		rows = append(rows, map[string]any{
			"label": label,
			"value": value,
		})
	}
	return map[string]any{
		"type":   "kv",
		"title":  strings.TrimSpace(title),
		"fields": rows,
	}
}

func BuildCalloutSection(title string, subtitle string, tone string, detailLines []string) map[string]any {
	return map[string]any{
		"type":         "callout",
		"title":        strings.TrimSpace(title),
		"subtitle":     strings.TrimSpace(subtitle),
		"tone":         strings.TrimSpace(tone),
		"detail_lines": normalizeStringSlice(detailLines),
	}
}

// BuildArgsSection 把父包已经本地化的参数字段拼成展示 section。
//
// 职责边界：
// 1. 只接受纯 KVField，不依赖父包 ToolArgumentView；
// 2. 不解释 detail / threshold / hard_categories 是否真实参与计算；
// 3. 没有有效字段时返回 nil，避免空 section 干扰前端。
func BuildArgsSection(title string, fields []KVField) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	valid := make([]KVField, 0, len(fields))
	for _, field := range fields {
		if strings.TrimSpace(field.Label) == "" || strings.TrimSpace(field.Value) == "" {
			continue
		}
		valid = append(valid, BuildKVField(field.Label, field.Value))
	}
	if len(valid) == 0 {
		return nil
	}
	return BuildKVSection(title, valid)
}

func parseObservationJSON(observation string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(observation)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil, false
	}
	return payload, true
}

func isSuccessPayload(payload map[string]any) bool {
	success, ok := readBool(payload, "success")
	return ok && success
}

func failureText(observation string, fallback string) string {
	if payload, ok := parseObservationJSON(observation); ok {
		if message := firstString(payload, "error", "message", "reason", "err"); message != "" {
			return message
		}
	}
	if strings.TrimSpace(observation) != "" {
		return strings.TrimSpace(observation)
	}
	return strings.TrimSpace(fallback)
}

func normalizeStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusDone:
		return StatusDone
	case StatusBlocked:
		return StatusBlocked
	case StatusFailed:
		return StatusFailed
	default:
		return ""
	}
}

func resolveStatusLabelCN(status string) string {
	switch normalizeStatus(status) {
	case StatusDone:
		return "已完成"
	case StatusBlocked:
		return "已阻断"
	default:
		return "失败"
	}
}

func resolveToolLabelCN(toolName string) string {
	switch strings.TrimSpace(toolName) {
	case "analyze_health":
		return "综合体检"
	case "analyze_rhythm":
		return "学习节律分析"
	default:
		return "诊断分析"
	}
}

func appendSectionIfPresent(target *[]map[string]any, section map[string]any) {
	if section == nil {
		return
	}
	*target = append(*target, section)
}

func metricListToMaps(metrics []MetricField) []map[string]any {
	if len(metrics) == 0 {
		return make([]map[string]any, 0)
	}
	out := make([]map[string]any, 0, len(metrics))
	for _, metric := range metrics {
		label := strings.TrimSpace(metric.Label)
		value := strings.TrimSpace(metric.Value)
		if label == "" || value == "" {
			continue
		}
		out = append(out, map[string]any{"label": label, "value": value})
	}
	if len(out) == 0 {
		return make([]map[string]any, 0)
	}
	return out
}

func itemListToMaps(items []ItemView) []map[string]any {
	if len(items) == 0 {
		return make([]map[string]any, 0)
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row := map[string]any{
			"title":        strings.TrimSpace(item.Title),
			"subtitle":     strings.TrimSpace(item.Subtitle),
			"tags":         normalizeStringSlice(item.Tags),
			"detail_lines": normalizeStringSlice(item.DetailLines),
		}
		if len(item.Meta) > 0 {
			row["meta"] = cloneAnyMap(item.Meta)
		}
		out = append(out, row)
	}
	return out
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return make([]string, 0)
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) == 0 {
		return make([]string, 0)
	}
	return out
}

func cloneSectionList(sections []map[string]any) []map[string]any {
	if len(sections) == 0 {
		return make([]map[string]any, 0)
	}
	out := make([]map[string]any, 0, len(sections))
	for _, section := range sections {
		out = append(out, cloneAnyMap(section))
	}
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = cloneAnyValue(value)
	}
	return out
}

func cloneAnyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneAnyMap(typed)
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneAnyValue(item))
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneAnyMap(item))
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return typed
	}
}

func readMap(input map[string]any, key string) map[string]any {
	if len(input) == 0 {
		return nil
	}
	value, ok := input[key]
	if !ok {
		return nil
	}
	row, _ := value.(map[string]any)
	return row
}

func readList(input map[string]any, key string) []any {
	if len(input) == 0 {
		return nil
	}
	value, ok := input[key]
	if !ok {
		return nil
	}
	rows, _ := value.([]any)
	return rows
}

func readString(input map[string]any, key string) string {
	if len(input) == 0 {
		return ""
	}
	value, ok := input[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", typed))
		if text == "<nil>" {
			return ""
		}
		return text
	}
}

func firstString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := readString(input, key); value != "" {
			return value
		}
	}
	return ""
}

func readBool(input map[string]any, key string) (bool, bool) {
	if len(input) == 0 {
		return false, false
	}
	value, ok := input[key]
	if !ok {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func readInt(input map[string]any, key string) int {
	value := readFloat(input, key)
	return int(value)
}

func readFloat(input map[string]any, key string) float64 {
	if len(input) == 0 {
		return 0
	}
	value, ok := input[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func severityRank(severity string) int {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return 0
	case "warning":
		return 1
	default:
		return 2
	}
}

func formatSeverityCN(severity string) string {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "critical":
		return "高风险"
	case "warning":
		return "需关注"
	default:
		return "提示"
	}
}

func formatBoolCN(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func formatFloat(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

func formatPercent(value float64) string {
	return fmt.Sprintf("%.0f%%", value*100)
}

func formatOperationCN(operation string) string {
	switch strings.TrimSpace(operation) {
	case "move":
		return "移动"
	case "swap":
		return "交换"
	case "close":
		return "收口"
	case "ask_user":
		return "询问用户"
	default:
		if strings.TrimSpace(operation) == "" {
			return "未指定"
		}
		return strings.TrimSpace(operation)
	}
}

func formatEffectCN(effect string) string {
	switch strings.TrimSpace(effect) {
	case "improve":
		return "明显改善"
	case "partial_improve":
		return "部分改善"
	case "shift":
		return "问题转移"
	case "no_gain":
		return "收益不足"
	case "regress":
		return "变差"
	case "close":
		return "收口"
	default:
		if strings.TrimSpace(effect) == "" {
			return "未标注"
		}
		return strings.TrimSpace(effect)
	}
}

func sortedKeys(input map[string]any) []string {
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(raw)
}
