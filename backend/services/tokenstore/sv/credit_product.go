package sv

import (
	"context"

	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
)

// ListCreditProducts 返回当前可售 Credit 商品列表。
func (s *Service) ListCreditProducts(ctx context.Context, actorUserID uint64) ([]creditcontracts.CreditProductView, error) {
	_ = actorUserID
	if err := s.Ready(); err != nil {
		return nil, err
	}

	products, err := s.creditDAO.ListActiveProducts(ctx)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return []creditcontracts.CreditProductView{}, nil
	}

	result := make([]creditcontracts.CreditProductView, 0, len(products))
	for _, product := range products {
		result = append(result, creditProductViewFromModel(product))
	}
	return result, nil
}
