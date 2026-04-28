package toolcontextresult

import (
	"fmt"
	"strings"
)

const (
	ViewTypeContextResult    = "tool.context_result"
	ViewVersionContextResult = 1
	StatusDone               = "done"
	StatusFailed             = "failed"
)

// ContextResultView 仅承载 context 工具卡片的纯展示数据。
//
// 职责边界：
// 1. 负责输出 view_type / version / collapsed / expanded 四段展示结构；
// 2. 不依赖父包 ToolExecutionResult，避免形成反向 import；
// 3. 不改写 ObservationText，原始文本由父包原样挂到 expanded.raw_text。
type ContextResultView struct {
	ViewType  string         `json:"view_type"`
	Version   int            `json:"version"`
	Collapsed map[string]any `json:"collapsed"`
	Expanded  map[string]any `json:"expanded"`
}

type MetricField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type KVField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type ItemView struct {
	Title       string         `json:"title"`
	Subtitle    string         `json:"subtitle"`
	Tags        []string       `json:"tags"`
	DetailLines []string       `json:"detail_lines"`
	Meta        map[string]any `json:"meta,omitempty"`
}

// ContextToolsAddPayload 对齐 context_tools_add observation 的机器字段。
type ContextToolsAddPayload struct {
	Tool      string   `json:"tool"`
	Success   bool     `json:"success"`
	Action    string   `json:"action"`
	Domain    string   `json:"domain,omitempty"`
	Packs     []string `json:"packs,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Message   string   `json:"message,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorCode string   `json:"error_code,omitempty"`
}

// ContextToolsRemovePayload 对齐 context_tools_remove observation 的机器字段。
type ContextToolsRemovePayload struct {
	Tool      string   `json:"tool"`
	Success   bool     `json:"success"`
	Action    string   `json:"action"`
	Domain    string   `json:"domain,omitempty"`
	Packs     []string `json:"packs,omitempty"`
	All       bool     `json:"all,omitempty"`
	Message   string   `json:"message,omitempty"`
	Error     string   `json:"error,omitempty"`
	ErrorCode string   `json:"error_code,omitempty"`
}

// BuildAddView 生成 context_tools_add 的结构化卡片。
func BuildAddView(payload ContextToolsAddPayload, observation string) ContextResultView {
	status := statusFromSuccess(payload.Success)
	summary := buildAddSummary(payload)
	detailLines := buildAddDetailLines(payload)
	item := BuildItem(
		fallbackText(ResolveDomainLabelCN(payload.Domain), "工具域"),
		summary,
		buildAddTags(payload),
		detailLines,
		map[string]any{
			"action": payload.Action,
			"domain": strings.TrimSpace(payload.Domain),
			"mode":   strings.TrimSpace(payload.Mode),
		},
	)

	sections := []map[string]any{
		buildContextCalloutSection(
			fallbackText(buildAddCalloutTitle(payload), "工具域变更"),
			summary,
			toneFromSuccess(payload.Success),
			detailLines,
		),
		BuildKVSection("当前工具区参数", []KVField{
			BuildKVField("工具域", fallbackText(ResolveDomainLabelCN(payload.Domain), "未指定")),
			BuildKVField("工具包", buildAddPackField(payload)),
			BuildKVField("注入模式", fallbackText(ResolveModeLabelCN(payload.Mode), "未指定")),
			BuildKVField("清空全部", "否"),
			BuildKVField("动作", fallbackText(ResolveActionLabelCN(payload.Action), strings.TrimSpace(payload.Action))),
		}),
		BuildItemsSection("变更摘要", "", []ItemView{item}),
	}
	if !payload.Success {
		appendErrorSection(&sections, payload.Error, payload.ErrorCode)
	}

	return ContextResultView{
		ViewType: ViewTypeContextResult,
		Version:  ViewVersionContextResult,
		Collapsed: map[string]any{
			"title":        buildAddCollapsedTitle(payload),
			"subtitle":     summary,
			"status":       status,
			"status_label": resolveStatusLabelCN(status),
			"metrics": buildMetrics(
				BuildMetric("域", fallbackText(shortDomainLabel(payload.Domain), "未指定")),
				BuildMetric("包", fmt.Sprintf("%d 个", len(payload.Packs))),
				BuildMetric("模式", fallbackText(ResolveModeLabelCN(payload.Mode), "未指定")),
			),
		},
		Expanded: map[string]any{
			"items":           []map[string]any{item.Map()},
			"sections":        sections,
			"raw_text":        observation,
			"machine_payload": buildAddMachinePayload(payload),
		},
	}
}

// BuildRemoveView 生成 context_tools_remove 的结构化卡片。
func BuildRemoveView(payload ContextToolsRemovePayload, observation string) ContextResultView {
	status := statusFromSuccess(payload.Success)
	summary := buildRemoveSummary(payload)
	detailLines := buildRemoveDetailLines(payload)
	item := BuildItem(
		buildRemoveItemTitle(payload),
		summary,
		buildRemoveTags(payload),
		detailLines,
		map[string]any{
			"action": payload.Action,
			"domain": strings.TrimSpace(payload.Domain),
			"all":    payload.All,
		},
	)

	sections := []map[string]any{
		buildContextCalloutSection(
			fallbackText(buildRemoveCalloutTitle(payload), "工具域变更"),
			summary,
			toneFromSuccess(payload.Success),
			detailLines,
		),
		BuildKVSection("当前工具区参数", []KVField{
			BuildKVField("工具域", buildRemoveDomainField(payload)),
			BuildKVField("工具包", buildRemovePackField(payload)),
			BuildKVField("注入模式", "不适用"),
			BuildKVField("清空全部", formatBoolCN(payload.All)),
			BuildKVField("动作", fallbackText(ResolveActionLabelCN(payload.Action), strings.TrimSpace(payload.Action))),
		}),
		BuildItemsSection("变更摘要", "", []ItemView{item}),
	}
	if !payload.Success {
		appendErrorSection(&sections, payload.Error, payload.ErrorCode)
	}

	return ContextResultView{
		ViewType: ViewTypeContextResult,
		Version:  ViewVersionContextResult,
		Collapsed: map[string]any{
			"title":        buildRemoveCollapsedTitle(payload),
			"subtitle":     summary,
			"status":       status,
			"status_label": resolveStatusLabelCN(status),
			"metrics": buildMetrics(
				BuildMetric("范围", buildRemoveMetricScope(payload)),
				BuildMetric("包", fmt.Sprintf("%d 个", len(payload.Packs))),
				BuildMetric("动作", fallbackText(ResolveActionLabelCN(payload.Action), strings.TrimSpace(payload.Action))),
			),
		},
		Expanded: map[string]any{
			"items":           []map[string]any{item.Map()},
			"sections":        sections,
			"raw_text":        observation,
			"machine_payload": buildRemoveMachinePayload(payload),
		},
	}
}

func ResolveDomainLabelCN(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "schedule":
		return "排程工具域"
	case "taskclass":
		return "任务类工具域"
	default:
		return ""
	}
}

func ResolvePackLabelCN(pack string) string {
	switch strings.ToLower(strings.TrimSpace(pack)) {
	case "core":
		return "固定 core"
	case "mutation":
		return "排程改写"
	case "analyze":
		return "健康分析"
	case "detail_read":
		return "明细读取"
	case "deep_analyze":
		return "深度分析"
	case "queue":
		return "队列微调"
	case "web":
		return "网页能力"
	default:
		return strings.TrimSpace(pack)
	}
}

func FormatPacksCN(packs []string) string {
	if len(packs) == 0 {
		return "无"
	}
	parts := make([]string, 0, len(packs))
	for _, pack := range packs {
		label := strings.TrimSpace(ResolvePackLabelCN(pack))
		if label == "" {
			continue
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return "无"
	}
	return strings.Join(parts, "、")
}

func ResolveModeLabelCN(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "replace":
		return "替换"
	case "merge":
		return "合并"
	default:
		return strings.TrimSpace(mode)
	}
}

func ResolveActionLabelCN(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "activate":
		return "激活"
	case "deactivate":
		return "移除域"
	case "deactivate_packs":
		return "移除包"
	case "clear_all":
		return "清空全部"
	case "reject":
		return "拒绝"
	default:
		return strings.TrimSpace(action)
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

func BuildItemsSection(title string, summary string, items []ItemView) map[string]any {
	normalized := make([]map[string]any, 0, len(items))
	for _, item := range items {
		normalized = append(normalized, item.Map())
	}
	return map[string]any{
		"type":    "items",
		"title":   strings.TrimSpace(title),
		"summary": strings.TrimSpace(summary),
		"items":   normalized,
	}
}

func BuildKVSection(title string, fields []KVField) map[string]any {
	normalized := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		label := strings.TrimSpace(field.Label)
		value := strings.TrimSpace(field.Value)
		if label == "" || value == "" {
			continue
		}
		normalized = append(normalized, map[string]any{
			"label": label,
			"value": value,
		})
	}
	return map[string]any{
		"type":   "kv",
		"title":  strings.TrimSpace(title),
		"fields": normalized,
	}
}

func buildContextCalloutSection(title string, summary string, tone string, detailLines []string) map[string]any {
	return map[string]any{
		"type":         "callout",
		"title":        strings.TrimSpace(title),
		"summary":      strings.TrimSpace(summary),
		"tone":         strings.TrimSpace(tone),
		"detail_lines": normalizeStringSlice(detailLines),
	}
}

func appendErrorSection(target *[]map[string]any, reason string, errorCode string) {
	lines := make([]string, 0, 2)
	if strings.TrimSpace(reason) != "" {
		lines = append(lines, strings.TrimSpace(reason))
	}
	if strings.TrimSpace(errorCode) != "" {
		lines = append(lines, fmt.Sprintf("错误码：%s", strings.TrimSpace(errorCode)))
	}
	*target = append(*target, buildContextCalloutSection("失败原因", fallbackText(reason, "工具调用失败"), "danger", lines))
}

func buildAddCollapsedTitle(payload ContextToolsAddPayload) string {
	if !payload.Success {
		return "激活工具域失败"
	}
	label := ResolveDomainLabelCN(payload.Domain)
	if label == "" {
		return "已激活工具域"
	}
	return fmt.Sprintf("已激活%s", label)
}

func buildRemoveCollapsedTitle(payload ContextToolsRemovePayload) string {
	if !payload.Success {
		return "移除工具域失败"
	}
	if payload.All {
		return "已清空业务工具域"
	}
	if len(payload.Packs) > 0 {
		return "已移除工具包"
	}
	label := ResolveDomainLabelCN(payload.Domain)
	if label == "" {
		return "已移除工具域"
	}
	return fmt.Sprintf("已移除%s", label)
}

func buildAddCalloutTitle(payload ContextToolsAddPayload) string {
	if payload.Success {
		return "动态工具区已更新"
	}
	return "激活失败"
}

func buildRemoveCalloutTitle(payload ContextToolsRemovePayload) string {
	if payload.Success {
		return "动态工具区已更新"
	}
	return "移除失败"
}

func buildAddSummary(payload ContextToolsAddPayload) string {
	if !payload.Success {
		return fallbackText(payload.Error, "激活工具域失败")
	}
	domainLabel := fallbackText(ResolveDomainLabelCN(payload.Domain), "工具域")
	packsText := formatPackFieldText(payload.Packs)
	modeLabel := fallbackText(ResolveModeLabelCN(payload.Mode), "替换")
	if len(payload.Packs) == 0 {
		return fmt.Sprintf("%s已激活，模式=%s，仅保留固定 core。", domainLabel, modeLabel)
	}
	return fmt.Sprintf("%s已激活，模式=%s，启用 %s。", domainLabel, modeLabel, packsText)
}

func buildRemoveSummary(payload ContextToolsRemovePayload) string {
	if !payload.Success {
		return fallbackText(payload.Error, "移除工具域失败")
	}
	if payload.All {
		return "已清空全部业务工具域，仅保留 context 管理工具。"
	}
	domainLabel := fallbackText(ResolveDomainLabelCN(payload.Domain), "工具域")
	if len(payload.Packs) > 0 {
		return fmt.Sprintf("已从%s移除 %s。", domainLabel, FormatPacksCN(payload.Packs))
	}
	return fmt.Sprintf("已移除%s。", domainLabel)
}

func buildAddDetailLines(payload ContextToolsAddPayload) []string {
	lines := []string{
		fmt.Sprintf("工具域：%s", fallbackText(ResolveDomainLabelCN(payload.Domain), "未指定")),
		fmt.Sprintf("工具包：%s", buildAddPackField(payload)),
		fmt.Sprintf("注入模式：%s", fallbackText(ResolveModeLabelCN(payload.Mode), "未指定")),
	}
	if strings.TrimSpace(payload.Message) != "" {
		lines = append(lines, strings.TrimSpace(payload.Message))
	}
	if !payload.Success && strings.TrimSpace(payload.Error) != "" {
		lines = append(lines, fmt.Sprintf("失败原因：%s", strings.TrimSpace(payload.Error)))
	}
	return lines
}

func buildRemoveDetailLines(payload ContextToolsRemovePayload) []string {
	lines := []string{
		fmt.Sprintf("工具域：%s", buildRemoveDomainField(payload)),
		fmt.Sprintf("工具包：%s", buildRemovePackField(payload)),
		fmt.Sprintf("清空全部：%s", formatBoolCN(payload.All)),
	}
	if strings.TrimSpace(payload.Message) != "" {
		lines = append(lines, strings.TrimSpace(payload.Message))
	}
	if !payload.Success && strings.TrimSpace(payload.Error) != "" {
		lines = append(lines, fmt.Sprintf("失败原因：%s", strings.TrimSpace(payload.Error)))
	}
	return lines
}

func buildAddTags(payload ContextToolsAddPayload) []string {
	tags := []string{
		fallbackText(ResolveActionLabelCN(payload.Action), "激活"),
	}
	if modeLabel := strings.TrimSpace(ResolveModeLabelCN(payload.Mode)); modeLabel != "" {
		tags = append(tags, modeLabel)
	}
	if len(payload.Packs) > 0 {
		tags = append(tags, fmt.Sprintf("%d 个包", len(payload.Packs)))
	}
	return normalizeStringSlice(tags)
}

func buildRemoveTags(payload ContextToolsRemovePayload) []string {
	tags := []string{
		fallbackText(ResolveActionLabelCN(payload.Action), "移除"),
	}
	if payload.All {
		tags = append(tags, "全部")
	}
	if len(payload.Packs) > 0 {
		tags = append(tags, fmt.Sprintf("%d 个包", len(payload.Packs)))
	}
	return normalizeStringSlice(tags)
}

func buildRemoveItemTitle(payload ContextToolsRemovePayload) string {
	if payload.All {
		return "全部业务工具域"
	}
	return fallbackText(ResolveDomainLabelCN(payload.Domain), "工具域")
}

func buildRemoveDomainField(payload ContextToolsRemovePayload) string {
	if payload.All {
		return "全部业务工具域"
	}
	return fallbackText(ResolveDomainLabelCN(payload.Domain), "未指定")
}

func buildRemoveMetricScope(payload ContextToolsRemovePayload) string {
	if payload.All {
		return "全部"
	}
	return fallbackText(shortDomainLabel(payload.Domain), "单域")
}

func buildAddMachinePayload(payload ContextToolsAddPayload) map[string]any {
	return map[string]any{
		"tool":       payload.Tool,
		"success":    payload.Success,
		"action":     strings.TrimSpace(payload.Action),
		"domain":     strings.TrimSpace(payload.Domain),
		"packs":      append([]string(nil), payload.Packs...),
		"mode":       strings.TrimSpace(payload.Mode),
		"message":    strings.TrimSpace(payload.Message),
		"error":      strings.TrimSpace(payload.Error),
		"error_code": strings.TrimSpace(payload.ErrorCode),
	}
}

func buildRemoveMachinePayload(payload ContextToolsRemovePayload) map[string]any {
	return map[string]any{
		"tool":       payload.Tool,
		"success":    payload.Success,
		"action":     strings.TrimSpace(payload.Action),
		"domain":     strings.TrimSpace(payload.Domain),
		"packs":      append([]string(nil), payload.Packs...),
		"all":        payload.All,
		"message":    strings.TrimSpace(payload.Message),
		"error":      strings.TrimSpace(payload.Error),
		"error_code": strings.TrimSpace(payload.ErrorCode),
	}
}

func buildMetrics(metrics ...MetricField) []map[string]any {
	normalized := make([]map[string]any, 0, len(metrics))
	for _, metric := range metrics {
		label := strings.TrimSpace(metric.Label)
		value := strings.TrimSpace(metric.Value)
		if label == "" || value == "" {
			continue
		}
		normalized = append(normalized, map[string]any{
			"label": label,
			"value": value,
		})
	}
	return normalized
}

func toneFromSuccess(success bool) string {
	if success {
		return "info"
	}
	return "danger"
}

func statusFromSuccess(success bool) string {
	if success {
		return StatusDone
	}
	return StatusFailed
}

func resolveStatusLabelCN(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case StatusDone:
		return "已完成"
	default:
		return "失败"
	}
}

func shortDomainLabel(domain string) string {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "schedule":
		return "排程"
	case "taskclass":
		return "任务类"
	default:
		return strings.TrimSpace(domain)
	}
}

func formatPackFieldText(packs []string) string {
	if len(packs) == 0 {
		return "无"
	}
	return FormatPacksCN(packs)
}

func buildAddPackField(payload ContextToolsAddPayload) string {
	if len(payload.Packs) == 0 {
		return "仅固定 core"
	}
	return FormatPacksCN(payload.Packs)
}

func buildRemovePackField(payload ContextToolsRemovePayload) string {
	if len(payload.Packs) == 0 {
		if payload.All {
			return "全部清空"
		}
		if strings.EqualFold(strings.TrimSpace(payload.Action), "deactivate") {
			return "未指定（按整域处理）"
		}
		return "无"
	}
	return FormatPacksCN(payload.Packs)
}

func formatBoolCN(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func fallbackText(text string, fallback string) string {
	if strings.TrimSpace(text) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(text)
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
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
		return nil
	}
	return out
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func (view ItemView) Map() map[string]any {
	item := map[string]any{
		"title":        strings.TrimSpace(view.Title),
		"subtitle":     strings.TrimSpace(view.Subtitle),
		"tags":         normalizeStringSlice(view.Tags),
		"detail_lines": normalizeStringSlice(view.DetailLines),
	}
	if len(view.Meta) > 0 {
		item["meta"] = cloneAnyMap(view.Meta)
	}
	return item
}
