package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	"github.com/LoveLosita/smartflow/backend/model"
	segmentkafka "github.com/segmentio/kafka-go"
	"gorm.io/gorm"
)

// MessageHandler 是 outbox 消费分发处理器。
//
// 设计约束：
// 1) 入参 envelope 已经完成最外层解析（含 outbox_id、biz_type、payload）；
// 2) 若返回 nil，表示业务处理成功，框架将继续提交 Kafka offset；
// 3) 若返回 error，框架会按“可重试错误”处理：回写 outbox 失败状态并进入重试窗口。
type MessageHandler func(ctx context.Context, envelope kafkabus.Envelope) error

// Engine 是 Outbox + Kafka 的通用异步引擎。
//
// 职责边界：
// 1) 负责 outbox 扫描、Kafka 投递、Kafka 消费与统一状态机流转；
// 2) 负责 biz_type 到处理器的分发；
// 3) 不关心具体业务含义（例如“聊天记录落库”），业务语义由 handler 提供。
//
// 状态流转口径：
// pending -> published -> consumed（成功）；
// pending/published --失败--> pending(带 next_retry_at) 或 dead（达到最大重试）。
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

// NewEngine 创建 outbox 异步引擎。
//
// 说明：
// 1) cfg.Enabled=false 时返回 nil，调用方可按“异步关闭”处理；
// 2) producer/consumer 初始化失败时会确保资源回收，避免半初始化泄漏。
func NewEngine(repo *Repository, cfg kafkabus.Config) (*Engine, error) {
	// 1. 配置关闭时直接返回 nil，让上层可以“无侵入降级为同步模式”。
	if !cfg.Enabled {
		return nil, nil
	}
	// 2. 仓储缺失属于启动期配置错误，直接返回。
	if repo == nil {
		return nil, errors.New("outbox repository is nil")
	}

	// 3. 先初始化 producer，再初始化 consumer。
	//    如果第二步失败，要主动回收第一步资源，避免泄漏。
	producer, err := kafkabus.NewProducer(cfg)
	if err != nil {
		return nil, err
	}
	consumer, err := kafkabus.NewConsumer(cfg)
	if err != nil {
		_ = producer.Close()
		return nil, err
	}

	// 4. 汇总配置，构造引擎实例。
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

// RegisterHandler 注册某个 biz_type 的消费处理器。
//
// 设计要求：
// 1) biz_type 必须唯一，重复注册会覆盖旧值（并打印提示日志）；
// 2) handler 不能为空；
// 3) 建议在 Start 前完成注册，减少运行时热更新复杂度。
func (e *Engine) RegisterHandler(bizType string, handler MessageHandler) error {
	// 1. 参数校验：防止业务侧在启动链路上把 nil 引擎继续往下用。
	if e == nil {
		return errors.New("outbox engine is nil")
	}
	// 2. biz_type 为空会导致无法分发，提前拦截。
	if bizType == "" {
		return errors.New("bizType is empty")
	}
	// 3. handler 为空会在消费时 panic，必须提前拒绝。
	if handler == nil {
		return errors.New("handler is nil")
	}

	// 4. 加写锁更新 handler 映射，保证并发注册时 map 安全。
	e.handlersMu.Lock()
	defer e.handlersMu.Unlock()
	if _, exists := e.handlers[bizType]; exists {
		log.Printf("outbox handler 覆盖注册: biz_type=%s", bizType)
	}
	e.handlers[bizType] = handler
	return nil
}

func (e *Engine) getHandler(bizType string) (MessageHandler, bool) {
	// 读锁足够满足并发读取需求，避免无谓阻塞。
	e.handlersMu.RLock()
	defer e.handlersMu.RUnlock()
	h, ok := e.handlers[bizType]
	return h, ok
}

// Start 启动 outbox 异步引擎。
//
// 会启动两个后台循环：
// 1) dispatch loop：扫描 due outbox 并投递到 Kafka；
// 2) consume loop：消费 Kafka 并按 biz_type 分发处理。
func (e *Engine) Start(ctx context.Context) {
	if e == nil {
		return
	}

	// 1. 启动日志：把关键运行参数打出来，便于排查“为什么没消费/没扫描”。
	log.Printf("outbox engine starting: topic=%s brokers=%v retry_scan=%s batch=%d", e.topic, e.brokers, e.scanEvery, e.scanBatch)

	// 2. 启动前探活 topic 是否可用。
	//    注意：即使探活失败也不会阻断引擎启动，后续循环会继续重试。
	if err := kafkabus.WaitTopicReady(ctx, e.brokers, e.topic, 30*time.Second); err != nil {
		log.Printf("Kafka topic not ready before consume loop start: %v", err)
	} else {
		log.Printf("Kafka topic is ready: %s", e.topic)
	}

	// 3. 并行启动两条核心循环：
	//    - dispatch loop：负责 outbox -> Kafka；
	//    - consume loop：负责 Kafka -> handler -> outbox 状态推进。
	go e.startDispatchLoop(ctx)
	go e.startConsumeLoop(ctx)
}

// Close 关闭 producer/consumer 资源。
func (e *Engine) Close() {
	if e == nil {
		return
	}
	// 逐个关闭并记录错误，避免某个 close 失败导致后续资源无法回收。
	if err := e.producer.Close(); err != nil {
		log.Printf("关闭 Kafka producer 失败: %v", err)
	}
	if err := e.consumer.Close(); err != nil {
		log.Printf("关闭 Kafka consumer 失败: %v", err)
	}
}

// Enqueue 把业务消息写入 outbox（请求路径调用）。
//
// 注意：
// 1) 该方法不做 Kafka 网络写入，只有数据库写入；
// 2) messageKey 建议使用业务幂等键（如 conversation_id）以提升分区稳定性；
// 3) payload 需要可 JSON 序列化。
func (e *Engine) Enqueue(ctx context.Context, bizType, messageKey string, payload any) error {
	if e == nil {
		return errors.New("outbox engine is nil")
	}
	// 这里故意只写数据库，不做 Kafka 网络 IO，
	// 目的是把请求耗时稳定在“单次写库”的可控范围。
	_, err := e.repo.CreateMessage(ctx, bizType, e.topic, messageKey, payload, e.maxRetry)
	return err
}

func (e *Engine) startDispatchLoop(ctx context.Context) {
	// 1. 定时扫描 due outbox 记录。
	//    扫描间隔由 scanEvery 控制，避免每次请求都主动触发投递造成抖动。
	ticker := time.NewTicker(e.scanEvery)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// 2. 收到退出信号后优雅停止循环。
			return
		case <-ticker.C:
			// 3. 拉取当前窗口内可投递消息。
			pendingMessages, err := e.repo.ListDueMessages(ctx, e.scanBatch)
			if err != nil {
				log.Printf("扫描 outbox 失败: %v", err)
				continue
			}
			if len(pendingMessages) > 0 {
				log.Printf("outbox due messages=%d, start dispatch", len(pendingMessages))
			}

			// 4. 逐条投递，单条失败不影响同批后续消息。
			for _, msg := range pendingMessages {
				if err = e.dispatchOne(ctx, msg.ID); err != nil {
					log.Printf("重试投递 outbox 消息失败(id=%d): %v", msg.ID, err)
				}
			}
		}
	}
}

func (e *Engine) dispatchOne(ctx context.Context, outboxID int64) error {
	// 1. 投递前重新按 ID 读取最新状态，避免用到过期快照。
	outboxMsg, err := e.repo.GetByID(ctx, outboxID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 1.1 记录已不存在（可能被清理），按幂等成功处理。
			return nil
		}
		return err
	}
	// 1.2 最终态直接跳过，避免重复投递。
	if outboxMsg.Status == model.OutboxStatusConsumed || outboxMsg.Status == model.OutboxStatusDead {
		return nil
	}

	// 2. 组装 Kafka 包装体，统一带上 outbox_id 供消费端做状态回写。
	envelope := kafkabus.Envelope{
		OutboxID: outboxMsg.ID,
		BizType:  outboxMsg.BizType,
		Payload:  json.RawMessage(outboxMsg.Payload),
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		// 2.1 包装层序列化失败通常不可恢复，直接标 dead。
		markErr := e.repo.MarkDead(ctx, outboxMsg.ID, "序列化 outbox 包装失败: "+err.Error())
		if markErr != nil {
			log.Printf("标记 outbox 死信失败(id=%d): %v", outboxMsg.ID, markErr)
		}
		return err
	}

	// 3. 先投 Kafka，再把 outbox 状态推进到 published。
	//    任一步骤失败都回写 retry，让扫描器后续重试。
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
	// 消费循环采用“拉取 -> 处理 -> 提交 offset”的标准模型。
	for {
		select {
		case <-ctx.Done():
			// 1. 收到退出信号后终止循环。
			return
		default:
		}

		// 2. 拉取下一条 Kafka 消息。
		msg, err := e.consumer.Dequeue(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// 2.1 context 主动取消时，不记错误日志，直接退出。
				return
			}
			// 2.2 临时错误短暂退避后继续，避免空转刷日志。
			log.Printf("Kafka 消费拉取失败(topic=%s): %v", e.topic, err)
			time.Sleep(300 * time.Millisecond)
			continue
		}

		// 3. 单条消息处理失败仅记录日志，不阻断消费循环。
		if err = e.handleMessage(ctx, msg); err != nil {
			log.Printf("处理 Kafka 消息失败(topic=%s, partition=%d, offset=%d): %v", msg.Topic, msg.Partition, msg.Offset, err)
		}
	}
}

func (e *Engine) handleMessage(ctx context.Context, msg segmentkafka.Message) error {
	// 1. 先解析最外层 envelope，拿到 outbox_id + biz_type + payload。
	var envelope kafkabus.Envelope
	if err := json.Unmarshal(msg.Value, &envelope); err != nil {
		// 1.1 包装层损坏时无法恢复，直接提交 offset 防止无限重放。
		_ = e.consumer.Commit(ctx, msg)
		return fmt.Errorf("解析 Kafka 包装失败: %w", err)
	}
	if envelope.OutboxID <= 0 {
		// 1.2 缺少 outbox_id 无法回写状态，同样提交 offset 跳过。
		_ = e.consumer.Commit(ctx, msg)
		return errors.New("Kafka 包装缺少 outbox_id")
	}

	// 2. 根据 biz_type 查找业务处理器。
	handler, ok := e.getHandler(envelope.BizType)
	if !ok {
		// 2.1 未注册处理器是配置错误，标记 dead 并提交 offset，避免重复消费。
		_ = e.repo.MarkDead(ctx, envelope.OutboxID, "未知业务类型: "+envelope.BizType)
		if err := e.consumer.Commit(ctx, msg); err != nil {
			return err
		}
		return nil
	}

	// 3. 调用业务处理器。
	if err := handler(ctx, envelope); err != nil {
		// 统一按“可重试错误”处理，回写 retry 状态后提交 offset，避免同一条消息在 Kafka 侧死循环。
		if markErr := e.repo.MarkFailedForRetry(ctx, envelope.OutboxID, "消费处理失败: "+err.Error()); markErr != nil {
			return markErr
		}
		if commitErr := e.consumer.Commit(ctx, msg); commitErr != nil {
			return commitErr
		}
		return err
	}

	// 4. 业务处理成功后提交 offset。
	return e.consumer.Commit(ctx, msg)
}
