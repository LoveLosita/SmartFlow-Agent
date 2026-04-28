package web_result

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// 设计说明：
// 1. 本轮只处理 web 工具卡片，按 AGENTS.md 的迁移约束避免同一轮跨多个能力域抽公共 toolview 层。
// 2. 因此这里先在 web_result 包内保留最小公共 helper，保证 web_search / web_fetch 先完成切流。
// 3. 若后续 taskclass / context 也出现同类卡片 helper，再由主代理统一评估是否下沉成公共层。

// BuildResultView 统一封装 web 结果卡片结构。
//
// 职责边界：
// 1. 负责把已经计算好的折叠态、展开态内容组装成标准视图。
// 2. 负责在子包内补齐 status / status_label，避免依赖父包状态常量。
// 3. 不负责 ToolExecutionResult 外层协议，也不改写 observation 原文。
func BuildResultView(input BuildResultViewInput) ResultView {
	status := normalizeStatus(input.Status)
	if status == "" {
		status = StatusDone
	}

	collapsed := CollapsedView{
		Title:       input.Title,
		Subtitle:    input.Subtitle,
		Status:      status,
		StatusLabel: resolveStatusLabelCN(status),
		Metrics:     appendMetricCopy(input.Metrics),
	}
	expanded := ExpandedView{
		Items:          appendItemCopy(input.Items),
		Sections:       cloneSectionList(input.Sections),
		RawText:        input.Observation,
		MachinePayload: cloneAnyMap(input.MachinePayload),
	}

	return ResultView{
		ViewType:  normalizeViewType(input.ViewType),
		Version:   ViewVersionResult,
		Collapsed: collapsed.Map(),
		Expanded:  expanded.Map(),
	}
}

func BuildMetric(label string, value string) MetricField {
	return MetricField{
		Label: strings.TrimSpace(label),
		Value: strings.TrimSpace(value),
	}
}

func BuildKVField(label string, value string) KVField {
	return KVField{
		Label: strings.TrimSpace(label),
		Value: strings.TrimSpace(value),
	}
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

func BuildItemsSection(title string, items []ItemView) map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, item.Map())
	}
	return map[string]any{
		"type":  "items",
		"title": strings.TrimSpace(title),
		"items": rows,
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

func BuildArgsSection(title string, fields []KVField) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	valid := make([]KVField, 0, len(fields))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		valid = append(valid, BuildKVField(label, value))
	}
	if len(valid) == 0 {
		return nil
	}
	return BuildKVSection(title, valid)
}

func appendSectionIfPresent(target *[]map[string]any, section map[string]any) {
	if section == nil {
		return
	}
	*target = append(*target, section)
}

func appendMetricCopy(metrics []MetricField) []MetricField {
	if len(metrics) == 0 {
		return make([]MetricField, 0)
	}
	out := make([]MetricField, 0, len(metrics))
	for _, metric := range metrics {
		label := strings.TrimSpace(metric.Label)
		value := strings.TrimSpace(metric.Value)
		if label == "" || value == "" {
			continue
		}
		out = append(out, MetricField{Label: label, Value: value})
	}
	if len(out) == 0 {
		return make([]MetricField, 0)
	}
	return out
}

func appendItemCopy(items []ItemView) []ItemView {
	if len(items) == 0 {
		return make([]ItemView, 0)
	}
	out := make([]ItemView, 0, len(items))
	for _, item := range items {
		out = append(out, BuildItem(item.Title, item.Subtitle, item.Tags, item.DetailLines, item.Meta))
	}
	return out
}

func normalizeViewType(viewType string) string {
	switch strings.TrimSpace(viewType) {
	case ViewTypeFetchResult:
		return ViewTypeFetchResult
	case ViewTypeSearchResult:
		return ViewTypeSearchResult
	default:
		return ViewTypeSearchResult
	}
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
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneAnyMap(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, cloneAnyValue(item))
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

func firstString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := readString(input, key); value != "" {
			return value
		}
	}
	return ""
}

func readString(input map[string]any, key string) string {
	if len(input) == 0 {
		return ""
	}
	value, exists := input[key]
	if !exists || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		text := strings.TrimSpace(fmt.Sprintf("%v", typed))
		if text == "" || text == "<nil>" {
			return ""
		}
		return text
	}
}

func readBool(input map[string]any, key string) (bool, bool) {
	if len(input) == 0 {
		return false, false
	}
	value, exists := input[key]
	if !exists {
		return false, false
	}
	typed, ok := value.(bool)
	return typed, ok
}

func readInt(input map[string]any, key string) int {
	if len(input) == 0 {
		return 0
	}
	value, exists := input[key]
	if !exists || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func previewText(text string, limit int) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func previewLines(text string, maxLines int, maxChars int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return make([]string, 0)
	}

	lines := strings.Split(trimmed, "\n")
	out := make([]string, 0, maxLines)
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, previewText(line, maxChars))
		if maxLines > 0 && len(out) >= maxLines {
			break
		}
	}
	if len(out) == 0 {
		out = append(out, previewText(trimmed, maxChars))
	}
	return out
}

func formatStringSliceCN(items []string, limit int) string {
	normalized := normalizeStringSlice(items)
	if len(normalized) == 0 {
		return ""
	}
	if limit <= 0 || len(normalized) <= limit {
		return strings.Join(normalized, "、")
	}
	return fmt.Sprintf("%s 等 %d 个", strings.Join(normalized[:limit], "、"), len(normalized))
}

func formatBoolCN(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func classifyUnavailableStatus(message string) string {
	trimmed := strings.TrimSpace(message)
	lower := strings.ToLower(trimmed)
	switch {
	case strings.Contains(trimmed, "暂未启用"),
		strings.Contains(trimmed, "未启用"),
		strings.Contains(trimmed, "暂未初始化"),
		strings.Contains(trimmed, "未初始化"),
		strings.Contains(trimmed, "未配置"),
		strings.Contains(lower, "not enabled"),
		strings.Contains(lower, "not configured"),
		strings.Contains(lower, "unavailable"):
		return StatusBlocked
	default:
		return StatusFailed
	}
}

func buildRawPreviewSection(rawText string) map[string]any {
	preview := previewText(rawText, 160)
	if preview == "" {
		return nil
	}
	return BuildCalloutSection("原始结果预览", preview, "info", previewLines(rawText, 3, 120))
}

func hostnameFromURL(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}
