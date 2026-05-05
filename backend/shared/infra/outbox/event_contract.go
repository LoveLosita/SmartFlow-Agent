package outbox

import (
	"encoding/json"
	"errors"
	"strings"
)

const (
	// DefaultEventVersion 是通用事件协议默认版本。
	DefaultEventVersion = "v1"
)

// OutboxEventPayload 是 outbox.payload 的统一事件外壳。
type OutboxEventPayload struct {
	EventID      string          `json:"event_id,omitempty"`
	EventType    string          `json:"event_type"`
	EventVersion string          `json:"event_version,omitempty"`
	AggregateID  string          `json:"aggregate_id,omitempty"`
	Payload      json.RawMessage `json:"payload"`
}

// ParsedOutboxEventPayload 是 dispatch 阶段使用的标准化结构。
type ParsedOutboxEventPayload struct {
	EventID      string
	EventType    string
	EventVersion string
	AggregateID  string
	PayloadJSON  json.RawMessage
}

// parseOutboxEventPayload 解析 outbox.payload。
//
// 当前策略（极致清理版）：
// 1. 只接受“统一事件外壳”格式；
// 2. 不再支持旧格式纯业务 JSON 回退；
// 3. event_type 缺失时直接报错，交由上层标 dead。
func parseOutboxEventPayload(rawPayload string) (*ParsedOutboxEventPayload, error) {
	var wrapped OutboxEventPayload
	if err := json.Unmarshal([]byte(rawPayload), &wrapped); err != nil {
		return nil, err
	}

	eventType := strings.TrimSpace(wrapped.EventType)
	if eventType == "" {
		return nil, errors.New("event type is empty")
	}
	if len(wrapped.Payload) == 0 {
		return nil, errors.New("payload is empty")
	}

	eventVersion := strings.TrimSpace(wrapped.EventVersion)
	if eventVersion == "" {
		eventVersion = DefaultEventVersion
	}

	return &ParsedOutboxEventPayload{
		EventID:      strings.TrimSpace(wrapped.EventID),
		EventType:    eventType,
		EventVersion: eventVersion,
		AggregateID:  strings.TrimSpace(wrapped.AggregateID),
		PayloadJSON:  wrapped.Payload,
	}, nil
}
