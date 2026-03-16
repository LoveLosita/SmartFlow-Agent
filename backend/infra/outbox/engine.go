package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	"github.com/LoveLosita/smartflow/backend/model"
	segmentkafka "github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// MessageHandler 是事件消费处理器。
//
// 语义约束：
// 1. 入参 envelope 已完成最外层解析；
// 2. 返回 nil 表示处理成功，框架提交 offset；
// 3. 返回 error 表示可重试失败，框架回写 retry 后提交 offset。
type MessageHandler func(ctx context.Context, envelope kafkabus.Envelope) error

// PublishRequest 是通用事件发布入参。
//
// 设计目标：
// 1. 业务只描述“要发什么事件”，不关心 outbox/kafka 细节；
// 2. 统一收敛事件元数据（event_type/version/aggregate_id）；
// 3. payload 支持任意 DTO，由 infra 统一 JSON 序列化。
type PublishRequest struct {
	EventType    string
	EventVersion string
	MessageKey   string
	AggregateID  string
	EventID      string
	Payload      any
}

// Engine 是 Outbox + Kafka 通用异步引擎。
//
// 职责边界：
// 1. 负责 outbox 扫描、kafka 投递、kafka 消费、状态机推进；
// 2. 负责 event_type -> handler 路由；
// 3. 不负责任何业务语义（业务由 handler 承担）。
type Engine struct {
	repo     *Repository
	producer *kafkabus.Producer
	consumer *kafkabus.Consumer

	brokers   []string
	topic     string
	maxRetry  int
	scanEvery time.Duration
	scanBatch int

	handlersMu sync.RWMutex
	handlers   map[string]MessageHandler
}

// NewEngine 创建异步引擎。
//
// 规则：
// 1. kafka.enabled=false 时返回 nil，调用方可降级同步；
// 2. producer/consumer 任一步失败都会回收已创建资源。
func NewEngine(repo *Repository, cfg kafkabus.Config) (*Engine, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if repo == nil {
		return nil, errors.New("outbox repository is nil")
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

	return &Engine{
		repo:      repo,
		producer:  producer,
		consumer:  consumer,
		brokers:   cfg.Brokers,
		topic:     cfg.Topic,
		maxRetry:  cfg.MaxRetry,
		scanEvery: cfg.RetryScanInterval,
		scanBatch: cfg.RetryBatchSize,
		handlers:  make(map[string]MessageHandler),
	}, nil
}

// RegisterHandler 是历史别名（等价 RegisterEventHandler）。
func (e *Engine) RegisterHandler(eventType string, handler MessageHandler) error {
	return e.RegisterEventHandler(eventType, handler)
}

// RegisterEventHandler 注册事件处理器。
func (e *Engine) RegisterEventHandler(eventType string, handler MessageHandler) error {
	if e == nil {
		return errors.New("outbox engine is nil")
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return errors.New("eventType is empty")
	}
	if handler == nil {
		return errors.New("handler is nil")
	}

	e.handlersMu.Lock()
	defer e.handlersMu.Unlock()
	if _, exists := e.handlers[eventType]; exists {
		log.Printf("outbox handler 覆盖注册: event_type=%s", eventType)
	}
	e.handlers[eventType] = handler
	return nil
}

func (e *Engine) getHandler(eventType string) (MessageHandler, bool) {
	e.handlersMu.RLock()
	defer e.handlersMu.RUnlock()
	h, ok := e.handlers[eventType]
	return h, ok
}

// Start 启动 dispatch + consume 两个后台循环。
func (e *Engine) Start(ctx context.Context) {
	if e == nil {
		return
	}

	log.Printf("outbox engine starting: topic=%s brokers=%v retry_scan=%s batch=%d", e.topic, e.brokers, e.scanEvery, e.scanBatch)
	if err := kafkabus.WaitTopicReady(ctx, e.brokers, e.topic, 30*time.Second); err != nil {
		log.Printf("Kafka topic not ready before consume loop start: %v", err)
	} else {
		log.Printf("Kafka topic is ready: %s", e.topic)
	}

	go e.startDispatchLoop(ctx)
	go e.startConsumeLoop(ctx)
}

// Close 关闭 kafka 资源。
func (e *Engine) Close() {
	if e == nil {
		return
	}
	if err := e.producer.Close(); err != nil {
		log.Printf("关闭 Kafka producer 失败: %v", err)
	}
	if err := e.consumer.Close(); err != nil {
		log.Printf("关闭 Kafka consumer 失败: %v", err)
	}
}

// Enqueue 是历史别名（等价 Publish）。
func (e *Engine) Enqueue(ctx context.Context, eventType, messageKey string, payload any) error {
	return e.Publish(ctx, PublishRequest{
		EventType:   eventType,
		MessageKey:  messageKey,
		AggregateID: messageKey,
		Payload:     payload,
	})
}

// Publish 发布事件到 outbox。
//
// 步骤：
// 1. 标准化 event_type/version/key；
// 2. payload 序列化；
// 3. 写入 outbox（仅本地写库，不做 kafka 网络 IO）。
func (e *Engine) Publish(ctx context.Context, req PublishRequest) error {
	if e == nil {
		return errors.New("outbox engine is nil")
	}

	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return errors.New("eventType is empty")
	}
	eventVersion := strings.TrimSpace(req.EventVersion)
	if eventVersion == "" {
		eventVersion = DefaultEventVersion
	}
	messageKey := strings.TrimSpace(req.MessageKey)
	aggregateID := strings.TrimSpace(req.AggregateID)
	if aggregateID == "" {
		aggregateID = messageKey
	}

	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}

	_, err = e.repo.CreateMessage(ctx, eventType, e.topic, messageKey, OutboxEventPayload{
		EventID:      strings.TrimSpace(req.EventID),
		EventType:    eventType,
		EventVersion: eventVersion,
		AggregateID:  aggregateID,
		Payload:      payloadJSON,
	}, e.maxRetry)
	return err
}

func (e *Engine) startDispatchLoop(ctx context.Context) {
	ticker := time.NewTicker(e.scanEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pendingMessages, err := e.repo.ListDueMessages(ctx, e.scanBatch)
			if err != nil {
				log.Printf("扫描 outbox 失败: %v", err)
				continue
			}
			if len(pendingMessages) > 0 {
				log.Printf("outbox due messages=%d, start dispatch", len(pendingMessages))
			}

			for _, msg := range pendingMessages {
				if err = e.dispatchOne(ctx, msg.ID); err != nil {
					log.Printf("重试投递 outbox 消息失败(id=%d): %v", msg.ID, err)
				}
			}
		}
	}
}

func (e *Engine) dispatchOne(ctx context.Context, outboxID int64) error {
	outboxMsg, err := e.repo.GetByID(ctx, outboxID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if outboxMsg.Status == model.OutboxStatusConsumed || outboxMsg.Status == model.OutboxStatusDead {
		return nil
	}

	eventPayload, payloadErr := parseOutboxEventPayload(outboxMsg.Payload)
	if payloadErr != nil {
		markErr := e.repo.MarkDead(ctx, outboxMsg.ID, "解析 outbox 事件包失败: "+payloadErr.Error())
		if markErr != nil {
			log.Printf("标记 outbox 死信失败(id=%d): %v", outboxMsg.ID, markErr)
		}
		return payloadErr
	}
	if eventPayload.EventID == "" {
		eventPayload.EventID = strconv.FormatInt(outboxMsg.ID, 10)
	}

	envelope := kafkabus.Envelope{
		OutboxID:     outboxMsg.ID,
		EventID:      eventPayload.EventID,
		EventType:    eventPayload.EventType,
		EventVersion: eventPayload.EventVersion,
		AggregateID:  eventPayload.AggregateID,
		Payload:      eventPayload.PayloadJSON,
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		markErr := e.repo.MarkDead(ctx, outboxMsg.ID, "序列化 outbox 包装失败: "+err.Error())
		if markErr != nil {
			log.Printf("标记 outbox 死信失败(id=%d): %v", outboxMsg.ID, markErr)
		}
		return err
	}

	if err = e.producer.Enqueue(ctx, outboxMsg.Topic, outboxMsg.MessageKey, raw); err != nil {
		_ = e.repo.MarkFailedForRetry(ctx, outboxMsg.ID, "投递 Kafka 失败: "+err.Error())
		return err
	}
	if err = e.repo.MarkPublished(ctx, outboxMsg.ID); err != nil {
		_ = e.repo.MarkFailedForRetry(ctx, outboxMsg.ID, "更新已投递状态失败: "+err.Error())
		return err
	}
	return nil
}

func (e *Engine) startConsumeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := e.consumer.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			log.Printf("Kafka 消费拉取失败(topic=%s): %v", e.topic, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}

		if err = e.handleMessage(ctx, msg); err != nil {
			log.Printf("处理 Kafka 消息失败(topic=%s, partition=%d, offset=%d): %v", msg.Topic, msg.Partition, msg.Offset, err)
		}
	}
}

func (e *Engine) handleMessage(ctx context.Context, msg segmentkafka.Message) error {
	var envelope kafkabus.Envelope
	if err := json.Unmarshal(msg.Value, &envelope); err != nil {
		_ = e.consumer.Commit(ctx, msg)
		return fmt.Errorf("解析 Kafka 包装失败: %w", err)
	}
	if envelope.OutboxID <= 0 {
		_ = e.consumer.Commit(ctx, msg)
		return errors.New("Kafka 包装缺少 outbox_id")
	}

	eventType := strings.TrimSpace(envelope.EventType)
	if eventType == "" {
		_ = e.repo.MarkDead(ctx, envelope.OutboxID, "消息缺少事件类型")
		if err := e.consumer.Commit(ctx, msg); err != nil {
			return err
		}
		return nil
	}

	handler, ok := e.getHandler(eventType)
	if !ok {
		_ = e.repo.MarkDead(ctx, envelope.OutboxID, "未知事件类型: "+eventType)
		if err := e.consumer.Commit(ctx, msg); err != nil {
			return err
		}
		return nil
	}

	if err := handler(ctx, envelope); err != nil {
		if markErr := e.repo.MarkFailedForRetry(ctx, envelope.OutboxID, "消费处理失败: "+err.Error()); markErr != nil {
			return markErr
		}
		if commitErr := e.consumer.Commit(ctx, msg); commitErr != nil {
			return commitErr
		}
		return err
	}

	return e.consumer.Commit(ctx, msg)
}
