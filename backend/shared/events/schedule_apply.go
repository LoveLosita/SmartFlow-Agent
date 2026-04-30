package events

import (
	"errors"
	"strconv"
	"strings"
)

const (
	ScheduleApplySucceededEventType = "schedule.apply.succeeded"
	ScheduleApplyFailedEventType    = "schedule.apply.failed"
	ScheduleApplyResultEventVersion = "1"
)

// ScheduleApplyResultPayload 是主动调度确认应用的结果事件载荷。
//
// 职责边界：
// 1. 只描述 apply 成功或失败的结果事实，不表达 apply 请求；
// 2. 不复用 preview DB model，也不携带完整变更明细；
// 3. MVP 第一版 confirm 同步执行，是否发布该事件由后续接入点决定。
type ScheduleApplyResultPayload struct {
	PreviewID       string `json:"preview_id"`
	ApplyID         string `json:"apply_id"`
	UserID          int    `json:"user_id"`
	TriggerID       string `json:"trigger_id,omitempty"`
	CandidateID     string `json:"candidate_id,omitempty"`
	AppliedEventIDs []int  `json:"applied_event_ids,omitempty"`
	ApplyStatus     string `json:"apply_status"`
	ErrorCode       string `json:"error_code,omitempty"`
	TraceID         string `json:"trace_id,omitempty"`
}

// ValidateForEvent 按具体事件类型校验 apply 结果载荷。
//
// 职责边界：
// 1. succeeded 要求至少包含一个正式日程 event id；
// 2. failed 要求 error_code，方便排障和前端提示映射；
// 3. 不校验 event id 是否存在，正式落库事务负责保证。
func (p ScheduleApplyResultPayload) ValidateForEvent(eventType string) error {
	if strings.TrimSpace(p.PreviewID) == "" {
		return errors.New("preview_id 不能为空")
	}
	if strings.TrimSpace(p.ApplyID) == "" {
		return errors.New("apply_id 不能为空")
	}
	if p.UserID <= 0 {
		return errors.New("user_id 必须大于 0")
	}
	switch strings.TrimSpace(eventType) {
	case ScheduleApplySucceededEventType:
		if len(p.AppliedEventIDs) == 0 {
			return errors.New("schedule.apply.succeeded 必须包含 applied_event_ids")
		}
	case ScheduleApplyFailedEventType:
		if strings.TrimSpace(p.ErrorCode) == "" {
			return errors.New("schedule.apply.failed 必须包含 error_code")
		}
	default:
		return errors.New("未知的 schedule apply 结果事件类型")
	}
	return nil
}

func (p ScheduleApplyResultPayload) MessageKey() string {
	if p.UserID <= 0 {
		return ""
	}
	return strconv.Itoa(p.UserID)
}

func (p ScheduleApplyResultPayload) AggregateID() string {
	if strings.TrimSpace(p.ApplyID) != "" {
		return strings.TrimSpace(p.ApplyID)
	}
	return strings.TrimSpace(p.PreviewID)
}
