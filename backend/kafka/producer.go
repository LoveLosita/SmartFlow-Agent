package kafka

import (
	"context"
	"errors"

	segmentkafka "github.com/segmentio/kafka-go"
)

// Producer 是 Kafka 写入端封装。
// 这里保持同步写（Async=false），方便把写入结果直接反馈给 outbox 状态机。
type Producer struct {
	writer *segmentkafka.Writer
}

func NewProducer(cfg Config) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers 未配置")
	}
	writer := &segmentkafka.Writer{
		Addr: segmentkafka.TCP(cfg.Brokers...),
		// Hash 分区器保证相同 key 落同一分区，利于同会话消息顺序。
		Balancer:     &segmentkafka.Hash{},
		RequiredAcks: segmentkafka.RequireOne,
		// 关闭异步，确保写失败时可立即触发 outbox 重试逻辑。
		Async: false,
	}
	return &Producer{writer: writer}, nil
}

// Enqueue 将消息写入 Kafka。
// 成功仅代表“已被 Kafka 接收”，不代表业务已完成（业务完成由 consumer + 落库决定）。
func (p *Producer) Enqueue(ctx context.Context, topic, key string, value []byte) error {
	if p == nil || p.writer == nil {
		return errors.New("kafka producer 未初始化")
	}
	msg := segmentkafka.Message{
		Topic: topic,
		Key:   []byte(key),
		Value: value,
	}
	return p.writer.WriteMessages(ctx, msg)
}

func (p *Producer) Close() error {
	if p == nil || p.writer == nil {
		return nil
	}
	return p.writer.Close()
}
