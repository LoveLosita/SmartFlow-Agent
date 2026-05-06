package llm

import (
	"context"
	"strings"

	llmdao "github.com/LoveLosita/smartflow/backend/services/llm/dao"
	llmcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/llm"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"github.com/cloudwego/eino/schema"
)

// RuntimeService 是独立 LLM 进程对外暴露的业务门面。
//
// 职责边界：
// 1. 负责模型别名选择、BillingContext 注入、准入守卫与 outbox 写入；
// 2. 不负责 prompt 编排，调用方仍然直接传入 messages；
// 3. 不负责价格换算细则，本轮先把 usage 事件稳定写入 outbox，价格字段留给后续主代理接线。
type RuntimeService struct {
	legacy          *Service
	textClients     map[string]*Client
	textModelNames  map[string]string
	responsesClient *ArkResponsesClient
	responsesModel  string
	balanceGuard    *CreditBalanceGuard
	chargeRecorder  *ChargeRecorder
	defaultProvider string
}

type RuntimeServiceOptions struct {
	LegacyService     *Service
	CacheDAO          *llmdao.CacheDAO
	PriceRuleDAO      *llmdao.PriceRuleDAO
	SnapshotProvider  CreditBalanceSnapshotProvider
	OutboxRepo        *outboxinfra.Repository
	OutboxMaxRetry    int
	ProviderName      string
	LiteModelName     string
	ProModelName      string
	MaxModelName      string
	CourseVisionModel string
}

func NewRuntimeService(opts RuntimeServiceOptions) (*RuntimeService, error) {
	if opts.LegacyService == nil {
		return nil, ErrRuntimeServiceNotReady
	}

	chargeRecorder, err := NewChargeRecorder(ChargeRecorderOptions{
		Repo:         opts.OutboxRepo,
		MaxRetry:     opts.OutboxMaxRetry,
		ProviderName: opts.ProviderName,
		Pricing:      NewCreditPriceResolver(CreditPriceResolverOptions{DAO: opts.PriceRuleDAO}),
	})
	if err != nil {
		return nil, err
	}

	return &RuntimeService{
		legacy: opts.LegacyService,
		textClients: map[string]*Client{
			llmcontracts.ModelAliasLite: opts.LegacyService.LiteClient(),
			llmcontracts.ModelAliasPro:  opts.LegacyService.ProClient(),
			llmcontracts.ModelAliasMax:  opts.LegacyService.MaxClient(),
		},
		textModelNames: map[string]string{
			llmcontracts.ModelAliasLite: strings.TrimSpace(opts.LiteModelName),
			llmcontracts.ModelAliasPro:  strings.TrimSpace(opts.ProModelName),
			llmcontracts.ModelAliasMax:  strings.TrimSpace(opts.MaxModelName),
		},
		responsesClient: opts.LegacyService.CourseImageResponsesClient(),
		responsesModel:  strings.TrimSpace(opts.CourseVisionModel),
		balanceGuard: NewCreditBalanceGuard(CreditBalanceGuardOptions{
			CacheDAO:         opts.CacheDAO,
			SnapshotProvider: opts.SnapshotProvider,
		}),
		chargeRecorder:  chargeRecorder,
		defaultProvider: firstNonEmptyString(strings.TrimSpace(opts.ProviderName), llmcontracts.ProviderNameArk),
	}, nil
}

func (s *RuntimeService) LegacyService() *Service {
	if s == nil {
		return nil
	}
	return s.legacy
}

// GenerateText 负责处理一次非流式文本调用。
func (s *RuntimeService) GenerateText(ctx context.Context, req llmcontracts.TextRequest) (*TextResult, error) {
	client, alias, modelName, err := s.resolveTextClient(req.ModelAlias)
	if err != nil {
		return nil, err
	}

	// 1. 先把跨进程 billing 副本还原回 ctx，保持业务侧调用面不改签名。
	// 2. 再做一次 Redis 快照级准入守卫；守卫失败直接短路，不继续发起模型调用。
	// 3. 模型成功后同步写 LLM outbox；写失败只打日志，避免因为记账侧抖动反向打挂主链路。
	ctx, billing := applyRequestBillingContext(ctx, req.Billing, alias)
	billing = EnsureTextBillingIdentity(billing, req.Options, req.Messages)
	if !billing.IsZero() {
		ctx = WithBillingContext(ctx, billing)
	}
	if err = s.balanceGuard.Guard(ctx, billing); err != nil {
		return nil, err
	}

	result, err := client.GenerateText(ctx, req.Messages, toServiceGenerateOptions(req.Options))
	if err != nil {
		return nil, err
	}
	logChargeRecordError("llm.text.generate", s.chargeRecorder.RecordTextUsage(ctx, billing, alias, modelName, "llm.text.generate", result.Usage))
	return result, nil
}

// StreamText 负责处理一次流式文本调用。
func (s *RuntimeService) StreamText(ctx context.Context, req llmcontracts.StreamTextRequest) (StreamReader, error) {
	client, alias, modelName, err := s.resolveTextClient(req.ModelAlias)
	if err != nil {
		return nil, err
	}

	ctx, billing := applyRequestBillingContext(ctx, req.Billing, alias)
	billing = EnsureTextBillingIdentity(billing, req.Options, req.Messages)
	if !billing.IsZero() {
		ctx = WithBillingContext(ctx, billing)
	}
	if err = s.balanceGuard.Guard(ctx, billing); err != nil {
		return nil, err
	}

	reader, err := client.Stream(ctx, req.Messages, toServiceGenerateOptions(req.Options))
	if err != nil {
		return nil, err
	}
	return NewUsageAccountingStreamReader(reader, func(usage *schema.TokenUsage) {
		logChargeRecordError("llm.text.stream", s.chargeRecorder.RecordTextUsage(ctx, billing, alias, modelName, "llm.text.stream", usage))
	}), nil
}

// GenerateResponsesText 负责处理课程图片解析使用的 Responses 文本调用。
func (s *RuntimeService) GenerateResponsesText(ctx context.Context, req llmcontracts.ResponsesRequest) (*ArkResponsesResult, error) {
	client, alias, modelName, err := s.resolveResponsesClient(req.ModelAlias)
	if err != nil {
		return nil, err
	}

	ctx, billing := applyRequestBillingContext(ctx, req.Billing, alias)
	billing = EnsureResponsesBillingIdentity(billing, req.Messages)
	if !billing.IsZero() {
		ctx = WithBillingContext(ctx, billing)
	}
	if err = s.balanceGuard.Guard(ctx, billing); err != nil {
		return nil, err
	}

	result, err := client.GenerateText(ctx, toServiceResponsesMessages(req.Messages), toServiceResponsesOptions(req.Options))
	if err != nil {
		return nil, err
	}
	logChargeRecordError("llm.responses.generate", s.chargeRecorder.RecordResponsesUsage(ctx, billing, alias, modelName, "llm.responses.generate", result.Usage))
	return result, nil
}

func (s *RuntimeService) resolveTextClient(modelAlias string) (*Client, string, string, error) {
	if s == nil {
		return nil, "", "", ErrRuntimeServiceNotReady
	}

	alias := llmcontracts.NormalizeModelAlias(modelAlias)
	client, ok := s.textClients[alias]
	if !ok {
		return nil, alias, "", ErrUnsupportedModelAlias
	}
	if client == nil {
		return nil, alias, "", ErrRuntimeServiceNotReady
	}
	return client, alias, firstNonEmptyString(s.textModelNames[alias], alias), nil
}

func (s *RuntimeService) resolveResponsesClient(modelAlias string) (*ArkResponsesClient, string, string, error) {
	if s == nil || s.responsesClient == nil {
		return nil, "", "", ErrRuntimeServiceNotReady
	}

	alias := strings.TrimSpace(modelAlias)
	if alias == "" {
		alias = llmcontracts.ModelAliasCourseImageResponses
	}
	if alias != llmcontracts.ModelAliasCourseImageResponses {
		return nil, alias, "", ErrUnsupportedModelAlias
	}
	return s.responsesClient, alias, firstNonEmptyString(s.responsesModel, alias), nil
}

func applyRequestBillingContext(ctx context.Context, input *llmcontracts.BillingContext, modelAlias string) (context.Context, BillingContext) {
	billing := BillingContext{}
	if input != nil {
		billing = BillingContext{
			UserID:         input.UserID,
			EventID:        input.EventID,
			Scene:          input.Scene,
			RequestID:      input.RequestID,
			ConversationID: input.ConversationID,
			ModelAlias:     input.ModelAlias,
			SkipCharge:     input.SkipCharge,
		}
	}
	if strings.TrimSpace(billing.ModelAlias) == "" {
		billing.ModelAlias = strings.TrimSpace(modelAlias)
	}
	if billing.IsZero() {
		return ctx, billing
	}
	return WithBillingContext(ctx, billing), billing
}

func toServiceGenerateOptions(input llmcontracts.GenerateOptions) GenerateOptions {
	return GenerateOptions{
		Temperature: input.Temperature,
		MaxTokens:   input.MaxTokens,
		Thinking:    ThinkingMode(strings.TrimSpace(input.Thinking)),
		Metadata:    input.Metadata,
	}
}

func toServiceResponsesMessages(input []llmcontracts.ResponsesMessage) []ArkResponsesMessage {
	if len(input) == 0 {
		return nil
	}
	output := make([]ArkResponsesMessage, 0, len(input))
	for _, item := range input {
		output = append(output, ArkResponsesMessage{
			Role:        item.Role,
			Text:        item.Text,
			ImageURL:    item.ImageURL,
			ImageDetail: item.ImageDetail,
		})
	}
	return output
}

func toServiceResponsesOptions(input llmcontracts.ResponsesOptions) ArkResponsesOptions {
	return ArkResponsesOptions{
		Model:           input.Model,
		Temperature:     input.Temperature,
		MaxOutputTokens: input.MaxOutputTokens,
		Thinking:        ThinkingMode(strings.TrimSpace(input.Thinking)),
		TextFormat:      input.TextFormat,
	}
}

func toContractTextResult(result *TextResult) *llmcontracts.TextResult {
	if result == nil {
		return nil
	}
	return &llmcontracts.TextResult{
		Text:         result.Text,
		Usage:        CloneUsage(result.Usage),
		FinishReason: result.FinishReason,
	}
}

func toContractResponsesResult(result *ArkResponsesResult) *llmcontracts.ResponsesResult {
	if result == nil {
		return nil
	}
	output := &llmcontracts.ResponsesResult{
		Text:             result.Text,
		Status:           result.Status,
		IncompleteReason: result.IncompleteReason,
		ErrorCode:        result.ErrorCode,
		ErrorMessage:     result.ErrorMessage,
	}
	if result.Usage != nil {
		output.Usage = &llmcontracts.ResponsesUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		}
	}
	return output
}

func toServiceTextResult(result *llmcontracts.TextResult) *TextResult {
	if result == nil {
		return nil
	}
	return &TextResult{
		Text:         result.Text,
		Usage:        CloneUsage(result.Usage),
		FinishReason: result.FinishReason,
	}
}

func toServiceResponsesResult(result *llmcontracts.ResponsesResult) *ArkResponsesResult {
	if result == nil {
		return nil
	}
	output := &ArkResponsesResult{
		Text:             result.Text,
		Status:           result.Status,
		IncompleteReason: result.IncompleteReason,
		ErrorCode:        result.ErrorCode,
		ErrorMessage:     result.ErrorMessage,
	}
	if result.Usage != nil {
		output.Usage = &ArkResponsesUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		}
	}
	return output
}
