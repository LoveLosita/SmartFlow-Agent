package kafka

import (
	"context"
	"errors"

	segmentkafka "github.com/segmentio/kafka-go"
)

// Consumer 是 Kafka 读取端封装。
// 采用“手动提交 offset”，确保业务落库与 offset 提交的顺序可控。
type Consumer struct {
	reader *segmentkafka.Reader
}

func NewConsumer(cfg Config) (*Consumer, error) {
	if len(cfg.Brokers) == 0 {
		return nil, errors.New("kafka brokers 未配置")
	}
	reader := segmentkafka.NewReader(segmentkafka.ReaderConfig{
		Brokers:  cfg.Brokers,
		Topic:    cfg.Topic,
		GroupID:  cfg.GroupID,
		MinBytes: 1,
		MaxBytes: 10e6,
		// 关闭自动提交，业务处理成功后显式 Commit。
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

// Commit 显式提交 offset。
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
