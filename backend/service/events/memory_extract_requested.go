package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/memory"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

const (
	// EventTypeMemoryExtractRequested 是“记忆抽取请求”事件类型。
	EventTypeMemoryExtractRequested = "memory.extract.requested"
	maxMemorySourceTextLength       = 1500
)

// RegisterMemoryExtractRequestedHandler 注册“记忆抽取请求”消费者。
//
// 职责边界：
// 1. 只负责把事件转为 memory_jobs 任务；
// 2. 不在消费回调里执行 LLM 重计算；
// 3. 通过 memory.Module.WithTx(tx) 复用同一套接入门面，保证事务边界仍由 outbox 掌控。
func RegisterMemoryExtractRequestedHandler(
	bus *outboxinfra.EventBus,
	outboxRepo *outboxinfra.Repository,
	memoryModule *memory.Module,
) error {
	if bus == nil {
		return errors.New("event bus is nil")
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}
	if memoryModule == nil {
		return errors.New("memory module is nil")
	}

	handler := func(ctx context.Context, envelope kafkabus.Envelope) error {
		var payload model.MemoryExtractRequestedPayload
		if unmarshalErr := json.Unmarshal(envelope.Payload, &payload); unmarshalErr != nil {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "解析记忆抽取载荷失败: "+unmarshalErr.Error())
			return nil
		}

		if validateErr := validateMemoryExtractPayload(payload); validateErr != nil {
			_ = outboxRepo.MarkDead(ctx, envelope.OutboxID, "记忆抽取载荷非法: "+validateErr.Error())
			return nil
		}

		return outboxRepo.ConsumeAndMarkConsumed(ctx, envelope.OutboxID, func(tx *gorm.DB) error {
			jobPayload := memorymodel.ExtractJobPayload{
				UserID:          payload.UserID,
				ConversationID:  strings.TrimSpace(payload.ConversationID),
				AssistantID:     strings.TrimSpace(payload.AssistantID),
				RunID:           strings.TrimSpace(payload.RunID),
				SourceMessageID: payload.SourceMessageID,
				SourceRole:      strings.TrimSpace(payload.SourceRole),
				SourceText:      strings.TrimSpace(payload.SourceText),
				OccurredAt:      payload.OccurredAt,
				TraceID:         strings.TrimSpace(payload.TraceID),
				IdempotencyKey:  strings.TrimSpace(payload.IdempotencyKey),
			}
			return memoryModule.WithTx(tx).EnqueueExtract(ctx, jobPayload, envelope.EventID)
		})
	}

	return bus.RegisterEventHandler(EventTypeMemoryExtractRequested, handler)
}

// EnqueueMemoryExtractRequestedInTx 在事务内写入 memory.extract.requested outbox 消息。
//
// 设计目的：
// 1. 让“聊天消息已落库”和“记忆抽取事件已入队”同事务提交；
// 2. 任意一步失败都整体回滚，避免出现链路断点。
func EnqueueMemoryExtractRequestedInTx(
	ctx context.Context,
	outboxRepo *outboxinfra.Repository,
	kafkaCfg kafkabus.Config,
	chatPayload model.ChatHistoryPersistPayload,
) error {
	if !isMemoryWriteEnabled() {
		return nil
	}
	if outboxRepo == nil {
		return errors.New("outbox repository is nil")
	}

	memoryPayload, shouldEnqueue := buildMemoryExtractPayloadFromChat(chatPayload)
	if !shouldEnqueue {
		return nil
	}

	payloadJSON, err := json.Marshal(memoryPayload)
	if err != nil {
		return err
	}

	outboxPayload := outboxinfra.OutboxEventPayload{
		EventType:    EventTypeMemoryExtractRequested,
		EventVersion: outboxinfra.DefaultEventVersion,
		AggregateID:  strings.TrimSpace(chatPayload.ConversationID),
		Payload:      payloadJSON,
	}

	_, err = outboxRepo.CreateMessage(
		ctx,
		EventTypeMemoryExtractRequested,
		kafkaCfg.Topic,
		strings.TrimSpace(chatPayload.ConversationID),
		outboxPayload,
		kafkaCfg.MaxRetry,
	)
	return err
}

// PublishMemoryExtractFromGraph 在 graph 完成后直接发布记忆抽取事件。
//
// 设计目的：
// 1. 绕过 chat-persist 链路，由 agent service 在 graph 完成后按需调用；
// 2. 内部完成 source text 截断、幂等 key 生成、memory 开关检查；
// 3. 发布失败只记日志，不阻断主链路。
func PublishMemoryExtractFromGraph(
	ctx context.Context,
	publisher outboxinfra.EventPublisher,
	userID int,
	conversationID string,
	sourceText string,
) error {
	if !isMemoryWriteEnabled() {
		return nil
	}
	if publisher == nil {
		return errors.New("event publisher is nil")
	}

	sourceText = strings.TrimSpace(sourceText)
	if sourceText == "" || userID <= 0 || strings.TrimSpace(conversationID) == "" {
		return nil
	}

	truncated := truncateByRune(sourceText, maxMemorySourceTextLength)
	now := time.Now()
	payload := model.MemoryExtractRequestedPayload{
		UserID:         userID,
		ConversationID: strings.TrimSpace(conversationID),
		SourceRole:     "user",
		SourceText:     truncated,
		OccurredAt:     now,
		IdempotencyKey: buildMemoryExtractIdempotencyKey(userID, conversationID, truncated),
	}

	return publisher.Publish(ctx, outboxinfra.PublishRequest{
		EventType:    EventTypeMemoryExtractRequested,
		EventVersion: outboxinfra.DefaultEventVersion,
		MessageKey:   payload.ConversationID,
		AggregateID:  payload.ConversationID,
		Payload:      payload,
	})
}

func buildMemoryExtractPayloadFromChat(chatPayload model.ChatHistoryPersistPayload) (model.MemoryExtractRequestedPayload, bool) {
	role := strings.ToLower(strings.TrimSpace(chatPayload.Role))
	if role != "user" {
		return model.MemoryExtractRequestedPayload{}, false
	}

	sourceText := strings.TrimSpace(chatPayload.Message)
	if sourceText == "" {
		return model.MemoryExtractRequestedPayload{}, false
	}

	truncatedSourceText := truncateByRune(sourceText, maxMemorySourceTextLength)
	now := time.Now()
	return model.MemoryExtractRequestedPayload{
		UserID:         chatPayload.UserID,
		ConversationID: strings.TrimSpace(chatPayload.ConversationID),
		// Day1 先保留 assistant_id/run_id 空值，后续从主链路上下文补齐。
		AssistantID:     "",
		RunID:           "",
		SourceMessageID: 0,
		SourceRole:      role,
		SourceText:      truncatedSourceText,
		OccurredAt:      now,
		TraceID:         "",
		IdempotencyKey:  buildMemoryExtractIdempotencyKey(chatPayload.UserID, chatPayload.ConversationID, truncatedSourceText),
	}, true
}

func validateMemoryExtractPayload(payload model.MemoryExtractRequestedPayload) error {
	if payload.UserID <= 0 {
		return errors.New("user_id is invalid")
	}
	if strings.TrimSpace(payload.ConversationID) == "" {
		return errors.New("conversation_id is empty")
	}
	if strings.TrimSpace(payload.SourceRole) == "" {
		return errors.New("source_role is empty")
	}
	if strings.TrimSpace(payload.SourceText) == "" {
		return errors.New("source_text is empty")
	}
	if strings.TrimSpace(payload.IdempotencyKey) == "" {
		return errors.New("idempotency_key is empty")
	}
	return nil
}

func buildMemoryExtractIdempotencyKey(userID int, conversationID, sourceText string) string {
	raw := fmt.Sprintf("%d|%s|%s", userID, strings.TrimSpace(conversationID), strings.TrimSpace(sourceText))
	sum := sha256.Sum256([]byte(raw))
	return "memory_extract_" + strconv.Itoa(userID) + "_" + hex.EncodeToString(sum[:8])
}

func truncateByRune(raw string, max int) string {
	if max <= 0 {
		return ""
	}

	runes := []rune(raw)
	if len(runes) <= max {
		return raw
	}
	return string(runes[:max])
}

func isMemoryWriteEnabled() bool {
	if !viper.IsSet("memory.enabled") {
		return true
	}
	return viper.GetBool("memory.enabled")
}
