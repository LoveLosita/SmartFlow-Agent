package kafka

import (
	"context"
	"errors"

	segmentkafka "github.com/segmentio/kafka-go"
)

type Consumer struct {
	reader *segmentkafka.Reader
}

func NewConsumer(cfg Config) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers 未配置")
	}
	reader := segmentkafka.NewReader(segmentkafka.ReaderConfig{
		Brokers:        cfg.Brokers,
		Topic:          cfg.Topic,
		GroupID:        cfg.GroupID,
		MinBytes:       1,
		MaxBytes:       10e6,
		CommitInterval: 0,
		StartOffset:    segmentkafka.FirstOffset,
	})
	return &Consumer{reader: reader}, nil
}

// Dequeue 从 Kafka 拉取一条消息（不自动提交 offset）。
func (c *Consumer) Dequeue(ctx context.Context) (segmentkafka.Message, error) {
	if c == nil || c.reader == nil {
		return segmentkafka.Message{}, errors.New("kafka consumer 未初始化")
	}
	return c.reader.FetchMessage(ctx)
}

func (c *Consumer) Commit(ctx context.Context, msg segmentkafka.Message) error {
	if c == nil || c.reader == nil {
		return errors.New("kafka consumer 未初始化")
	}
	return c.reader.CommitMessages(ctx, msg)
}

func (c *Consumer) Close() error {
	if c == nil || c.reader == nil {
		return nil
	}
	return c.reader.Close()
}
