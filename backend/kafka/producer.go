package kafka

import (
	"context"
	"errors"

	segmentkafka "github.com/segmentio/kafka-go"
)

type Producer struct {
	writer *segmentkafka.Writer
}

func NewProducer(cfg Config) (*Producer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers 未配置")
	}
	writer := &segmentkafka.Writer{
		Addr:         segmentkafka.TCP(cfg.Brokers...),
		Balancer:     &segmentkafka.Hash{},
		RequiredAcks: segmentkafka.RequireOne,
		Async:        false,
	}
	return &Producer{writer: writer}, nil
}

// Enqueue 将消息写入 Kafka。
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
