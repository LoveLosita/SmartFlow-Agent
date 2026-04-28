package taskclass_result

const (
	// ViewTypeWriteResult 固定为任务类写入结果卡片的前端识别类型。
	ViewTypeWriteResult = "taskclass.write_result"

	// ViewVersionWriteResult 固定为当前任务类写入结果结构版本。
	ViewVersionWriteResult = 1

	// 这里不依赖父包状态常量，避免子包反向 import tools 形成循环依赖。
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusBlocked = "blocked"
)

// WriteResultView 是子包暴露给父包 adapter 的纯展示结构。
//
// 职责边界：
// 1. 只承载 view_type / version / collapsed / expanded 四段展示数据；
// 2. 不负责 ToolExecutionResult、SSE、timeline 等父包协议；
// 3. collapsed / expanded 继续保留 map 形态，便于父包直接桥接。
type WriteResultView struct {
	ViewType  string         `json:"view_type"`
	Version   int            `json:"version"`
	Collapsed map[string]any `json:"collapsed"`
	Expanded  map[string]any `json:"expanded"`
}

// MetricField 是 collapsed.metrics 的轻量键值结构。
type MetricField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// KVField 是 expanded.kv section 的轻量键值结构。
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

// UpsertResult 承载写入 observation 里可稳定提取的结果字段。
//
// 职责边界：
// 1. 只描述 upsert_task_class 的结果，不承载请求参数；
// 2. ValidationIssues 仅用于展示校验失败原因，不负责重新校验；
// 3. Error / ErrorCode 保持和 observation 一致，避免展示层发明新语义。
type UpsertResult struct {
	Tool             string
	Success          bool
	TaskClassID      int
	Created          bool
	Error            string
	ErrorCode        string
	ValidationOK     bool
	ValidationIssues []string
}

// RequestSummary 描述写入请求中适合前端展示的稳定字段摘要。
//
// 职责边界：
// 1. 只保留卡片展示稳定需要的信息，不回传完整原始 args；
// 2. RequestedID 表示调用方请求更新的任务类 ID，不等同于持久化后的真实 ID；
// 3. Items 已经是展示层可直接消费的扁平摘要，不再承担业务校验职责。
type RequestSummary struct {
	RequestedID        int
	Name               string
	Mode               string
	StartDate          string
	EndDate            string
	SubjectType        string
	DifficultyLevel    string
	CognitiveIntensity string
	TotalSlots         int
	AllowFillerCourse  bool
	Strategy           string
	ExcludedSlots      []int
	ExcludedDaysOfWeek []int
	Source             string
	Items              []TaskClassItemSummary
}

// TaskClassItemSummary 描述单个任务项的展示摘要。
type TaskClassItemSummary struct {
	ID                  int
	Order               int
	Content             string
	EmbeddedWeek        int
	EmbeddedDay         int
	EmbeddedSectionFrom int
	EmbeddedSectionTo   int
}

// BuildUpsertTaskClassViewInput 是 upsert_task_class 卡片 builder 的输入。
//
// 职责边界：
// 1. Observation 必须原样传入，供 raw_text 保留原始结果；
// 2. MachinePayload 只作为调试/后续交互的隐藏字段，不参与标题摘要计算；
// 3. Status 由父包 adapter 传入，子包只负责标准化，不重新推断工具执行链路。
type BuildUpsertTaskClassViewInput struct {
	Status         string
	Observation    string
	Result         UpsertResult
	Request        RequestSummary
	MachinePayload map[string]any
}
