package web_result

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

type fetchObservation struct {
	Tool      string `json:"tool"`
	URL       string `json:"url"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Truncated bool   `json:"truncated"`
	Error     string `json:"error"`
	Err       string `json:"err"`
	Reason    string `json:"reason"`
}

// BuildFetchView 负责把 web_fetch observation 构造成前端可直接消费的结果卡片。
//
// 职责边界：
// 1. 负责解析成功 / 失败 / provider 未启用 / 非 JSON 回退四类场景。
// 2. 负责保留 raw_text 与 machine_payload，便于前端调试与后续交互。
// 3. 不负责真正抓取网页，也不改写传入 observation 原文。
func BuildFetchView(input FetchViewInput) ResultView {
	payloadMap, ok := parseObservationJSON(input.Observation)
	if !ok {
		return buildFetchTextFallbackView(input)
	}

	payload := fetchObservation{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(input.Observation)), &payload); err != nil {
		return buildFetchTextFallbackView(input)
	}

	rawURL := strings.TrimSpace(payload.URL)
	if rawURL == "" {
		rawURL = strings.TrimSpace(input.URL)
	}

	errorMessage := firstNonEmpty(payload.Error, payload.Err, payload.Reason)
	if errorMessage == "" {
		errorMessage = firstString(payloadMap, "message")
	}
	if errorMessage != "" {
		return buildFetchFailureView(input, rawURL, errorMessage, payloadMap)
	}

	title := strings.TrimSpace(payload.Title)
	content := strings.TrimSpace(payload.Content)
	host := hostnameFromURL(rawURL)
	contentChars := utf8.RuneCountInString(content)
	if title == "" {
		title = "网页正文"
		if host != "" {
			title = host
		}
	}

	itemTags := make([]string, 0, 2)
	if host != "" {
		itemTags = append(itemTags, host)
	}
	if payload.Truncated {
		itemTags = append(itemTags, "已截断")
	}

	items := []ItemView{
		BuildItem(
			title,
			rawURL,
			itemTags,
			buildFetchPreviewLines(content),
			map[string]any{
				"url":         rawURL,
				"title":       strings.TrimSpace(payload.Title),
				"content_len": contentChars,
				"truncated":   payload.Truncated,
			},
		),
	}

	sections := []map[string]any{
		BuildKVSection("页面信息", []KVField{
			BuildKVField("链接", rawURL),
			BuildKVField("标题", fallbackText(payload.Title, "未提取到标题")),
			BuildKVField("正文长度", fmt.Sprintf("%d 字", contentChars)),
			BuildKVField("是否截断", formatBoolCN(payload.Truncated)),
		}),
		BuildCalloutSection(
			"正文预览",
			previewText(content, 120),
			"info",
			buildFetchPreviewLines(content),
		),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("抓取参数", buildFetchArgFields(input)))

	return BuildResultView(BuildResultViewInput{
		ViewType:       ViewTypeFetchResult,
		Status:         StatusDone,
		Title:          buildFetchTitle(title),
		Subtitle:       buildFetchSubtitle(rawURL, host),
		Metrics:        buildFetchMetrics(contentChars, payload.Truncated, host),
		Items:          items,
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: payloadMap,
	})
}

func buildFetchTextFallbackView(input FetchViewInput) ResultView {
	subtitle := "抓取结果不是合法 JSON，已回退为文本预览。"
	if strings.TrimSpace(input.Observation) == "" {
		subtitle = "抓取工具没有返回结构化结果，已回退为文本预览。"
	}

	sections := []map[string]any{
		BuildCalloutSection("结果不可解析", subtitle, "danger", []string{subtitle}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("抓取参数", buildFetchArgFields(input)))
	appendSectionIfPresent(&sections, buildRawPreviewSection(input.Observation))

	return BuildResultView(BuildResultViewInput{
		ViewType:    ViewTypeFetchResult,
		Status:      StatusFailed,
		Title:       "网页抓取结果不可解析",
		Subtitle:    subtitle,
		Metrics:     buildFetchMetrics(0, false, hostnameFromURL(input.URL)),
		Items:       make([]ItemView, 0),
		Sections:    sections,
		Observation: input.Observation,
	})
}

func buildFetchFailureView(
	input FetchViewInput,
	rawURL string,
	errorMessage string,
	payloadMap map[string]any,
) ResultView {
	status := classifyUnavailableStatus(errorMessage)
	title := "网页抓取失败"
	calloutTitle := "抓取执行失败"
	tone := "danger"
	if status == StatusBlocked {
		title = "网页抓取未启用"
		calloutTitle = "抓取服务未启用"
		tone = "warning"
	}

	sections := []map[string]any{
		BuildCalloutSection(calloutTitle, errorMessage, tone, []string{errorMessage}),
	}
	appendSectionIfPresent(&sections, BuildArgsSection("抓取参数", buildFetchArgFields(input)))

	return BuildResultView(BuildResultViewInput{
		ViewType:       ViewTypeFetchResult,
		Status:         status,
		Title:          title,
		Subtitle:       buildFetchFailureSubtitle(rawURL, errorMessage),
		Metrics:        buildFetchMetrics(0, false, hostnameFromURL(rawURL)),
		Items:          make([]ItemView, 0),
		Sections:       sections,
		Observation:    input.Observation,
		MachinePayload: payloadMap,
	})
}

func buildFetchMetrics(contentChars int, truncated bool, host string) []MetricField {
	metrics := []MetricField{
		BuildMetric("正文长度", fmt.Sprintf("%d 字", contentChars)),
		BuildMetric("是否截断", formatBoolCN(truncated)),
	}
	if strings.TrimSpace(host) != "" {
		metrics = append(metrics, BuildMetric("来源", host))
	}
	return metrics
}

func buildFetchArgFields(input FetchViewInput) []KVField {
	fields := make([]KVField, 0, 2)
	if rawURL := strings.TrimSpace(input.URL); rawURL != "" {
		fields = append(fields, BuildKVField("链接", rawURL))
	}
	if input.MaxChars > 0 {
		fields = append(fields, BuildKVField("截断上限", fmt.Sprintf("%d 字", input.MaxChars)))
	}
	return fields
}

func buildFetchPreviewLines(content string) []string {
	lines := previewLines(content, 3, 120)
	if len(lines) > 0 {
		return lines
	}
	return []string{"正文为空"}
}

func buildFetchTitle(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "已抓取网页正文"
	}
	return fmt.Sprintf("已抓取：%s", previewText(title, 36))
}

func buildFetchSubtitle(rawURL string, host string) string {
	if strings.TrimSpace(host) != "" {
		return fmt.Sprintf("来源：%s", host)
	}
	if strings.TrimSpace(rawURL) != "" {
		return fmt.Sprintf("来源：%s", previewText(rawURL, 48))
	}
	return "已返回网页正文。"
}

func buildFetchFailureSubtitle(rawURL string, errorMessage string) string {
	if strings.TrimSpace(rawURL) == "" {
		return strings.TrimSpace(errorMessage)
	}
	return fmt.Sprintf("链接：%s", previewText(rawURL, 48))
}

func fallbackText(text string, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(text)
}
