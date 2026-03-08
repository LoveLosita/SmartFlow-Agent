package kafka

import "encoding/json"

// Envelope 是投递到 Kafka 的统一包裹结构。
type Envelope struct {
	OutboxID int64           `json:"outbox_id"`
	BizType  string          `json:"biz_type"`
	Payload  json.RawMessage `json:"payload"`
}
