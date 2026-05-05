package web_result

import (
	"encoding/json"
	"fmt"
	"strings"
)

type searchObservation struct {
	Tool   string                  `json:"tool"`
	Query  string                  `json:"query"`
	Count  int                     `json:"count"`
	Items  []searchObservationItem `json:"items"`
	Error  string                  `json:"error"`
	Err    string                  `json:"err"`
	Reason string                  `json:"reason"`
}

type searchObservationItem struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Snippet     string `json:"snippet"`
	Domain      string `json:"domain"`
	PublishedAt string `json:"published_at"`
}

// BuildSearchView 负责把 web_search observation 构造成前端可直接消费的结果卡片。
//
// 职责边界：
// 1. 负责解析成功 / 失败 / provider 未启用 / 非 JSON 回退四类场景。
// 2. 负责保留 raw_text 与 machine_payload，方便前端调试与后续交互。
// 3. 不负责执行搜索，也不改写传入 observation 原文。
func BuildSearchView(input SearchViewInput) ResultView {
	payloadMap, ok := parseObservationJSON(input.Observation)
	if !ok {
		return buildSearchTextFallbackView(input)
	}

	payload := searchObservation{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(input.Observation)), &payload); err != nil {
		return buildSearchTextFallbackView(input)
	}

	query := strings.TrimSpace(payload.Query)
	if query == "" {
		query = strings.TrimSpace(input.Query)
	}

	errorMessage := firstNonEmpty(payload.Error, payload.Err, payload.Reason)
	if errorMessage == "" {
		errorMessage = firstString(payloadMap, "message")
	}
	if errorMessage != "" {
		return buildSearchFailureView(input, query, errorMessage, payloadMap)
	}

	items := buildSearchItems(payload.Items)
	count := payload.Count
	if count < len(items) {
		count = len(items)
	}

	title := fmt.Sprintf("找到 %d 条网页结果", count)
	subtitle := buildSearchSubtitle(query)
	sections := make([]map[string]any, 0, 3)
	appendSectionIfPresent(&sections, BuildArgsSection("搜索参数", buildSearchArgFields(input)))
	if len(items) > 0 {
		sections = append(sections, BuildItemsSection("搜索结果", items))
	} else {
		sections = append(sections, BuildCalloutSection(
			"没有命中结果",
			"当前关键词没有返回可展示结果，可以尝试缩短关键词或放宽筛选条件。",
			"info",
			[]string{"建议优先调整关键词，再决定是否继续抓取具体页面。"},
		))
	}

	return BuildResultView(BuildResultViewInput{
		ViewType:       ViewTypeSearchResult,
		Status:         StatusDone,
		Title:          title,
		Subtitle:       subtitle,
		Metrics:        buildSearchMetrics(count, input.DomainAllow, input.RecencyDays),
		Items:          items,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: payloadMap,
	})
}

func buildSearchTextFallbackView(input SearchViewInput) ResultView {
	subtitle := "搜索结果不是合法 JSON，已回退为文本预览。"
	if strings.TrimSpace(input.Observation) == "" {
		subtitle = "搜索工具没有返回结构化结果，已回退为文本预览。"
	}

	sections := []map[string]any{
		BuildCalloutSection("结果不可解析", subtitle, "danger", []string{subtitle}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("搜索参数", buildSearchArgFields(input)))
	appendSectionIfPresent(&sections, buildRawPreviewSection(input.Observation))

	return BuildResultView(BuildResultViewInput{
		ViewType:    ViewTypeSearchResult,
		Status:      StatusFailed,
		Title:       "网页搜索结果不可解析",
		Subtitle:    subtitle,
		Metrics:     buildSearchMetrics(0, input.DomainAllow, input.RecencyDays),
		Items:       make([]ItemView, 0),
		Sections:    sections,
		Observation: input.Observation,
	})
}

func buildSearchFailureView(
	input SearchViewInput,
	query string,
	errorMessage string,
	payloadMap map[string]any,
) ResultView {
	status := classifyUnavailableStatus(errorMessage)
	title := "网页搜索失败"
	calloutTitle := "搜索执行失败"
	tone := "danger"
	if status == StatusBlocked {
		title = "网页搜索未启用"
		calloutTitle = "搜索 Provider 未启用"
		tone = "warning"
	}

	sections := []map[string]any{
		BuildCalloutSection(calloutTitle, errorMessage, tone, []string{errorMessage}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("搜索参数", buildSearchArgFields(input)))

	return BuildResultView(BuildResultViewInput{
		ViewType:       ViewTypeSearchResult,
		Status:         status,
		Title:          title,
		Subtitle:       buildSearchFailureSubtitle(query, errorMessage),
		Metrics:        buildSearchMetrics(0, input.DomainAllow, input.RecencyDays),
		Items:          make([]ItemView, 0),
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: payloadMap,
	})
}

func buildSearchItems(items []searchObservationItem) []ItemView {
	if len(items) == 0 {
		return make([]ItemView, 0)
	}
	out := make([]ItemView, 0, len(items))
	for index, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = fmt.Sprintf("结果 %d", index+1)
		}

		subtitle := strings.TrimSpace(item.URL)
		if domain := strings.TrimSpace(item.Domain); domain != "" {
			subtitle = domain
		}

		tags := make([]string, 0, 2)
		if domain := strings.TrimSpace(item.Domain); domain != "" {
			tags = append(tags, domain)
		}
		if publishedAt := strings.TrimSpace(item.PublishedAt); publishedAt != "" {
			tags = append(tags, publishedAt)
		}

		detailLines := make([]string, 0, 2)
		if snippet := strings.TrimSpace(item.Snippet); snippet != "" {
			detailLines = append(detailLines, previewText(snippet, 120))
		}
		if rawURL := strings.TrimSpace(item.URL); rawURL != "" {
			detailLines = append(detailLines, rawURL)
		}

		out = append(out, BuildItem(title, subtitle, tags, detailLines, map[string]any{
			"url":          strings.TrimSpace(item.URL),
			"domain":       strings.TrimSpace(item.Domain),
			"published_at": strings.TrimSpace(item.PublishedAt),
		}))
	}
	return out
}

func buildSearchMetrics(count int, domainAllow []string, recencyDays int) []MetricField {
	metrics := []MetricField{
		BuildMetric("结果数", fmt.Sprintf("%d", count)),
	}
	if len(domainAllow) > 0 {
		metrics = append(metrics, BuildMetric("域名过滤", formatStringSliceCN(domainAllow, 2)))
	}
	if recencyDays > 0 {
		metrics = append(metrics, BuildMetric("时效", fmt.Sprintf("近 %d 天", recencyDays)))
	}
	return metrics
}

func buildSearchArgFields(input SearchViewInput) []KVField {
	fields := make([]KVField, 0, 4)
	if query := strings.TrimSpace(input.Query); query != "" {
		fields = append(fields, BuildKVField("关键词", query))
	}
	if input.TopK > 0 {
		fields = append(fields, BuildKVField("结果上限", fmt.Sprintf("%d", input.TopK)))
	}
	if len(input.DomainAllow) > 0 {
		fields = append(fields, BuildKVField("域名过滤", formatStringSliceCN(input.DomainAllow, 4)))
	}
	if input.RecencyDays > 0 {
		fields = append(fields, BuildKVField("时效范围", fmt.Sprintf("近 %d 天", input.RecencyDays)))
	}
	return fields
}

func buildSearchSubtitle(query string) string {
	if strings.TrimSpace(query) == "" {
		return "已返回网页搜索结果。"
	}
	return fmt.Sprintf("关键词：%s", previewText(query, 40))
}

func buildSearchFailureSubtitle(query string, errorMessage string) string {
	if strings.TrimSpace(query) == "" {
		return strings.TrimSpace(errorMessage)
	}
	return fmt.Sprintf("关键词：%s", previewText(query, 40))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
