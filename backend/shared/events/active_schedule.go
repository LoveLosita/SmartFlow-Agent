package events

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	ActiveScheduleTriggeredEventType    = "active_schedule.triggered"
	ActiveScheduleTriggeredEventVersion = "1"
)

const (
	ActiveScheduleTriggerTypeImportantUrgentTask = "important_urgent_task"
	ActiveScheduleTriggerTypeUnfinishedFeedback  = "unfinished_feedback"

	ActiveScheduleSourceWorkerDueJob = "worker_due_job"
	ActiveScheduleSourceAPITrigger   = "api_trigger"
	ActiveScheduleSourceAPIDryRun    = "api_dry_run"
	ActiveScheduleSourceUserFeedback = "user_feedback"

	ActiveScheduleTargetTypeTaskPool      = "task_pool"
	ActiveScheduleTargetTypeScheduleEvent = "schedule_event"
	ActiveScheduleTargetTypeTaskItem      = "task_item"
)

// ActiveScheduleTriggeredPayload 是 active_schedule.triggered 的事件载荷。
//
// 职责边界：
// 1. 只描述“主动调度链路需要处理一个触发信号”这一事件事实；
// 2. 不复用 GORM model，也不承载候选生成、预览写入或通知投递逻辑；
// 3. Payload 只保留触发源补充 JSON，消费方需要按自身 DTO 再解析。
type ActiveScheduleTriggeredPayload struct {
	TriggerID      string          `json:"trigger_id"`
	UserID         int             `json:"user_id"`
	TriggerType    string          `json:"trigger_type"`
	Source         string          `json:"source"`
	TargetType     string          `json:"target_type"`
	TargetID       int             `json:"target_id"`
	FeedbackID     string          `json:"feedback_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	DedupeKey      string          `json:"dedupe_key,omitempty"`
	MockNow        *time.Time      `json:"mock_now,omitempty"`
	IsMockTime     bool            `json:"is_mock_time"`
	RequestedAt    time.Time       `json:"requested_at"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	TraceID        string          `json:"trace_id,omitempty"`
}

// Validate 校验事件契约必填字段与第一版枚举范围。
//
// 职责边界：
// 1. 只做协议级基础校验，避免无效事件进入 worker；
// 2. 不检查 target 是否存在、是否归属用户，这些属于业务读模型责任；
// 3. dry-run 不应发布该事件，因此 source=api_dry_run 会被拒绝。
func (p ActiveScheduleTriggeredPayload) Validate() error {
	if strings.TrimSpace(p.TriggerID) == "" {
		return errors.New("trigger_id 不能为空")
	}
	if p.UserID <= 0 {
		return errors.New("user_id 必须大于 0")
	}
	if !isAllowedActiveScheduleTriggerType(p.TriggerType) {
		return errors.New("trigger_type 不在主动调度第一版允许范围内")
	}
	if !isAllowedActiveScheduleSource(p.Source) {
		return errors.New("source 不在主动调度第一版允许范围内")
	}
	if p.Source == ActiveScheduleSourceAPIDryRun {
		return errors.New("api_dry_run 不允许发布 active_schedule.triggered")
	}
	if !isAllowedActiveScheduleTargetType(p.TargetType) {
		return errors.New("target_type 不在主动调度第一版允许范围内")
	}
	if p.TargetID <= 0 {
		return errors.New("target_id 必须大于 0")
	}
	if p.RequestedAt.IsZero() {
		return errors.New("requested_at 不能为空")
	}
	if p.MockNow != nil && !p.IsMockTime {
		return errors.New("mock_now 非空时必须标记 is_mock_time=true")
	}
	return nil
}

// MessageKey 返回 outbox/Kafka 消息键。
//
// 说明：
// 1. 按文档约定使用 user_id，便于同一用户事件在消费侧保持局部有序；
// 2. 只做字符串构造，不访问数据库。
func (p ActiveScheduleTriggeredPayload) MessageKey() string {
	if p.UserID <= 0 {
		return ""
	}
	return strconv.Itoa(p.UserID)
}

// AggregateID 返回事件聚合 ID。
//
// 说明：
// 1. active_schedule.triggered 的聚合主键是 trigger_id；
// 2. 若 trigger_id 为空，返回空字符串，由发布方在 Validate 前发现问题。
func (p ActiveScheduleTriggeredPayload) AggregateID() string {
	return strings.TrimSpace(p.TriggerID)
}

func isAllowedActiveScheduleTriggerType(value string) bool {
	switch strings.TrimSpace(value) {
	case ActiveScheduleTriggerTypeImportantUrgentTask, ActiveScheduleTriggerTypeUnfinishedFeedback:
		return true
	default:
		return false
	}
}

func isAllowedActiveScheduleSource(value string) bool {
	switch strings.TrimSpace(value) {
	case ActiveScheduleSourceWorkerDueJob, ActiveScheduleSourceAPITrigger, ActiveScheduleSourceAPIDryRun, ActiveScheduleSourceUserFeedback:
		return true
	default:
		return false
	}
}

func isAllowedActiveScheduleTargetType(value string) bool {
	switch strings.TrimSpace(value) {
	case ActiveScheduleTargetTypeTaskPool, ActiveScheduleTargetTypeScheduleEvent, ActiveScheduleTargetTypeTaskItem:
		return true
	default:
		return false
	}
}
