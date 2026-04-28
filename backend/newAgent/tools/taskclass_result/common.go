package taskclass_result

import (
	"fmt"
	"strings"
)

// 说明：
// 1. schedule_read / schedule_analysis 已经各自带有一套卡片 helper；
// 2. 这一轮只迁 taskclass 写入结果，如果现在强行把前三批 helper 回抽成公共层，会扩大回归面；
// 3. 因此本包只保留 taskclass.write_result 所需的最小 helper，待非 schedule 主链稳定后再统一评估抽象。

func buildWriteResultView(
	status string,
	title string,
	subtitle string,
	metrics []MetricField,
	items []ItemView,
	sections []map[string]any,
	observation string,
	machinePayload map[string]any,
) WriteResultView {
	normalizedStatus := normalizeStatus(status)
	if normalizedStatus == "" {
		normalizedStatus = StatusDone
	}

	collapsed := map[string]any{
		"title":        strings.TrimSpace(title),
		"subtitle":     strings.TrimSpace(subtitle),
		"status":       normalizedStatus,
		"status_label": resolveStatusLabelCN(normalizedStatus),
		"metrics":      metricListToMaps(metrics),
	}
	expanded := map[string]any{
		"items":    itemListToMaps(items),
		"sections": cloneSectionList(sections),
		"raw_text": observation,
	}
	if len(machinePayload) > 0 {
		expanded["machine_payload"] = cloneAnyMap(machinePayload)
	}

	return WriteResultView{
		ViewType:  ViewTypeWriteResult,
		Version:   ViewVersionWriteResult,
		Collapsed: collapsed,
		Expanded:  expanded,
	}
}

func buildMetric(label string, value string) MetricField {
	return MetricField{
		Label: strings.TrimSpace(label),
		Value: strings.TrimSpace(value),
	}
}

func buildKVField(label string, value string) KVField {
	return KVField{
		Label: strings.TrimSpace(label),
		Value: strings.TrimSpace(value),
	}
}

func buildItem(title string, subtitle string, tags []string, detailLines []string, meta map[string]any) ItemView {
	return ItemView{
		Title:       strings.TrimSpace(title),
		Subtitle:    strings.TrimSpace(subtitle),
		Tags:        normalizeStringSlice(tags),
		DetailLines: normalizeStringSlice(detailLines),
		Meta:        cloneAnyMap(meta),
	}
}

func buildItemsSection(title string, items []ItemView) map[string]any {
	return map[string]any{
		"type":  "items",
		"title": strings.TrimSpace(title),
		"items": itemListToMaps(items),
	}
}

func buildKVSection(title string, fields []KVField) map[string]any {
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

func buildCalloutSection(title string, subtitle string, tone string, detailLines []string) map[string]any {
	return map[string]any{
		"type":         "callout",
		"title":        strings.TrimSpace(title),
		"subtitle":     strings.TrimSpace(subtitle),
		"tone":         strings.TrimSpace(tone),
		"detail_lines": normalizeStringSlice(detailLines),
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

func formatSourceCN(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "chat":
		return "对话"
	case "memory":
		return "记忆"
	case "web":
		return "网页"
	case "":
		return "未标注"
	default:
		return strings.TrimSpace(source)
	}
}

func formatModeCN(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "自动排布"
	case "manual":
		return "手动维护"
	default:
		return fallbackText(mode, "未标注")
	}
}

func formatStrategyCN(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case "steady":
		return "稳态推进"
	case "rapid":
		return "快速推进"
	default:
		return fallbackText(strategy, "未标注")
	}
}

func formatSubjectTypeCN(subjectType string) string {
	switch strings.ToLower(strings.TrimSpace(subjectType)) {
	case "quantitative":
		return "计算型"
	case "memory":
		return "记忆型"
	case "reading":
		return "阅读型"
	case "mixed":
		return "混合型"
	default:
		return fallbackText(subjectType, "未标注")
	}
}

func formatLevelCN(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low":
		return "低"
	case "medium":
		return "中"
	case "high":
		return "高"
	default:
		return fallbackText(level, "未标注")
	}
}

func formatBoolCN(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func formatDateRangeCN(start string, end string) string {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	switch {
	case start != "" && end != "":
		return fmt.Sprintf("%s 至 %s", start, end)
	case start != "":
		return start
	case end != "":
		return end
	default:
		return "未标注"
	}
}

func formatIntListCN(values []int, emptyText string, formatFn func(int) string) string {
	if len(values) == 0 {
		return strings.TrimSpace(emptyText)
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, formatFn(value))
	}
	return strings.Join(parts, "、")
}

func formatWeekdayCN(day int) string {
	switch day {
	case 1:
		return "周一"
	case 2:
		return "周二"
	case 3:
		return "周三"
	case 4:
		return "周四"
	case 5:
		return "周五"
	case 6:
		return "周六"
	case 7:
		return "周日"
	default:
		return fmt.Sprintf("星期%d", day)
	}
}

func formatEmbeddedTimeCN(item TaskClassItemSummary) string {
	if item.EmbeddedWeek <= 0 || item.EmbeddedDay <= 0 || item.EmbeddedSectionFrom <= 0 || item.EmbeddedSectionTo <= 0 {
		return "未指定"
	}
	return fmt.Sprintf(
		"第%d周 %s 第%d-%d节",
		item.EmbeddedWeek,
		formatWeekdayCN(item.EmbeddedDay),
		item.EmbeddedSectionFrom,
		item.EmbeddedSectionTo,
	)
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

func truncateText(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) == 0 {
		return "未填写内容"
	}
	if limit <= 0 || len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func fallbackText(text string, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(text)
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
		out = append(out, map[string]any{
			"label": label,
			"value": value,
		})
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
	case []int:
		out := make([]int, len(typed))
		copy(out, typed)
		return out
	default:
		return typed
	}
}
