package kafka

import "encoding/json"

// Envelope 是 outbox 投递到 Kafka 的统一协议包。
//
// 协议边界：
// 1. 这是总线协议，不包含具体业务字段；
// 2. 路由只依赖 event_type，不再保留 biz_type 兼容字段；
// 3. payload 为原始业务 JSON，由业务 handler 决定如何反序列化。
type Envelope struct {
	// OutboxID 是 outbox 状态机主键，用于消费者回写 consumed/retry/dead。
	OutboxID int64 `json:"outbox_id"`
	// EventID 是事件唯一标识（当前默认回退为 outbox_id 字符串）。
	EventID string `json:"event_id,omitempty"`

	// EventType 是唯一路由键（例如 chat.history.persist.requested）。
	EventType string `json:"event_type"`
	// EventVersion 是事件版本号（默认 v1）。
	EventVersion string `json:"event_version,omitempty"`
	// AggregateID 是聚合主键（例如 conversation_id），用于追踪同一业务对象事件流。
	AggregateID string `json:"aggregate_id,omitempty"`

	// Payload 是业务载荷 JSON。
	Payload json.RawMessage `json:"payload"`
}
