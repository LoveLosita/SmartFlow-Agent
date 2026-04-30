package apply

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type ConfirmAction string

const (
	ConfirmActionConfirm ConfirmAction = "confirm"
)

type ChangeType string

const (
	ChangeTypeAddTaskPoolToSchedule       ChangeType = "add_task_pool_to_schedule"
	ChangeTypeCreateMakeup                ChangeType = "create_makeup"
	ChangeTypeAskUser                     ChangeType = "ask_user"
	ChangeTypeNotifyOnly                  ChangeType = "notify_only"
	ChangeTypeClose                       ChangeType = "close"
	ChangeTypeCompressWithNextDynamicTask ChangeType = "compress_with_next_dynamic_task"
)

type CommandType string

const (
	CommandTypeInsertTaskPoolEvent CommandType = "insert_task_pool_event"
	CommandTypeInsertMakeupEvent   CommandType = "insert_makeup_event"
)

type ApplyStatus string

const (
	ApplyStatusNone     ApplyStatus = "none"
	ApplyStatusApplying ApplyStatus = "applying"
	ApplyStatusApplied  ApplyStatus = "applied"
	ApplyStatusFailed   ApplyStatus = "failed"
	ApplyStatusRejected ApplyStatus = "rejected"
	ApplyStatusExpired  ApplyStatus = "expired"
)

type ErrorCode string

const (
	ErrorCodeExpired               ErrorCode = "expired"
	ErrorCodeIdempotencyConflict   ErrorCode = "idempotency_conflict"
	ErrorCodeBaseVersionChanged    ErrorCode = "base_version_changed"
	ErrorCodeTargetNotFound        ErrorCode = "target_not_found"
	ErrorCodeTargetCompleted       ErrorCode = "target_completed"
	ErrorCodeTargetAlreadySchedule ErrorCode = "target_already_scheduled"
	ErrorCodeSlotConflict          ErrorCode = "slot_conflict"
	ErrorCodeInvalidEditedChanges  ErrorCode = "invalid_edited_changes"
	ErrorCodeUnsupportedChangeType ErrorCode = "unsupported_change_type"
	ErrorCodeDBError               ErrorCode = "db_error"
	ErrorCodeInvalidRequest        ErrorCode = "invalid_request"
	ErrorCodeForbidden             ErrorCode = "forbidden"
	ErrorCodeAlreadyApplied        ErrorCode = "already_applied"
)

// ApplyError 表示 confirm/apply 链路可被 API 直接映射的业务错误。
//
// 职责边界：
// 1. 只承载错误分类和可读信息，便于主线程写入 apply_error 或转成 HTTP 响应；
// 2. 不负责决定 preview 状态流转，状态更新仍由接入层或后续 preview repo 完成；
// 3. Err 保留底层错误，Error() 返回中文消息，便于日志排障。
type ApplyError struct {
	Code    ErrorCode
	Message string
	Err     error
}

func (e *ApplyError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return string(e.Code)
}

func (e *ApplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newApplyError(code ErrorCode, message string, err error) error {
	return &ApplyError{Code: code, Message: message, Err: err}
}

func errorCodeOf(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var applyErr *ApplyError
	if errors.As(err, &applyErr) {
		return applyErr.Code
	}
	return ErrorCodeDBError
}

// Slot 是 confirm 请求与 apply command 之间共享的最小节次坐标。
//
// 职责边界：
// 1. 只表达 week/day_of_week/section，不绑定 schedules 表；
// 2. 不负责相对时间到绝对时间的转换，该转换由 apply port/adapter 完成；
// 3. IsZero 用于识别前端未传坐标或候选 JSON 缺字段的情况。
type Slot struct {
	Week      int `json:"week"`
	DayOfWeek int `json:"day_of_week"`
	Section   int `json:"section"`
}

func (s Slot) IsZero() bool {
	return s.Week == 0 && s.DayOfWeek == 0 && s.Section == 0
}

// SlotSpan 表示一段连续节次，供转换器展开为正式写入命令。
type SlotSpan struct {
	Start            Slot `json:"start"`
	End              Slot `json:"end"`
	DurationSections int  `json:"duration_sections"`
}

// ApplyChange 是 confirm 请求和候选转换后的统一 change DTO。
//
// 职责边界：
// 1. 表达用户最终确认的结构化变更，可来自 preview 原候选或 edited_changes；
// 2. 不承载数据库模型，也不表示已经真实落库；
// 3. Type 决定是否可转换为正式写入命令，ask_user/notify_only/close 会被保留为跳过项。
type ApplyChange struct {
	ChangeID         string            `json:"change_id,omitempty"`
	Type             ChangeType        `json:"type"`
	TargetType       string            `json:"target_type,omitempty"`
	TargetID         int               `json:"target_id,omitempty"`
	TaskID           int               `json:"task_id,omitempty"`
	EventID          int               `json:"event_id,omitempty"`
	Week             int               `json:"week,omitempty"`
	DayOfWeek        int               `json:"day_of_week,omitempty"`
	SectionFrom      int               `json:"section_from,omitempty"`
	SectionTo        int               `json:"section_to,omitempty"`
	DurationSections int               `json:"duration_sections,omitempty"`
	MakeupForEventID int               `json:"makeup_for_event_id,omitempty"`
	SourceEventID    int               `json:"source_event_id,omitempty"`
	Slots            []Slot            `json:"slots,omitempty"`
	EditedAllowed    bool              `json:"edited_allowed,omitempty"`
	Metadata         map[string]string `json:"metadata,omitempty"`
	NormalizedHash   string            `json:"normalized_hash,omitempty"`
}

// ConfirmRequest 是主动调度详情页提交确认时的入口 DTO。
//
// 职责边界：
// 1. PreviewID 可由路由层补齐，body 内没有 preview_id 时也能参与转换；
// 2. EditedChanges 为空时，转换器会回退使用 preview 中 candidate 的原始 changes；
// 3. IdempotencyKey 只代表一次确认动作，不代表 candidate 身份。
type ConfirmRequest struct {
	PreviewID      string        `json:"preview_id,omitempty"`
	UserID         int           `json:"user_id,omitempty"`
	CandidateID    string        `json:"candidate_id"`
	Action         ConfirmAction `json:"action"`
	EditedChanges  []ApplyChange `json:"edited_changes,omitempty"`
	IdempotencyKey string        `json:"idempotency_key"`
	RequestedAt    time.Time     `json:"requested_at,omitempty"`
	TraceID        string        `json:"trace_id,omitempty"`
}

type ApplyCommand struct {
	CommandType   CommandType       `json:"command_type"`
	ChangeID      string            `json:"change_id,omitempty"`
	ChangeType    ChangeType        `json:"change_type"`
	TargetType    string            `json:"target_type"`
	TargetID      int               `json:"target_id"`
	Slots         []Slot            `json:"slots"`
	SourceEventID int               `json:"source_event_id,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

type SkippedChange struct {
	ChangeID   string     `json:"change_id,omitempty"`
	ChangeType ChangeType `json:"change_type"`
	Reason     string     `json:"reason"`
}

// ApplyActiveScheduleRequest 是传给正式写入 port 的请求 DTO。
//
// 职责边界：
// 1. 只描述已完成 preview/candidate 转换和基础校验后的写入意图；
// 2. 不直接执行 schedules 写入，真正事务由 ScheduleApplyPort/adapter 负责；
// 3. RequestHash 用于 preview_id + idempotency_key + body_hash 的幂等识别。
type ApplyActiveScheduleRequest struct {
	PreviewID             string          `json:"preview_id"`
	ApplyID               string          `json:"apply_id"`
	IdempotencyKey        string          `json:"idempotency_key"`
	RequestHash           string          `json:"request_hash"`
	RequestBodyHash       string          `json:"request_body_hash"`
	UserID                int             `json:"user_id"`
	CandidateID           string          `json:"candidate_id"`
	BaseVersion           string          `json:"base_version"`
	Changes               []ApplyChange   `json:"changes"`
	Commands              []ApplyCommand  `json:"commands"`
	SkippedChanges        []SkippedChange `json:"skipped_changes,omitempty"`
	NormalizedChangesHash string          `json:"normalized_changes_hash"`
	RequestedAt           time.Time       `json:"requested_at"`
	TraceID               string          `json:"trace_id,omitempty"`
}

type ApplyActiveScheduleResult struct {
	ApplyID              string          `json:"apply_id"`
	ApplyStatus          ApplyStatus     `json:"apply_status"`
	AppliedEventIDs      []int           `json:"applied_event_ids,omitempty"`
	AppliedScheduleIDs   []int           `json:"applied_schedule_ids,omitempty"`
	AppliedChanges       []ApplyChange   `json:"applied_changes,omitempty"`
	SkippedChanges       []SkippedChange `json:"skipped_changes,omitempty"`
	WarningMessages      []string        `json:"warning_messages,omitempty"`
	ErrorCode            ErrorCode       `json:"error_code,omitempty"`
	ErrorMessage         string          `json:"error_message,omitempty"`
	RequestHash          string          `json:"request_hash,omitempty"`
	NormalizedChangeHash string          `json:"normalized_change_hash,omitempty"`
}

type ConfirmResult struct {
	PreviewID       string                      `json:"preview_id"`
	ApplyID         string                      `json:"apply_id"`
	ApplyStatus     ApplyStatus                 `json:"apply_status"`
	CandidateID     string                      `json:"candidate_id"`
	RequestHash     string                      `json:"request_hash,omitempty"`
	RequestBodyHash string                      `json:"request_body_hash,omitempty"`
	ApplyRequest    *ApplyActiveScheduleRequest `json:"apply_request,omitempty"`
	ApplyResult     *ApplyActiveScheduleResult  `json:"apply_result,omitempty"`
	SkippedChanges  []SkippedChange             `json:"skipped_changes,omitempty"`
	ErrorCode       ErrorCode                   `json:"error_code,omitempty"`
	ErrorMessage    string                      `json:"error_message,omitempty"`
}

// ScheduleApplyPort 是主动调度 apply 层唯一允许调用的正式写入端口。
//
// 职责边界：
// 1. 负责在事务内重读 task/schedule/task_item 真值并写入正式日程；
// 2. 负责返回真实 applied_changes/applied_event_ids，而不是候选原始内容；
// 3. apply 包本身不直接 import DAO 写 schedules，避免绕过既有领域能力。
type ScheduleApplyPort interface {
	ApplyActiveScheduleChanges(ctx context.Context, req ApplyActiveScheduleRequest) (ApplyActiveScheduleResult, error)
}
