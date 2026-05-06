package llm

import (
	"context"
	"strings"
	"sync"
	"time"

	llmdao "github.com/LoveLosita/smartflow/backend/services/llm/dao"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
)

const (
	defaultPriceRuleCacheTTL = time.Minute
	tokenPriceScalePer1K     = int64(1000)
	rmbMicrosPerYuan         = int64(1_000_000)
)

type UsagePricingInput struct {
	Scene           string
	ProviderName    string
	ModelName       string
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
}

type UsagePriceQuote struct {
	RuleID          uint64
	RMBCostMicros   int64
	CreditCost      int64
	MatchedScene    string
	MatchedProvider string
	MatchedModel    string
}

type UsagePricingResolver interface {
	Resolve(ctx context.Context, input UsagePricingInput) (UsagePriceQuote, error)
}

type CreditPriceResolverOptions struct {
	DAO      *llmdao.PriceRuleDAO
	CacheTTL time.Duration
}

type CreditPriceResolver struct {
	dao      *llmdao.PriceRuleDAO
	cacheTTL time.Duration

	mu        sync.RWMutex
	cachedAt  time.Time
	cachedSet []llmdao.CreditPriceRule
}

func NewCreditPriceResolver(opts CreditPriceResolverOptions) *CreditPriceResolver {
	cacheTTL := opts.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultPriceRuleCacheTTL
	}
	return &CreditPriceResolver{
		dao:      opts.DAO,
		cacheTTL: cacheTTL,
	}
}

func (r *CreditPriceResolver) Resolve(ctx context.Context, input UsagePricingInput) (UsagePriceQuote, error) {
	if r == nil || r.dao == nil {
		return UsagePriceQuote{}, nil
	}

	rules, err := r.loadRules(ctx)
	if err != nil {
		return UsagePriceQuote{}, err
	}
	if len(rules) == 0 {
		return UsagePriceQuote{}, nil
	}

	scene := strings.TrimSpace(input.Scene)
	providerName := strings.TrimSpace(input.ProviderName)
	modelName := strings.TrimSpace(input.ModelName)

	for _, rule := range rules {
		if !matchesPriceRuleField(rule.Scene, scene) {
			continue
		}
		if !matchesPriceRuleField(rule.ProviderName, providerName) {
			continue
		}
		if !matchesPriceRuleField(rule.ModelName, modelName) {
			continue
		}
		return quoteUsagePrice(rule, input), nil
	}

	return UsagePriceQuote{}, nil
}

func (r *CreditPriceResolver) loadRules(ctx context.Context) ([]llmdao.CreditPriceRule, error) {
	now := time.Now()

	r.mu.RLock()
	if len(r.cachedSet) > 0 && now.Sub(r.cachedAt) < r.cacheTTL {
		rules := clonePriceRules(r.cachedSet)
		r.mu.RUnlock()
		return rules, nil
	}
	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.cachedSet) > 0 && now.Sub(r.cachedAt) < r.cacheTTL {
		return clonePriceRules(r.cachedSet), nil
	}

	rules, err := r.dao.ListActiveRules(ctx)
	if err != nil {
		return nil, err
	}

	r.cachedSet = clonePriceRules(rules)
	r.cachedAt = now
	return clonePriceRules(r.cachedSet), nil
}

func clonePriceRules(input []llmdao.CreditPriceRule) []llmdao.CreditPriceRule {
	if len(input) == 0 {
		return nil
	}
	output := make([]llmdao.CreditPriceRule, len(input))
	copy(output, input)
	return output
}

func matchesPriceRuleField(ruleValue string, actual string) bool {
	ruleValue = strings.TrimSpace(ruleValue)
	actual = strings.TrimSpace(actual)

	if ruleValue == "" || ruleValue == "*" {
		return true
	}
	return strings.EqualFold(ruleValue, actual)
}

func quoteUsagePrice(rule llmdao.CreditPriceRule, input UsagePricingInput) UsagePriceQuote {
	inputTokens := maxInt64(input.InputTokens, 0)
	outputTokens := maxInt64(input.OutputTokens, 0)
	cachedTokens := clampInt64(input.CachedTokens, 0, inputTokens)
	reasoningTokens := clampInt64(input.ReasoningTokens, 0, outputTokens)

	nonCachedInputTokens := inputTokens - cachedTokens
	nonReasoningOutputTokens := outputTokens - reasoningTokens

	chargePrices := creditcontracts.DeriveChargePriceMicrosSet(
		rule.InputPriceMicros,
		rule.OutputPriceMicros,
		rule.CachedPriceMicros,
		rule.ReasoningPriceMicros,
		rule.ProfitRateBps,
	)

	totalMicrosScaled := nonCachedInputTokens*maxInt64(chargePrices.InputChargePriceMicros, 0) +
		cachedTokens*maxInt64(chargePrices.CachedChargePriceMicros, 0) +
		nonReasoningOutputTokens*maxInt64(chargePrices.OutputChargePriceMicros, 0) +
		reasoningTokens*maxInt64(chargePrices.ReasoningChargePriceMicros, 0)

	rmbCostMicros := ceilDivInt64(totalMicrosScaled, tokenPriceScalePer1K)
	creditCost := int64(0)
	if rmbCostMicros > 0 && rule.CreditPerYuan > 0 {
		creditCost = ceilDivInt64(rmbCostMicros*rule.CreditPerYuan, rmbMicrosPerYuan)
	}

	return UsagePriceQuote{
		RuleID:          rule.ID,
		RMBCostMicros:   rmbCostMicros,
		CreditCost:      creditCost,
		MatchedScene:    strings.TrimSpace(rule.Scene),
		MatchedProvider: strings.TrimSpace(rule.ProviderName),
		MatchedModel:    strings.TrimSpace(rule.ModelName),
	}
}

func ceilDivInt64(numerator int64, denominator int64) int64 {
	if numerator <= 0 || denominator <= 0 {
		return 0
	}
	return (numerator + denominator - 1) / denominator
}

func clampInt64(value int64, minValue int64, maxValue int64) int64 {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func maxInt64(value int64, minValue int64) int64 {
	if value < minValue {
		return minValue
	}
	return value
}
