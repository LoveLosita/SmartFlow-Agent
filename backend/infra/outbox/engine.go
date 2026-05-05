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

const minPublishedRescueAfter = 10 * time.Second

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

// Engine 是单个服务的 Outbox + Kafka 异步引擎。
//
// 职责边界：
// 1. 负责一个服务目录下的 outbox 扫描、Kafka 投递、Kafka 消费、状态机推进；
// 2. 负责 event_type -> handler 路由；
// 3. 不负责任何跨服务路由决策，跨服务分发由 EventBus 门面完成。
type Engine struct {
	repo     *Repository
	producer *kafkabus.Producer
	consumer *kafkabus.Consumer

	brokers   []string
	route     ServiceRoute
	maxRetry  int
	scanEvery time.Duration
	scanBatch int
	// publishedRescueAfter 是 published 消息本地兜底消费窗口，避免 Kafka 已投递但 consumer 长时间未完成时永久卡住。
	publishedRescueAfter time.Duration
	// publishedRescueEnabled 控制是否启用本地兜底消费；默认只给幂等账务类服务打开。
	publishedRescueEnabled bool

	handlersMu sync.RWMutex
	handlers   map[string]MessageHandler
}

// NewEngine 创建单服务异步引擎。
//
// 规则：
// 1. kafka.enabled=false 时返回 nil，调用方可降级同步；
// 2. serviceName 非空时优先使用服务级默认目录，topic/group/table 不再沿用共享终态；
// 3. producer/consumer 任一步失败都会回收已创建资源。
func NewEngine(repo *Repository, cfg kafkabus.Config) (*Engine, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if repo == nil {
		return nil, errors.New("outbox repository is nil")
	}

	route := resolveEngineRoute(repo, cfg)
	cfg.Topic = route.Topic
	cfg.GroupID = route.GroupID

	serviceRepo := repo.WithRoute(route)
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
		repo:                   serviceRepo,
		producer:               producer,
		consumer:               consumer,
		brokers:                cfg.Brokers,
		route:                  route,
		maxRetry:               cfg.MaxRetry,
		scanEvery:              cfg.RetryScanInterval,
		scanBatch:              cfg.RetryBatchSize,
		publishedRescueAfter:   normalizePublishedRescueAfter(cfg.RetryScanInterval),
		publishedRescueEnabled: route.ServiceName == ServiceNameTokenStore,
		handlers:               make(map[string]MessageHandler),
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
		log.Printf("outbox handler 覆盖注册: service=%s event_type=%s", e.route.ServiceName, eventType)
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

	log.Printf(
		"outbox engine starting: service=%s table=%s topic=%s group=%s brokers=%v retry_scan=%s batch=%d",
		e.route.ServiceName,
		e.route.TableName,
		e.route.Topic,
		e.route.GroupID,
		e.brokers,
		e.scanEvery,
		e.scanBatch,
	)
	if err := kafkabus.WaitTopicReady(ctx, e.brokers, e.route.Topic, 30*time.Second); err != nil {
		log.Printf("Kafka topic not ready before consume loop start: %v", err)
	} else {
		log.Printf("Kafka topic is ready: %s", e.route.Topic)
	}

	e.StartDispatch(ctx)
	e.StartConsume(ctx)
}

// StartDispatch 单独启动 outbox -> Kafka 的投递循环。
func (e *Engine) StartDispatch(ctx context.Context) {
	if e == nil {
		return
	}
	go e.startDispatchLoop(ctx)
}

// StartConsume 单独启动 Kafka -> handler 的消费循环。
func (e *Engine) StartConsume(ctx context.Context) {
	if e == nil {
		return
	}
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
// 3. 写入当前服务的 outbox 表，不再由调用方手传 topic。
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

	_, err = e.repo.CreateMessage(ctx, eventType, messageKey, OutboxEventPayload{
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
			pendingMessages, err := e.repo.ListDueMessages(ctx, e.route.ServiceName, e.scanBatch)
			if err != nil {
				log.Printf("扫描 outbox 失败: %v", err)
				continue
			}
			if len(pendingMessages) > 0 {
				log.Printf("outbox due messages=%d, service=%s start dispatch", len(pendingMessages), e.route.ServiceName)
			}

			for _, msg := range pendingMessages {
				if err = e.dispatchOne(ctx, msg.ID); err != nil {
					log.Printf("重试投递 outbox 消息失败(id=%d): %v", msg.ID, err)
				}
			}
			if err = e.rescueStalePublishedMessages(ctx); err != nil {
				log.Printf("兜底消费已投递 outbox 消息失败(service=%s): %v", e.route.ServiceName, err)
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

	envelope, payloadErr := e.envelopeFromOutboxMessage(outboxMsg)
	if payloadErr != nil {
		markErr := e.repo.MarkDead(ctx, outboxMsg.ID, "解析 outbox 事件包失败: "+payloadErr.Error())
		if markErr != nil {
			log.Printf("标记 outbox 死信失败(id=%d): %v", outboxMsg.ID, markErr)
		}
		return payloadErr
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		markErr := e.repo.MarkDead(ctx, outboxMsg.ID, "序列化 outbox 封装失败: "+err.Error())
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

// rescueStalePublishedMessages 对 published 后长时间未 consumed 的消息做本地兜底消费。
//
// 职责边界：
// 1. 只处理当前 service 表内的 stale published 消息，不扫描其它服务；
// 2. 不重新投递 Kafka，直接复用 handler 的幂等消费逻辑，避免同一坏分区长期卡死；
// 3. 单条失败只写日志并继续下一条，避免一条坏消息阻断整批兜底。
func (e *Engine) rescueStalePublishedMessages(ctx context.Context) error {
	if e == nil || !e.publishedRescueEnabled {
		return nil
	}
	before := time.Now().Add(-e.publishedRescueAfter)
	messages, err := e.repo.ListStalePublishedMessages(ctx, e.route.ServiceName, before, e.scanBatch)
	if err != nil {
		return err
	}
	if len(messages) > 0 {
		log.Printf("outbox stale published messages=%d, service=%s start local consume", len(messages), e.route.ServiceName)
	}
	for _, msg := range messages {
		if err := e.consumePublishedOne(ctx, msg.ID); err != nil {
			log.Printf("兜底消费 outbox 消息失败(id=%d, service=%s): %v", msg.ID, e.route.ServiceName, err)
		}
	}
	return nil
}

// consumePublishedOne 兜底消费单条已投递但未完成的 outbox 消息。
//
// 职责边界：
// 1. 只在当前状态仍为 published 时处理，避免覆盖正常 consumer 的最终态；
// 2. 解析失败标记 dead，业务失败交给 handleEnvelope 推进重试；
// 3. 不提交 Kafka offset，因为这里没有从 Kafka 读取消息。
func (e *Engine) consumePublishedOne(ctx context.Context, outboxID int64) error {
	outboxMsg, err := e.repo.GetByID(ctx, outboxID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if outboxMsg.Status != model.OutboxStatusPublished {
		return nil
	}

	envelope, payloadErr := e.envelopeFromOutboxMessage(outboxMsg)
	if payloadErr != nil {
		markErr := e.repo.MarkDead(ctx, outboxMsg.ID, "解析已投递 outbox 事件包失败: "+payloadErr.Error())
		if markErr != nil {
			log.Printf("标记 outbox 死信失败(id=%d): %v", outboxMsg.ID, markErr)
		}
		return payloadErr
	}
	return e.handleEnvelope(ctx, envelope, false)
}

// envelopeFromOutboxMessage 把 outbox 表记录还原成统一事件信封。
//
// 职责边界：
// 1. 只做 payload 外壳解析和缺省字段补齐；
// 2. 不判断业务事件是否合法，具体校验仍交给 handler；
// 3. event_id 缺失时使用 outbox id 兜底，保持历史消息可消费。
func (e *Engine) envelopeFromOutboxMessage(outboxMsg *model.AgentOutboxMessage) (kafkabus.Envelope, error) {
	if outboxMsg == nil {
		return kafkabus.Envelope{}, errors.New("outbox message is nil")
	}
	eventPayload, err := parseOutboxEventPayload(outboxMsg.Payload)
	if err != nil {
		return kafkabus.Envelope{}, err
	}
	if eventPayload.EventID == "" {
		eventPayload.EventID = strconv.FormatInt(outboxMsg.ID, 10)
	}
	serviceName := strings.TrimSpace(outboxMsg.ServiceName)
	if serviceName == "" {
		serviceName = e.route.ServiceName
	}
	return kafkabus.Envelope{
		OutboxID:     outboxMsg.ID,
		EventID:      eventPayload.EventID,
		EventType:    eventPayload.EventType,
		EventVersion: eventPayload.EventVersion,
		ServiceName:  serviceName,
		AggregateID:  eventPayload.AggregateID,
		Payload:      eventPayload.PayloadJSON,
	}, nil
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
			log.Printf("Kafka 消费拉取失败(topic=%s): %v", e.route.Topic, err)
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
		return fmt.Errorf("解析 Kafka 封装失败: %w", err)
	}
	if envelope.OutboxID <= 0 {
		_ = e.consumer.Commit(ctx, msg)
		return errors.New("Kafka 封装缺少 outbox_id")
	}

	if err := e.handleEnvelope(ctx, envelope, true); err != nil {
		if commitErr := e.consumer.Commit(ctx, msg); commitErr != nil {
			return commitErr
		}
		return err
	}
	return e.consumer.Commit(ctx, msg)
}

// handleEnvelope 执行统一事件信封的本地 handler 路由和状态推进。
//
// 职责边界：
// 1. 负责事件类型、服务归属和 handler 存在性校验；
// 2. handler 成功后由业务 handler 自己标记 consumed；
// 3. retryOnFailure=true 时才把失败消息退回 pending，避免本地兜底把已投递消息重复投到 Kafka。
func (e *Engine) handleEnvelope(ctx context.Context, envelope kafkabus.Envelope, retryOnFailure bool) error {
	status, err := e.currentMessageStatus(ctx, envelope.OutboxID)
	if err != nil {
		return err
	}
	if status != model.OutboxStatusPublished {
		return nil
	}

	eventType := strings.TrimSpace(envelope.EventType)
	if eventType == "" {
		_ = e.repo.MarkDead(ctx, envelope.OutboxID, "消息缺少事件类型")
		return nil
	}

	runtimeServiceName := strings.TrimSpace(e.route.ServiceName)
	if runtimeServiceName != "" {
		messageServiceName := strings.TrimSpace(envelope.ServiceName)
		if messageServiceName == "" {
			if resolvedServiceName, ok := ResolveEventService(eventType); ok {
				messageServiceName = resolvedServiceName
			}
		}
		if messageServiceName == "" || messageServiceName != runtimeServiceName {
			log.Printf(
				"跳过非本服务事件: runtime_service=%s message_service=%s event_type=%s outbox_id=%d",
				runtimeServiceName,
				messageServiceName,
				eventType,
				envelope.OutboxID,
			)
			return nil
		}
	}

	handler, ok := e.getHandler(eventType)
	if !ok {
		if runtimeServiceName == "" {
			_ = e.repo.MarkDead(ctx, envelope.OutboxID, "未知事件类型: "+eventType)
		} else {
			_ = e.repo.MarkDead(ctx, envelope.OutboxID, "本服务未注册 handler: "+eventType)
		}
		return nil
	}

	if err := handler(ctx, envelope); err != nil {
		if retryOnFailure {
			if markErr := e.repo.MarkFailedForRetry(ctx, envelope.OutboxID, "消费处理失败: "+err.Error()); markErr != nil {
				return markErr
			}
		}
		return err
	}

	return nil
}

// currentMessageStatus 读取 outbox 当前状态，作为重复 Kafka 消息的第一道闸门。
//
// 职责边界：
// 1. 只返回当前状态，不推进状态机；
// 2. 记录已消失时按最终态处理，避免历史 Kafka 消息造成消费循环报错；
// 3. handler 只允许在 published 状态执行，pending/consumed/dead 都直接跳过。
func (e *Engine) currentMessageStatus(ctx context.Context, outboxID int64) (string, error) {
	msg, err := e.repo.GetByID(ctx, outboxID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.OutboxStatusConsumed, nil
		}
		return "", err
	}
	return strings.TrimSpace(msg.Status), nil
}

// normalizePublishedRescueAfter 根据扫描间隔计算 published 兜底窗口。
//
// 职责边界：
// 1. 只做最小窗口保护，避免刚投递的消息被立即本地重复消费；
// 2. 不读取配置中心，保持 outbox engine 构造参数单一；
// 3. 返回值越小恢复越快，越大重复消费概率越低。
func normalizePublishedRescueAfter(scanEvery time.Duration) time.Duration {
	rescueAfter := scanEvery * 3
	if rescueAfter < minPublishedRescueAfter {
		return minPublishedRescueAfter
	}
	return rescueAfter
}

func resolveEngineRoute(repo *Repository, cfg kafkabus.Config) ServiceRoute {
	route := ServiceRoute{
		ServiceName: strings.TrimSpace(cfg.ServiceName),
		Topic:       strings.TrimSpace(cfg.Topic),
		GroupID:     strings.TrimSpace(cfg.GroupID),
	}
	if repo != nil {
		repoRoute := normalizeServiceRoute(repo.route)
		if route.ServiceName == "" {
			route.ServiceName = repoRoute.ServiceName
		}
		if route.TableName == "" {
			route.TableName = repoRoute.TableName
		}
		if route.Topic == "" {
			route.Topic = repoRoute.Topic
		}
		if route.GroupID == "" {
			route.GroupID = repoRoute.GroupID
		}
	}

	if route.ServiceName != "" {
		defaultRoute := DefaultServiceRoute(route.ServiceName)
		if route.TableName == "" {
			route.TableName = defaultRoute.TableName
		}
		if route.Topic == "" {
			route.Topic = defaultRoute.Topic
		}
		if route.GroupID == "" {
			route.GroupID = defaultRoute.GroupID
		}
		return normalizeServiceRoute(route)
	}

	if route.TableName == "" {
		route.TableName = DefaultServiceRoute(ServiceNameAgent).TableName
	}
	if route.Topic == "" {
		route.Topic = kafkabus.DefaultTopic
	}
	if route.GroupID == "" {
		route.GroupID = kafkabus.DefaultGroup
	}
	return normalizeServiceRoute(route)
}
