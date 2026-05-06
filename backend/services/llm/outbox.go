package llm

import (
	"context"
	"log"
	"strings"
	"time"

	llmcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/llm"
	sharedevents "github.com/LoveLosita/smartflow/backend/shared/events"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"github.com/cloudwego/eino/schema"
	"github.com/google/uuid"
)

const (
	defaultOutboxMaxRetry       = 20
	defaultBillingPersistWindow = 2 * time.Second
)

// ChargeRecorder 负责把一次已完成的 LLM usage 写入 LLM 自己的 outbox。
type ChargeRecorder struct {
	publisher    *outboxinfra.RepositoryPublisher
	providerName string
	pricing      UsagePricingResolver
}

type ChargeRecorderOptions struct {
	Repo         *outboxinfra.Repository
	MaxRetry     int
	ProviderName string
	Pricing      UsagePricingResolver
}

func NewChargeRecorder(opts ChargeRecorderOptions) (*ChargeRecorder, error) {
	if err := RegisterCreditChargeRoute(); err != nil {
		return nil, err
	}

	providerName := strings.TrimSpace(opts.ProviderName)
	if providerName == "" {
		providerName = llmcontracts.ProviderNameArk
	}

	if opts.Repo == nil {
		return &ChargeRecorder{providerName: providerName}, nil
	}

	maxRetry := opts.MaxRetry
	if maxRetry <= 0 {
		maxRetry = defaultOutboxMaxRetry
	}
	return &ChargeRecorder{
		// 1. 当前 outbox infra 仍是“由归属服务自己 dispatch + consume 自己的 outbox”模型。
		// 2. 因此这里必须让 Repository 按事件归属把 credit 事件写进 token-store 的 outbox，
		//    不能再强绑到 llm 自己的 route，否则消息只会停在 published 而无人消费。
		publisher:    outboxinfra.NewRepositoryPublisher(opts.Repo, maxRetry),
		providerName: providerName,
		pricing:      opts.Pricing,
	}, nil
}

func RegisterCreditChargeRoute() error {
	return outboxinfra.RegisterEventService(sharedevents.CreditChargeRequestedEventType, outboxinfra.ServiceTokenStore)
}

func (r *ChargeRecorder) RecordTextUsage(ctx context.Context, billing BillingContext, modelAlias, modelName, defaultScene string, usage *schema.TokenUsage) error {
	if usage == nil {
		return nil
	}
	return r.publish(ctx, billing, publishUsageInput{
		ModelAlias:      modelAlias,
		ModelName:       modelName,
		DefaultScene:    defaultScene,
		InputTokens:     int64(usage.PromptTokens),
		OutputTokens:    int64(usage.CompletionTokens),
		CachedTokens:    int64(usage.PromptTokenDetails.CachedTokens),
		ReasoningTokens: int64(usage.CompletionTokensDetails.ReasoningTokens),
		TotalTokens:     int64(usage.TotalTokens),
	})
}

func (r *ChargeRecorder) RecordResponsesUsage(ctx context.Context, billing BillingContext, modelAlias, modelName, defaultScene string, usage *ArkResponsesUsage) error {
	if usage == nil {
		return nil
	}
	return r.publish(ctx, billing, publishUsageInput{
		ModelAlias:   modelAlias,
		ModelName:    modelName,
		DefaultScene: defaultScene,
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
	})
}

type publishUsageInput struct {
	ModelAlias      string
	ModelName       string
	DefaultScene    string
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	TotalTokens     int64
}

func (r *ChargeRecorder) publish(ctx context.Context, billing BillingContext, input publishUsageInput) error {
	if r == nil || r.publisher == nil {
		return nil
	}

	billing = billing.Normalize()
	if billing.UserID == 0 {
		return nil
	}

	eventID := firstNonEmptyString(strings.TrimSpace(billing.EventID), uuid.NewString())
	requestID := firstNonEmptyString(strings.TrimSpace(billing.RequestID), eventID)
	scene := firstNonEmptyString(strings.TrimSpace(billing.Scene), strings.TrimSpace(input.DefaultScene))
	modelAlias := firstNonEmptyString(strings.TrimSpace(billing.ModelAlias), strings.TrimSpace(input.ModelAlias))
	modelName := firstNonEmptyString(strings.TrimSpace(input.ModelName), modelAlias)
	totalTokens := input.TotalTokens
	if totalTokens <= 0 {
		totalTokens = input.InputTokens + input.OutputTokens
	}

	payload := sharedevents.CreditChargeRequestedPayload{
		EventID:         eventID,
		UserID:          billing.UserID,
		Scene:           scene,
		RequestID:       requestID,
		ConversationID:  strings.TrimSpace(billing.ConversationID),
		ModelAlias:      modelAlias,
		ProviderName:    r.providerName,
		ModelName:       modelName,
		InputTokens:     input.InputTokens,
		OutputTokens:    input.OutputTokens,
		CachedTokens:    input.CachedTokens,
		ReasoningTokens: input.ReasoningTokens,
		TotalTokens:     totalTokens,
		RMBCostMicros:   0,
		CreditCost:      0,
		TriggeredAt:     time.Now(),
		SkipCharge:      billing.SkipCharge,
	}
	if !billing.SkipCharge {
		quote, err := r.resolvePriceQuote(ctx, payload)
		if err != nil {
			log.Printf("llm price quote resolve failed: event_id=%s user_id=%d err=%v", payload.EventID, payload.UserID, err)
		} else {
			payload.RMBCostMicros = quote.RMBCostMicros
			payload.CreditCost = quote.CreditCost
		}
	}
	if err := payload.Validate(); err != nil {
		return err
	}

	recordCtx, cancel := detachedBillingContext(ctx)
	defer cancel()
	return r.publisher.Publish(recordCtx, outboxinfra.PublishRequest{
		EventID:      payload.EventID,
		EventType:    sharedevents.CreditChargeRequestedEventType,
		EventVersion: sharedevents.CreditChargeEventVersion,
		MessageKey:   payload.MessageKey(),
		AggregateID:  payload.AggregateID(),
		Payload:      payload,
	})
}

func (r *ChargeRecorder) resolvePriceQuote(ctx context.Context, payload sharedevents.CreditChargeRequestedPayload) (UsagePriceQuote, error) {
	if r == nil || r.pricing == nil {
		return UsagePriceQuote{}, nil
	}

	return r.pricing.Resolve(ctx, UsagePricingInput{
		Scene:           payload.Scene,
		ProviderName:    payload.ProviderName,
		ModelName:       payload.ModelName,
		InputTokens:     payload.InputTokens,
		OutputTokens:    payload.OutputTokens,
		CachedTokens:    payload.CachedTokens,
		ReasoningTokens: payload.ReasoningTokens,
	})
}

func detachedBillingContext(ctx context.Context) (context.Context, context.CancelFunc) {
	base := context.Background()
	if ctx != nil {
		base = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(base, defaultBillingPersistWindow)
}

func logChargeRecordError(scene string, err error) {
	if err == nil {
		return
	}
	log.Printf("llm charge record failed: scene=%s err=%v", strings.TrimSpace(scene), err)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
