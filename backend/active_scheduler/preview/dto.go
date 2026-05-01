package preview

import (
	"encoding/json"
	"time"

	"github.com/LoveLosita/smartflow/backend/active_scheduler/candidate"
	schedulercontext "github.com/LoveLosita/smartflow/backend/active_scheduler/context"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/observe"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/ports"
)

// CreatePreviewRequest 是把 dry-run 结果固化成主动调度预览的请求 DTO。
//
// 职责边界：
// 1. 负责承载 preview 写库所需的 dry-run 结果与可选覆盖字段；
// 2. 不承载 confirm/apply 请求，也不允许调用方传入正式日程写入参数；
// 3. GeneratedAt 为空时由 Service 时钟生成，ExpiresAt 固定由 generated_at + 1h 推导。
type CreatePreviewRequest struct {
	ActiveContext       *schedulercontext.ActiveScheduleContext `json:"-"`
	Observation         observe.Result                          `json:"-"`
	Candidates          []candidate.Candidate                   `json:"-"`
	PreviewID           string                                  `json:"preview_id,omitempty"`
	TriggerID           string                                  `json:"trigger_id,omitempty"`
	SelectedCandidateID string                                  `json:"selected_candidate_id,omitempty"`
	BaseVersion         string                                  `json:"base_version,omitempty"`
	GeneratedAt         time.Time                               `json:"generated_at,omitempty"`
	ExplanationText     string                                  `json:"explanation_text,omitempty"`
	NotificationSummary string                                  `json:"notification_summary,omitempty"`
	FallbackUsed        bool                                    `json:"fallback_used,omitempty"`
}

// CreatePreviewResponse 是写入 preview 后可直接返回给 API 的响应 DTO。
type CreatePreviewResponse struct {
	Detail ActiveSchedulePreviewDetail `json:"detail"`
}

// GetPreviewRequest 是查询 preview 详情的请求 DTO。
//
// 职责边界：
// 1. UserID 来自鉴权上下文，不能信任前端透传；
// 2. PreviewID 来自路由参数；
// 3. 查询只返回预览快照，不执行过期状态回写、不触发 apply。
type GetPreviewRequest struct {
	UserID    int    `json:"user_id"`
	PreviewID string `json:"preview_id"`
}

// ActiveSchedulePreviewDetail 是主动调度预览详情页响应 DTO。
//
// 职责边界：
// 1. 负责把 active_schedule_previews 中的 JSON 快照还原成前端可展示结构；
// 2. 不包含正式写日程能力，不代表 confirm 请求已通过校验；
// 3. CanConfirm 只表达当前快照状态可发起确认，最终是否能应用仍由 confirm/apply 链路重校验。
type ActiveSchedulePreviewDetail struct {
	PreviewID         string                     `json:"preview_id"`
	Status            string                     `json:"status"`
	ApplyStatus       string                     `json:"apply_status"`
	ExpiresAt         time.Time                  `json:"expires_at"`
	GeneratedAt       time.Time                  `json:"generated_at"`
	Expired           bool                       `json:"expired"`
	Trigger           PreviewTriggerDTO          `json:"trigger"`
	Explanation       string                     `json:"explanation"`
	Notification      string                     `json:"notification_summary"`
	SelectedCandidate CandidateDTO               `json:"selected_candidate"`
	Candidates        []CandidateDTO             `json:"candidates"`
	Decision          observe.Decision           `json:"decision"`
	Metrics           observe.Metrics            `json:"metrics"`
	Issues            []observe.Issue            `json:"issues"`
	ContextSummary    ContextSummaryDTO          `json:"context_summary"`
	Before            SchedulePreviewVersion     `json:"before"`
	After             SchedulePreviewVersion     `json:"after"`
	Changes           []ActiveScheduleChangeItem `json:"changes"`
	Risk              RiskDTO                    `json:"risk"`
	BaseVersion       string                     `json:"base_version"`
	CanConfirm        bool                       `json:"can_confirm"`
	CanIgnore         bool                       `json:"can_ignore"`
	TraceID           string                     `json:"trace_id"`
}

type PreviewTriggerDTO struct {
	TriggerID   string    `json:"trigger_id"`
	TriggerType string    `json:"trigger_type"`
	Source      string    `json:"source"`
	TargetType  string    `json:"target_type"`
	TargetID    int       `json:"target_id"`
	RequestedAt time.Time `json:"requested_at"`
}

type CandidateDTO struct {
	CandidateID   string                     `json:"candidate_id"`
	CandidateType string                     `json:"candidate_type"`
	Title         string                     `json:"title"`
	Summary       string                     `json:"summary"`
	Target        CandidateTargetDTO         `json:"target"`
	Changes       []ActiveScheduleChangeItem `json:"changes"`
	BeforeSummary string                     `json:"before_summary"`
	AfterSummary  string                     `json:"after_summary"`
	Risk          string                     `json:"risk"`
	Score         int                        `json:"score"`
	Validation    candidate.Validation       `json:"validation"`
	Source        string                     `json:"source"`
}

type CandidateTargetDTO struct {
	TargetType string `json:"target_type"`
	TargetID   int    `json:"target_id"`
	Title      string `json:"title"`
}

type ContextSummaryDTO struct {
	UserID        int       `json:"user_id"`
	Timezone      string    `json:"timezone"`
	TriggerSource string    `json:"trigger_source"`
	RequestedAt   time.Time `json:"requested_at"`
	WindowStart   time.Time `json:"window_start"`
	WindowEnd     time.Time `json:"window_end"`
	WindowReason  string    `json:"window_reason"`
	TargetType    string    `json:"target_type"`
	TargetID      int       `json:"target_id"`
	TargetTitle   string    `json:"target_title"`
	MissingInfo   []string  `json:"missing_info"`
	TraceSteps    []string  `json:"trace_steps"`
	Warnings      []string  `json:"warnings"`
}

type SchedulePreviewVersion struct {
	Title        string                 `json:"title"`
	WindowStart  time.Time              `json:"window_start"`
	WindowEnd    time.Time              `json:"window_end"`
	Entries      []SchedulePreviewEntry `json:"entries"`
	SummaryLines []string               `json:"summary_lines"`
}

type SchedulePreviewEntry struct {
	EntryID     string    `json:"entry_id"`
	SourceType  string    `json:"source_type"`
	SourceID    int       `json:"source_id"`
	Title       string    `json:"title"`
	StartAt     time.Time `json:"start_at,omitempty"`
	EndAt       time.Time `json:"end_at,omitempty"`
	Week        int       `json:"week,omitempty"`
	DayOfWeek   int       `json:"day_of_week,omitempty"`
	SectionFrom int       `json:"section_from,omitempty"`
	SectionTo   int       `json:"section_to,omitempty"`
	Status      string    `json:"status"`
	Editable    bool      `json:"editable"`
}

type ActiveScheduleChangeItem struct {
	ChangeID         string            `json:"change_id"`
	ChangeType       string            `json:"change_type"`
	TargetType       string            `json:"target_type"`
	TargetID         int               `json:"target_id"`
	FromSlot         *SlotDTO          `json:"from_slot,omitempty"`
	ToSlot           *SlotSpanDTO      `json:"to_slot,omitempty"`
	DurationSections int               `json:"duration_sections"`
	AffectedEventIDs []int             `json:"affected_event_ids"`
	EditedAllowed    bool              `json:"edited_allowed"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type SlotDTO struct {
	Week      int       `json:"week"`
	DayOfWeek int       `json:"day_of_week"`
	Section   int       `json:"section"`
	StartAt   time.Time `json:"start_at,omitempty"`
	EndAt     time.Time `json:"end_at,omitempty"`
}

type SlotSpanDTO struct {
	Start            SlotDTO `json:"start"`
	End              SlotDTO `json:"end"`
	DurationSections int     `json:"duration_sections"`
}

type RiskDTO struct {
	Level        string               `json:"level"`
	Summary      string               `json:"summary"`
	Validation   candidate.Validation `json:"validation"`
	RiskMetrics  observe.RiskMetrics  `json:"risk_metrics"`
	AffectedIDs  []int                `json:"affected_event_ids"`
	RequiresLLM  bool                 `json:"requires_llm"`
	FallbackUsed bool                 `json:"fallback_used"`
}

// rawPreviewSnapshot 聚合需要写入 active_schedule_previews JSON 字段的快照。
type rawPreviewSnapshot struct {
	selectedCandidate CandidateDTO
	candidates        []CandidateDTO
	decision          observe.Decision
	metrics           observe.Metrics
	issues            []observe.Issue
	contextSummary    ContextSummaryDTO
	before            SchedulePreviewVersion
	changes           []ActiveScheduleChangeItem
	after             SchedulePreviewVersion
	risk              RiskDTO
}

func slotDTO(slot ports.Slot) SlotDTO {
	return SlotDTO{
		Week:      slot.Week,
		DayOfWeek: slot.DayOfWeek,
		Section:   slot.Section,
		StartAt:   slot.StartAt,
		EndAt:     slot.EndAt,
	}
}

func slotSpanDTO(span ports.SlotSpan) SlotSpanDTO {
	return SlotSpanDTO{
		Start:            slotDTO(span.Start),
		End:              slotDTO(span.End),
		DurationSections: span.DurationSections,
	}
}

// decodeJSONField 只负责 preview 包内部 DTO 解码。
//
// 说明：
// 1. 当前任务限制只允许修改 preview 目录，不能把 JSON helper 下沉到公共层；
// 2. 因此这里暂时保留包内小函数，后续若第二个 active_scheduler 子包也需要同类能力，再按 AGENTS 规则抽公共层；
// 3. 解码失败返回原始错误，避免把损坏快照静默展示给用户。
func decodeJSONField[T any](raw *string, fallback T) (T, error) {
	if raw == nil || *raw == "" {
		return fallback, nil
	}
	var value T
	if err := json.Unmarshal([]byte(*raw), &value); err != nil {
		return fallback, err
	}
	return value, nil
}
