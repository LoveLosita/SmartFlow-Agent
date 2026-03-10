package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/kafka"
	"github.com/LoveLosita/smartflow/backend/model"
	segmentkafka "github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// AgentAsyncPipeline 负责 outbox 扫描、Kafka 投递与消费落库。
type AgentAsyncPipeline struct {
	outboxRepo *dao.OutboxDAO
	producer   *kafkabus.Producer
	consumer   *kafkabus.Consumer
	brokers    []string
	topic      string
	maxRetry   int
	scanEvery  time.Duration
	scanBatch  int
}

func NewAgentAsyncPipeline(outboxRepo *dao.OutboxDAO, cfg kafkabus.Config) (*AgentAsyncPipeline, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	producer, err := kafkabus.NewProducer(cfg)
	if err != nil {
		return nil, err
	}
	consumer, err := kafkabus.NewConsumer(cfg)
	if err != nil {
		_ = producer.Close()
		return nil, err
	}
	return &AgentAsyncPipeline{
		outboxRepo: outboxRepo,
		producer:   producer,
		consumer:   consumer,
		brokers:    cfg.Brokers,
		topic:      cfg.Topic,
		maxRetry:   cfg.MaxRetry,
		scanEvery:  cfg.RetryScanInterval,
		scanBatch:  cfg.RetryBatchSize,
	}, nil
}

func (p *AgentAsyncPipeline) Start(ctx context.Context) {
	if p == nil {
		return
	}

	log.Printf("Kafka async pipeline starting: topic=%s brokers=%v retry_scan=%s batch=%d", p.topic, p.brokers, p.scanEvery, p.scanBatch)
	if err := kafkabus.WaitTopicReady(ctx, p.brokers, p.topic, 30*time.Second); err != nil {
		log.Printf("Kafka topic not ready before consume loop start: %v", err)
	} else {
		log.Printf("Kafka topic is ready: %s", p.topic)
	}

	go p.startDispatchLoop(ctx)
	go p.startConsumeLoop(ctx)
}

func (p *AgentAsyncPipeline) Close() {
	if p == nil {
		return
	}
	if err := p.producer.Close(); err != nil {
		log.Printf("关闭 Kafka producer 失败: %v", err)
	}
	if err := p.consumer.Close(); err != nil {
		log.Printf("关闭 Kafka consumer 失败: %v", err)
	}
}

// EnqueueChatHistoryPersist 仅把消息写入 outbox。
//
// 关键设计：
// 1) 不再在请求路径里做“首次同步投递 Kafka”；
// 2) 投递统一由 startDispatchLoop 异步扫描执行；
// 3) CreateChatHistoryMessage 会设置 next_retry_at=now，扫描器下一轮即可捞取。
//
// 这样可以把请求链路成本收敛到“写 outbox”，避免 Kafka 写入延迟污染首字和主链路时延。
func (p *AgentAsyncPipeline) EnqueueChatHistoryPersist(ctx context.Context, payload model.ChatHistoryPersistPayload) error {
	if p == nil {
		return errors.New("Kafka 异步链路未初始化")
	}
	_, err := p.outboxRepo.CreateChatHistoryMessage(ctx, p.topic, payload.ConversationID, payload, p.maxRetry)
	return err
}

func (p *AgentAsyncPipeline) startDispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(p.scanEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pendingMessages, err := p.outboxRepo.ListDueMessages(ctx, p.scanBatch)
			if err != nil {
				log.Printf("扫描 outbox 失败: %v", err)
				continue
			}
			if len(pendingMessages) > 0 {
				log.Printf("outbox due messages=%d, start dispatch", len(pendingMessages))
			}
			for _, msg := range pendingMessages {
				if err = p.dispatchOne(ctx, msg.ID); err != nil {
					log.Printf("重试投递 outbox 消息失败(id=%d): %v", msg.ID, err)
				}
			}
		}
	}
}

func (p *AgentAsyncPipeline) dispatchOne(ctx context.Context, outboxID int64) error {
	outboxMsg, err := p.outboxRepo.GetByID(ctx, outboxID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if outboxMsg.Status == model.OutboxStatusConsumed || outboxMsg.Status == model.OutboxStatusDead {
		return nil
	}

	envelope := kafkabus.Envelope{
		OutboxID: outboxMsg.ID,
		BizType:  outboxMsg.BizType,
		Payload:  json.RawMessage(outboxMsg.Payload),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		markErr := p.outboxRepo.MarkDead(ctx, outboxMsg.ID, "序列化 outbox 包装失败: "+err.Error())
		if markErr != nil {
			log.Printf("标记 outbox 死信失败(id=%d): %v", outboxMsg.ID, markErr)
		}
		return err
	}

	if err = p.producer.Enqueue(ctx, outboxMsg.Topic, outboxMsg.MessageKey, raw); err != nil {
		_ = p.outboxRepo.MarkFailedForRetry(ctx, outboxMsg.ID, "投递 Kafka 失败: "+err.Error())
		return err
	}
	if err = p.outboxRepo.MarkPublished(ctx, outboxMsg.ID); err != nil {
		_ = p.outboxRepo.MarkFailedForRetry(ctx, outboxMsg.ID, "更新已投递状态失败: "+err.Error())
		return err
	}
	return nil
}

func (p *AgentAsyncPipeline) startConsumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := p.consumer.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("Kafka 消费拉取失败(topic=%s): %v", p.topic, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		if err = p.handleMessage(ctx, msg); err != nil {
			log.Printf("处理 Kafka 消息失败(topic=%s, partition=%d, offset=%d): %v", msg.Topic, msg.Partition, msg.Offset, err)
		}
	}
}

func (p *AgentAsyncPipeline) handleMessage(ctx context.Context, msg segmentkafka.Message) error {
	var envelope kafkabus.Envelope
	if err := json.Unmarshal(msg.Value, &envelope); err != nil {
		_ = p.consumer.Commit(ctx, msg)
		return fmt.Errorf("解析 Kafka 包装失败: %w", err)
	}
	if envelope.OutboxID <= 0 {
		_ = p.consumer.Commit(ctx, msg)
		return errors.New("Kafka 包装缺少 outbox_id")
	}

	switch envelope.BizType {
	case model.OutboxBizTypeChatHistoryPersist:
		return p.consumeChatHistory(ctx, msg, envelope)
	default:
		_ = p.outboxRepo.MarkDead(ctx, envelope.OutboxID, "未知业务类型: "+envelope.BizType)
		if err := p.consumer.Commit(ctx, msg); err != nil {
			return err
		}
		return nil
	}
}

func (p *AgentAsyncPipeline) consumeChatHistory(ctx context.Context, msg segmentkafka.Message, envelope kafkabus.Envelope) error {
	var payload model.ChatHistoryPersistPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		_ = p.outboxRepo.MarkDead(ctx, envelope.OutboxID, "解析聊天持久化载荷失败: "+err.Error())
		if commitErr := p.consumer.Commit(ctx, msg); commitErr != nil {
			return commitErr
		}
		return nil
	}

	if err := p.outboxRepo.PersistChatHistoryAndMarkConsumed(ctx, envelope.OutboxID, payload); err != nil {
		if markErr := p.outboxRepo.MarkFailedForRetry(ctx, envelope.OutboxID, "消费并落库失败: "+err.Error()); markErr != nil {
			return markErr
		}
		if commitErr := p.consumer.Commit(ctx, msg); commitErr != nil {
			return commitErr
		}
		return err
	}

	return p.consumer.Commit(ctx, msg)
}
