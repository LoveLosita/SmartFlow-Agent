package events

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

const (
	NotificationFeishuRequestedEventType    = "notification.feishu.requested"
	NotificationFeishuRequestedEventVersion = "1"
)

// FeishuNotificationRequestedPayload 是飞书通知请求事件载荷。
//
// 职责边界：
// 1. 只描述“需要尝试发送一条飞书提醒”的跨模块协议；
// 2. 不包含 provider SDK 参数，也不复用 notification_records 的 GORM model；
// 3. 不决定是否真正投递，去重、配置关闭和重试由 notification 模块处理。
type FeishuNotificationRequestedPayload struct {
	NotificationID int64     `json:"notification_id,omitempty"`
	UserID         int       `json:"user_id"`
	TriggerID      string    `json:"trigger_id"`
	PreviewID      string    `json:"preview_id"`
	TriggerType    string    `json:"trigger_type"`
	TargetType     string    `json:"target_type"`
	TargetID       int       `json:"target_id"`
	DedupeKey      string    `json:"dedupe_key"`
	TargetURL      string    `json:"target_url"`
	SummaryText    string    `json:"summary_text,omitempty"`
	FallbackText   string    `json:"fallback_text,omitempty"`
	TraceID        string    `json:"trace_id,omitempty"`
	RequestedAt    time.Time `json:"requested_at"`
}

// Validate 校验飞书通知事件的协议级必填字段。
//
// 职责边界：
// 1. 只保证通知 handler 能定位用户、预览和去重键；
// 2. 不校验用户是否绑定飞书，也不调用 provider；
// 3. target_url 必须是站内相对路径，避免事件载荷携带任意外部跳转。
func (p FeishuNotificationRequestedPayload) Validate() error {
	if p.UserID <= 0 {
		return errors.New("user_id 必须大于 0")
	}
	if strings.TrimSpace(p.PreviewID) == "" {
		return errors.New("preview_id 不能为空")
	}
	if strings.TrimSpace(p.DedupeKey) == "" {
		return errors.New("dedupe_key 不能为空")
	}
	targetURL := strings.TrimSpace(p.TargetURL)
	if targetURL == "" {
		return errors.New("target_url 不能为空")
	}
	if !strings.HasPrefix(targetURL, "/assistant/") {
		return errors.New("target_url 必须是 /assistant/{conversation_id} 站内相对路径")
	}
	if strings.Contains(targetURL, "://") || strings.HasPrefix(targetURL, "//") {
		return errors.New("target_url 不允许携带外部链接")
	}
	if p.RequestedAt.IsZero() {
		return errors.New("requested_at 不能为空")
	}
	return nil
}

// MessageKey 返回 outbox/Kafka 消息键。
func (p FeishuNotificationRequestedPayload) MessageKey() string {
	if p.UserID <= 0 {
		return ""
	}
	return strconv.Itoa(p.UserID)
}

// AggregateID 返回事件聚合 ID。
//
// 说明：notification.feishu.requested 使用 preview_id 串联通知与预览。
func (p FeishuNotificationRequestedPayload) AggregateID() string {
	return strings.TrimSpace(p.PreviewID)
}
