package sv

import (
	"context"
	"strconv"
	"strings"
	"time"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	storemodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
)

// RecordForumRewardCredit 把论坛点赞/导入奖励直接写入 Credit 权威账本。
//
// 职责边界：
// 1. 只处理 forum_like / forum_import 两类论坛正向奖励；
// 2. 复用 event_id 做最终幂等键，重复消费时直接返回既有账本结果；
// 3. 奖励金额优先读取 credit_reward_rules，规则缺失时再走默认兜底。
func (s *Service) RecordForumRewardCredit(ctx context.Context, req forumRewardGrantRequest) (*creditcontracts.CreditTransactionView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}

	decision, err := s.forumRewardCreditDecision(ctx, req.Source)
	if err != nil {
		return nil, err
	}

	sourceRefID := strconv.FormatUint(req.SourceRefID, 10)
	ledger, _, err := s.applyCreditLedger(ctx, applyCreditLedgerRequest{
		EventID:      req.EventID,
		UserID:       req.ReceiverUserID,
		Source:       req.Source,
		SourceLabel:  creditSourceLabel(req.Source, ""),
		Direction:    creditDirectionFromAmount(decision.Amount),
		SourceRefID:  &sourceRefID,
		Amount:       decision.Amount,
		Status:       decision.Status,
		Description:  decision.Description,
		MetadataJSON: creditMetadataJSON(map[string]any{"reward_source": req.Source, "source_ref_id": req.SourceRefID}),
		CreatedAt:    time.Now(),
	})
	if err != nil {
		return nil, err
	}

	view := creditTransactionViewFromModel(*ledger)
	return &view, nil
}

func (s *Service) forumRewardCreditDecision(ctx context.Context, source string) (forumRewardDecision, error) {
	rules, err := s.creditDAO.ListRewardRules(ctx, tokenstoredao.ListCreditRewardRulesQuery{
		Source: strings.TrimSpace(source),
	})
	if err != nil {
		return forumRewardDecision{}, err
	}
	if len(rules) > 0 {
		rule := rules[0]
		if strings.TrimSpace(rule.Status) != storemodel.CreditRewardRuleStatusActive {
			return skippedForumRewardDecision(source, "奖励规则已停用，未发放 Credit"), nil
		}
		if rule.Amount <= 0 {
			return skippedForumRewardDecision(source, "奖励规则金额非正，未发放 Credit"), nil
		}
		return forumRewardDecision{
			Amount:      rule.Amount,
			Status:      storemodel.CreditLedgerStatusApplied,
			Description: forumRewardDescription(source),
		}, nil
	}

	switch strings.TrimSpace(source) {
	case storemodel.CreditLedgerSourceForumLike:
		return forumRewardDecision{
			Amount:      defaultForumLikeRewardAmount,
			Status:      storemodel.CreditLedgerStatusApplied,
			Description: forumRewardDescription(source),
		}, nil
	case storemodel.CreditLedgerSourceForumImport:
		return forumRewardDecision{
			Amount:      defaultForumImportRewardAmount,
			Status:      storemodel.CreditLedgerStatusApplied,
			Description: forumRewardDescription(source),
		}, nil
	default:
		return skippedForumRewardDecision(source, "未知论坛奖励来源，未发放 Credit"), nil
	}
}
