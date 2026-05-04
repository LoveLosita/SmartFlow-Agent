package activescheduler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ActiveScheduleRequest 是 gateway 调用 active-scheduler 的通用触发请求。
//
// 职责边界：
// 1. 只承载 dry-run / trigger / preview 三个入口共享的触发事实；
// 2. user_id 由 gateway 从 JWT 上下文补齐，不信任前端传入；
// 3. payload 保留原始 JSON，由服务侧按 trigger_type 再解释，避免 gateway 承担领域解析。
type ActiveScheduleRequest struct {
	UserID         int             `json:"user_id,omitempty"`
	TriggerType    string          `json:"trigger_type" binding:"required"`
	TargetType     string          `json:"target_type" binding:"required"`
	TargetID       int             `json:"target_id"`
	FeedbackID     string          `json:"feedback_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	MockNow        *time.Time      `json:"mock_now,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// TriggerResponse 是正式触发写入后的跨进程响应。
type TriggerResponse struct {
	TriggerID string  `json:"trigger_id"`
	Status    string  `json:"status"`
	PreviewID *string `json:"preview_id,omitempty"`
	DedupeHit bool    `json:"dedupe_hit"`
	TraceID   string  `json:"trace_id,omitempty"`
}

// GetPreviewRequest 是查询主动调度预览详情的跨进程请求。
type GetPreviewRequest struct {
	UserID    int    `json:"user_id,omitempty"`
	PreviewID string `json:"preview_id"`
}

// ConfirmPreviewRequest 是确认主动调度预览的跨进程请求。
//
// 职责边界：
// 1. edited_changes 保留原始 JSON，由 active-scheduler 服务侧反序列化为 apply.Change；
// 2. gateway 只补 user_id / preview_id，不理解正式写库命令；
// 3. action / idempotency_key 继续保持现有前端请求语义。
type ConfirmPreviewRequest struct {
	PreviewID      string          `json:"preview_id,omitempty"`
	UserID         int             `json:"user_id,omitempty"`
	CandidateID    string          `json:"candidate_id"`
	Action         string          `json:"action"`
	EditedChanges  json.RawMessage `json:"edited_changes,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	RequestedAt    time.Time       `json:"requested_at,omitempty"`
	TraceID        string          `json:"trace_id,omitempty"`
}

type ApplyErrorCode string

const (
	ApplyErrorCodeExpired               ApplyErrorCode = "expired"
	ApplyErrorCodeIdempotencyConflict   ApplyErrorCode = "idempotency_conflict"
	ApplyErrorCodeBaseVersionChanged    ApplyErrorCode = "base_version_changed"
	ApplyErrorCodeTargetNotFound        ApplyErrorCode = "target_not_found"
	ApplyErrorCodeTargetCompleted       ApplyErrorCode = "target_completed"
	ApplyErrorCodeTargetAlreadySchedule ApplyErrorCode = "target_already_scheduled"
	ApplyErrorCodeSlotConflict          ApplyErrorCode = "slot_conflict"
	ApplyErrorCodeInvalidEditedChanges  ApplyErrorCode = "invalid_edited_changes"
	ApplyErrorCodeUnsupportedChangeType ApplyErrorCode = "unsupported_change_type"
	ApplyErrorCodeDBError               ApplyErrorCode = "db_error"
	ApplyErrorCodeInvalidRequest        ApplyErrorCode = "invalid_request"
	ApplyErrorCodeForbidden             ApplyErrorCode = "forbidden"
	ApplyErrorCodeAlreadyApplied        ApplyErrorCode = "already_applied"
)

const (
	ApplyStatusRejected = "rejected"
	ApplyStatusExpired  = "expired"
	ApplyStatusFailed   = "failed"
)

// ApplyError 是 active-scheduler confirm/apply 业务拒绝的跨进程错误。
//
// 职责边界：
// 1. 只承载可映射到 HTTP 4xx/5xx 的错误码与展示文案；
// 2. 不暴露服务内部 applyadapter、DAO 或事务错误类型；
// 3. cause 仅用于 gateway 日志串联，响应体只使用 code/message。
type ApplyError struct {
	Code    ApplyErrorCode
	Message string
	Cause   error
}

func (e *ApplyError) Error() string {
	if e == nil {
		return ""
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, message)
}

func (e *ApplyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// ConfirmErrorResult 是 confirm/apply 失败时仍返回给前端的稳定数据形状。
type ConfirmErrorResult struct {
	ApplyStatus  string         `json:"apply_status"`
	ErrorCode    ApplyErrorCode `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
}
