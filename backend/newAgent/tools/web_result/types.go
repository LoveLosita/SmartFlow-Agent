package web_result

import "strings"

const (
	// ViewTypeSearchResult 是 web_search 结果卡片的前端识别类型。
	ViewTypeSearchResult = "web.search_result"

	// ViewTypeFetchResult 是 web_fetch 结果卡片的前端识别类型。
	ViewTypeFetchResult = "web.fetch_result"

	// ViewVersionResult 固定为当前 web 结果卡片结构版本。
	ViewVersionResult = 1

	// 这里不依赖父包状态常量，避免子包反向 import tools 形成循环依赖。
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusBlocked = "blocked"
)

// ResultView 是子包暴露给父包 adapter 的纯展示结构。
//
// 职责边界：
// 1. 负责承载 view_type / version / collapsed / expanded 四段展示数据。
// 2. 不负责 ToolExecutionResult、SSE、registry 等父包协议。
// 3. collapsed / expanded 保持 map 形态，便于父包直接桥接现有展示协议。
type ResultView struct {
	ViewType  string         `json:"view_type"`
	Version   int            `json:"version"`
	Collapsed map[string]any `json:"collapsed"`
	Expanded  map[string]any `json:"expanded"`
}

// CollapsedView 表示卡片折叠态数据。
type CollapsedView struct {
	Title       string        `json:"title"`
	Subtitle    string        `json:"subtitle"`
	Status      string        `json:"status"`
	StatusLabel string        `json:"status_label"`
	Metrics     []MetricField `json:"metrics"`
}

// ExpandedView 表示卡片展开态数据。
type ExpandedView struct {
	Items          []ItemView       `json:"items"`
	Sections       []map[string]any `json:"sections"`
	RawText        string           `json:"raw_text"`
	MachinePayload map[string]any   `json:"machine_payload"`
}

// MetricField 是 collapsed.metrics 的轻量键值结构。
type MetricField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// KVField 是 section.type=kv 的轻量键值结构。
type KVField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ItemView 是 expanded.items / section.items 的通用结构。
type ItemView struct {
	Title       string         `json:"title"`
	Subtitle    string         `json:"subtitle"`
	Tags        []string       `json:"tags"`
	DetailLines []string       `json:"detail_lines"`
	Meta        map[string]any `json:"meta,omitempty"`
}

// BuildResultViewInput 是通用 web 结果视图 builder 的输入。
//
// 职责边界：
// 1. 负责承载已经计算好的标题、副标题、指标、列表、分区。
// 2. 不负责执行 web 工具；observation 必须由父包 adapter 传入。
// 3. observation 会原样写入 raw_text，不能在这里改写给模型的观察文本。
type BuildResultViewInput struct {
	ViewType       string
	Status         string
	Title          string
	Subtitle       string
	Metrics        []MetricField
	Items          []ItemView
	Sections       []map[string]any
	Observation    string
	MachinePayload map[string]any
}

// SearchViewInput 是 web_search 视图构造输入。
type SearchViewInput struct {
	Observation string
	Query       string
	TopK        int
	DomainAllow []string
	RecencyDays int
}

// FetchViewInput 是 web_fetch 视图构造输入。
type FetchViewInput struct {
	Observation string
	URL         string
	MaxChars    int
}

func (view CollapsedView) Map() map[string]any {
	metrics := make([]map[string]any, 0, len(view.Metrics))
	for _, metric := range view.Metrics {
		label := strings.TrimSpace(metric.Label)
		value := strings.TrimSpace(metric.Value)
		if label == "" || value == "" {
			continue
		}
		metrics = append(metrics, map[string]any{
			"label": label,
			"value": value,
		})
	}
	if len(metrics) == 0 {
		metrics = make([]map[string]any, 0)
	}
	return map[string]any{
		"title":        strings.TrimSpace(view.Title),
		"subtitle":     strings.TrimSpace(view.Subtitle),
		"status":       normalizeStatus(view.Status),
		"status_label": strings.TrimSpace(view.StatusLabel),
		"metrics":      metrics,
	}
}

func (view ExpandedView) Map() map[string]any {
	items := make([]map[string]any, 0, len(view.Items))
	for _, item := range view.Items {
		items = append(items, item.Map())
	}
	if len(items) == 0 {
		items = make([]map[string]any, 0)
	}

	sections := cloneSectionList(view.Sections)
	if len(sections) == 0 {
		sections = make([]map[string]any, 0)
	}

	machinePayload := cloneAnyMap(view.MachinePayload)
	if machinePayload == nil {
		machinePayload = make(map[string]any)
	}

	return map[string]any{
		"items":           items,
		"sections":        sections,
		"raw_text":        view.RawText,
		"machine_payload": machinePayload,
	}
}

func (view ItemView) Map() map[string]any {
	out := map[string]any{
		"title":        strings.TrimSpace(view.Title),
		"subtitle":     strings.TrimSpace(view.Subtitle),
		"tags":         normalizeStringSlice(view.Tags),
		"detail_lines": normalizeStringSlice(view.DetailLines),
	}
	if len(view.Meta) > 0 {
		out["meta"] = cloneAnyMap(view.Meta)
	}
	return out
}
