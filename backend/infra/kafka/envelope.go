package kafka

import "encoding/json"

// Envelope 是 outbox 投递到 Kafka 的统一包裹结构。
// 设计目的：
// 1) 消费端先拿到 outbox_id，可直接回写状态；
// 2) biz_type 做分发，支持后续扩展更多异步业务；
// 3) payload 保持原始 JSON，按业务类型再反序列化。
type Envelope struct {
	OutboxID int64           `json:"outbox_id"`
	BizType  string          `json:"biz_type"`
	Payload  json.RawMessage `json:"payload"`
}
