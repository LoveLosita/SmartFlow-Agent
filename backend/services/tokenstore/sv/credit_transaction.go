package sv

import (
	"context"
	"strings"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

// ListCreditTransactions 查询当前用户自己的 Credit 流水。
func (s *Service) ListCreditTransactions(ctx context.Context, req creditcontracts.ListCreditTransactionsRequest) ([]creditcontracts.CreditTransactionView, creditcontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	if req.ActorUserID == 0 {
		return nil, creditcontracts.PageResult{}, respond.MissingParam
	}

	page, pageSize := normalizePage(req.Page, req.PageSize)
	query := tokenstoredao.ListCreditTransactionsQuery{
		UserID:    req.ActorUserID,
		Page:      page,
		PageSize:  pageSize,
		Source:    strings.TrimSpace(req.Source),
		Direction: strings.TrimSpace(req.Direction),
	}

	total, err := s.creditDAO.CountTransactions(ctx, query)
	if err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	items, err := s.creditDAO.ListTransactions(ctx, query)
	if err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	if len(items) == 0 {
		return []creditcontracts.CreditTransactionView{}, creditPageResult(page, pageSize, total), nil
	}

	result := make([]creditcontracts.CreditTransactionView, 0, len(items))
	for _, item := range items {
		result = append(result, creditTransactionViewFromModel(item))
	}
	return result, creditPageResult(page, pageSize, total), nil
}
