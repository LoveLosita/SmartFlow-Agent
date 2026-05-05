package schedule_analysis

const (
	// ViewTypeAnalysisResult 是第三批诊断分析结果卡片的前端识别类型。
	ViewTypeAnalysisResult = "schedule.analysis_result"

	// ViewVersionAnalysisResult 是当前诊断分析结果结构版本。
	ViewVersionAnalysisResult = 1

	// 这里不依赖父包状态常量，避免子包反向 import tools 形成循环依赖。
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusBlocked = "blocked"
)

// AnalysisResultView 是子包暴露给父包 adapter 的纯展示结构。
//
// 职责边界：
// 1. 负责承载 view_type / version / collapsed / expanded 四段展示数据；
// 2. 不负责 ToolExecutionResult、SSE、registry 等父包协议；
// 3. collapsed / expanded 保持 map 形态，方便父包直接桥接到现有展示协议。
type AnalysisResultView struct {
	ViewType  string         `json:"view_type"`
	Version   int            `json:"version"`
	Collapsed map[string]any `json:"collapsed"`
	Expanded  map[string]any `json:"expanded"`
}

// KVField 是展开态 kv section 的轻量键值结构。
type KVField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// MetricField 是 collapsed.metrics 的轻量键值结构。
type MetricField struct {
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

// BuildResultViewInput 是通用 analysis 结果视图 builder 的输入。
//
// 职责边界：
// 1. 负责承载已经计算好的标题、副标题、指标、列表、分区；
// 2. 不负责执行分析工具，observation 必须由父包 adapter 传入；
// 3. observation 会原样写入 raw_text，不能在这里改写给下游消费的 JSON。
type BuildResultViewInput struct {
	Status         string
	Title          string
	Subtitle       string
	Metrics        []MetricField
	Items          []ItemView
	Sections       []map[string]any
	Observation    string
	MachinePayload map[string]any
}

// BuildFailureViewInput 是失败视图 builder 的输入。
type BuildFailureViewInput struct {
	ToolName    string
	Status      string
	Title       string
	Subtitle    string
	Observation string
	ArgFields   []KVField
}

// AnalyzeHealthViewInput 是 analyze_health 视图构造输入。
type AnalyzeHealthViewInput struct {
	Observation string
	ArgFields   []KVField
}

// AnalyzeRhythmViewInput 是 analyze_rhythm 视图构造输入。
type AnalyzeRhythmViewInput struct {
	Observation string
	ArgFields   []KVField
}
