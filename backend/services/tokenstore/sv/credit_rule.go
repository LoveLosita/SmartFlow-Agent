package sv

import (
	"context"
	"strings"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
)

// ListCreditPriceRules 查询 Credit 价格规则。
func (s *Service) ListCreditPriceRules(ctx context.Context, req creditcontracts.ListCreditPriceRulesRequest) ([]creditcontracts.CreditPriceRuleView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}

	rules, err := s.creditDAO.ListPriceRules(ctx, tokenstoredao.ListCreditPriceRulesQuery{
		Scene:        strings.TrimSpace(req.Scene),
		ProviderName: strings.TrimSpace(req.ProviderName),
		ModelName:    strings.TrimSpace(req.ModelName),
		Status:       strings.TrimSpace(req.Status),
	})
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return []creditcontracts.CreditPriceRuleView{}, nil
	}

	result := make([]creditcontracts.CreditPriceRuleView, 0, len(rules))
	for _, rule := range rules {
		result = append(result, creditPriceRuleViewFromModel(rule))
	}
	return result, nil
}

// ListCreditRewardRules 查询 Credit 奖励规则。
func (s *Service) ListCreditRewardRules(ctx context.Context, req creditcontracts.ListCreditRewardRulesRequest) ([]creditcontracts.CreditRewardRuleView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}

	rules, err := s.creditDAO.ListRewardRules(ctx, tokenstoredao.ListCreditRewardRulesQuery{
		Source: strings.TrimSpace(req.Source),
		Status: strings.TrimSpace(req.Status),
	})
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return []creditcontracts.CreditRewardRuleView{}, nil
	}

	result := make([]creditcontracts.CreditRewardRuleView, 0, len(rules))
	for _, rule := range rules {
		result = append(result, creditRewardRuleViewFromModel(rule))
	}
	return result, nil
}
