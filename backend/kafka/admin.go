package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	segmentkafka "github.com/segmentio/kafka-go"
)

// WaitTopicReady 在指定超时时间内等待 Kafka topic 可用。
// 背景：初次部署时 broker 可能已启动，但 topic/partition 还没就绪。
// 这里启动前先探测，可减少“应用已启动但实际无法消费”的静默窗口。
func WaitTopicReady(parent context.Context, brokers []string, topic string, timeout time.Duration) error {
	if len(brokers) == 0 {
		return errors.New("kafka brokers is empty")
	}
	if topic == "" {
		return errors.New("kafka topic is empty")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastErr error
	for {
		if err := probeTopic(ctx, brokers, topic); err == nil {
			return nil
		} else {
			lastErr = err
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("wait topic ready timeout, topic=%s: %w", topic, lastErr)
			}
			return fmt.Errorf("wait topic ready timeout, topic=%s", topic)
		case <-ticker.C:
		}
	}
}

// probeTopic 轮询所有 broker，只要任一 broker 能读到 topic 分区信息即视为就绪。
func probeTopic(ctx context.Context, brokers []string, topic string) error {
	var lastErr error
	for _, broker := range brokers {
		conn, err := segmentkafka.DialContext(ctx, "tcp", broker)
		if err != nil {
			lastErr = err
			continue
		}

		partitions, readErr := conn.ReadPartitions(topic)
		_ = conn.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if len(partitions) == 0 {
			lastErr = fmt.Errorf("topic %s has no partitions yet", topic)
			continue
		}
		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return errors.New("unable to probe topic readiness")
}
