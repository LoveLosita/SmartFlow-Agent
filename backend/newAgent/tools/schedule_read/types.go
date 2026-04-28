package schedule_read

import (
	"strings"

	"github.com/LoveLosita/smartflow/backend/newAgent/tools/schedule"
)

const (
	// ViewTypeReadResult 固定为第二批 read 结果卡片的前端识别类型。
	ViewTypeReadResult = "schedule.read_result"

	// ViewVersionReadResult 固定为当前 read 结果结构版本。
	ViewVersionReadResult = 1

	// 这里不依赖父包状态常量，避免子包反向 import tools 形成循环依赖。
	StatusDone    = "done"
	StatusFailed  = "failed"
	StatusBlocked = "blocked"
)

// ReadResultView 是子包暴露给父包 adapter 的纯展示结构。
//
// 职责边界：
// 1. 负责承载 view_type / version / collapsed / expanded 四段展示数据。
// 2. 不负责 ToolExecutionResult、SSE、registry 等父包协议。
// 3. collapsed / expanded 继续保留 map 形态，方便父包直接桥接到现有展示协议。
type ReadResultView struct {
	ViewType  string         `json:"view_type"`
	Version   int            `json:"version"`
	Collapsed map[string]any `json:"collapsed"`
	Expanded  map[string]any `json:"expanded"`
}

// CollapsedView 表示折叠态卡片数据。
type CollapsedView struct {
	Title       string        `json:"title"`
	Subtitle    string        `json:"subtitle"`
	Status      string        `json:"status"`
	StatusLabel string        `json:"status_label"`
	Metrics     []MetricField `json:"metrics"`
}

// ExpandedView 表示展开态卡片数据。
type ExpandedView struct {
	Items          []ItemView       `json:"items"`
	Sections       []map[string]any `json:"sections"`
	RawText        string           `json:"raw_text"`
	MachinePayload map[string]any   `json:"machine_payload,omitempty"`
}

// MetricField 是 collapsed.metrics 的轻量键值结构。
type MetricField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// KVField 是展开态 kv section 的轻量键值结构。
type KVField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// ItemView 是展开态 items 的通用结构。
type ItemView struct {
	Title       string         `json:"title"`
	Subtitle    string         `json:"subtitle"`
	Tags        []string       `json:"tags"`
	DetailLines []string       `json:"detail_lines"`
	Meta        map[string]any `json:"meta,omitempty"`
}

// BuildResultViewInput 是通用 read 结果视图 builder 的输入。
//
// 职责边界：
// 1. 负责承载已经计算好的标题、副标题、指标、列表、分区。
// 2. 不负责判断工具是否执行成功；调用方需要在进入这里前确定 status。
// 3. observation 会原样写入 raw_text，不能在这里改写给 LLM 的观察文本语义。
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

// BuildFailureViewInput 是通用失败视图 builder 的输入。
type BuildFailureViewInput struct {
	ToolName    string
	Status      string
	Title       string
	Subtitle    string
	Observation string
	ArgFields   []KVField
}

// AvailableSlotsViewInput 是 query_available_slots 视图构造输入。
type AvailableSlotsViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	ArgFields   []KVField
}

// RangeViewInput 是 query_range 统一入口输入。
type RangeViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	Day         int
	SlotStart   *int
	SlotEnd     *int
	ArgFields   []KVField
}

// RangeFullDayViewInput 是 query_range 整天模式输入。
type RangeFullDayViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	Day         int
	ArgFields   []KVField
}

// RangeSpecificViewInput 是 query_range 指定时段模式输入。
type RangeSpecificViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	Day         int
	SlotStart   int
	SlotEnd     int
	ArgFields   []KVField
}

// TargetTasksViewInput 是 query_target_tasks 视图构造输入。
type TargetTasksViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	ArgFields   []KVField
}

// TaskInfoViewInput 是 get_task_info 视图构造输入。
type TaskInfoViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	TaskID      int
	ArgFields   []KVField
}

// OverviewViewInput 是 get_overview 视图构造输入。
type OverviewViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	ArgFields   []KVField
}

// QueueStatusViewInput 是 queue_status 视图构造输入。
type QueueStatusViewInput struct {
	State       *schedule.ScheduleState
	Observation string
	ArgFields   []KVField
}

// AvailableSlotsPayload 是 query_available_slots 的结构化结果。
type AvailableSlotsPayload struct {
	Tool            string                `json:"tool"`
	Success         bool                  `json:"success"`
	Error           string                `json:"error"`
	Count           int                   `json:"count"`
	StrictCount     int                   `json:"strict_count"`
	EmbeddedCount   int                   `json:"embedded_count"`
	FallbackUsed    bool                  `json:"fallback_used"`
	DayScope        string                `json:"day_scope"`
	DayOfWeek       []int                 `json:"day_of_week"`
	WeekFilter      []int                 `json:"week_filter"`
	WeekFrom        int                   `json:"week_from"`
	WeekTo          int                   `json:"week_to"`
	Span            int                   `json:"span"`
	AllowEmbed      bool                  `json:"allow_embed"`
	ExcludeSections []int                 `json:"exclude_sections"`
	Slots           []AvailableSlotRecord `json:"slots"`
}

// AvailableSlotRecord 是 query_available_slots 单条时段记录。
type AvailableSlotRecord struct {
	Day       int    `json:"day"`
	Week      int    `json:"week"`
	DayOfWeek int    `json:"day_of_week"`
	SlotStart int    `json:"slot_start"`
	SlotEnd   int    `json:"slot_end"`
	SlotType  string `json:"slot_type"`
}

// TargetTasksPayload 是 query_target_tasks 的结构化结果。
type TargetTasksPayload struct {
	Tool       string                  `json:"tool"`
	Success    bool                    `json:"success"`
	Error      string                  `json:"error"`
	Count      int                     `json:"count"`
	Status     string                  `json:"status"`
	DayScope   string                  `json:"day_scope"`
	DayOfWeek  []int                   `json:"day_of_week"`
	WeekFilter []int                   `json:"week_filter"`
	WeekFrom   int                     `json:"week_from"`
	WeekTo     int                     `json:"week_to"`
	Enqueue    bool                    `json:"enqueue"`
	Enqueued   int                     `json:"enqueued"`
	Queue      *TargetTasksQueueRecord `json:"queue"`
	Items      []TargetTaskRecord      `json:"items"`
}

// TargetTasksQueueRecord 是目标任务查询里的队列快照。
type TargetTasksQueueRecord struct {
	PendingCount   int `json:"pending_count"`
	CompletedCount int `json:"completed_count"`
	SkippedCount   int `json:"skipped_count"`
	CurrentTaskID  int `json:"current_task_id"`
	CurrentAttempt int `json:"current_attempt"`
}

// TargetTaskRecord 是 query_target_tasks 单条任务记录。
type TargetTaskRecord struct {
	TaskID      int                  `json:"task_id"`
	Name        string               `json:"name"`
	Category    string               `json:"category"`
	Status      string               `json:"status"`
	Duration    int                  `json:"duration"`
	TaskClassID int                  `json:"task_class_id"`
	Slots       []TargetTaskSlotInfo `json:"slots"`
}

// TargetTaskSlotInfo 是目标任务时段信息。
type TargetTaskSlotInfo struct {
	Day       int `json:"day"`
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	SlotStart int `json:"slot_start"`
	SlotEnd   int `json:"slot_end"`
}

// QueueStatusPayload 是 queue_status 的结构化结果。
type QueueStatusPayload struct {
	Tool           string             `json:"tool"`
	Success        bool               `json:"success"`
	Error          string             `json:"error"`
	PendingCount   int                `json:"pending_count"`
	CompletedCount int                `json:"completed_count"`
	SkippedCount   int                `json:"skipped_count"`
	CurrentTaskID  int                `json:"current_task_id"`
	CurrentAttempt int                `json:"current_attempt"`
	LastError      string             `json:"last_error"`
	NextTaskIDs    []int              `json:"next_task_ids"`
	Current        *QueueTaskSnapshot `json:"current"`
}

// QueueTaskSnapshot 是 queue_status 当前任务快照。
type QueueTaskSnapshot struct {
	TaskID      int                  `json:"task_id"`
	Name        string               `json:"name"`
	Category    string               `json:"category"`
	Status      string               `json:"status"`
	Duration    int                  `json:"duration"`
	TaskClassID int                  `json:"task_class_id"`
	Slots       []TargetTaskSlotInfo `json:"slots"`
}

func (view CollapsedView) Map() map[string]any {
	metrics := make([]map[string]any, 0, len(view.Metrics))
	for _, metric := range view.Metrics {
		metrics = append(metrics, map[string]any{
			"label": strings.TrimSpace(metric.Label),
			"value": strings.TrimSpace(metric.Value),
		})
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

	out := map[string]any{
		"items":    items,
		"sections": cloneSectionList(view.Sections),
		"raw_text": view.RawText,
	}
	if len(view.MachinePayload) > 0 {
		out["machine_payload"] = cloneAnyMap(view.MachinePayload)
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
