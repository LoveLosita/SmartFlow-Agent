package sv

import (
	"context"

	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
)

// ListProducts 返回当前可售商品列表。
//
// 职责边界：
// 1. 只返回 active 商品，不负责后台商品管理。
// 2. 负责补齐 price_text，保持前端不必重复格式化价格。
// 3. actorUserID 当前仅保留为统一接口形状，P0 不参与筛选。
func (s *Service) ListProducts(ctx context.Context, actorUserID uint64) ([]tokencontracts.TokenProductView, error) {
	_ = actorUserID
	if err := s.Ready(); err != nil {
		return nil, err
	}

	products, err := s.tokenDAO.ListActiveProducts(ctx)
	if err != nil {
		return nil, err
	}
	if len(products) == 0 {
		return []tokencontracts.TokenProductView{}, nil
	}

	result := make([]tokencontracts.TokenProductView, 0, len(products))
	for _, product := range products {
		result = append(result, productViewFromModel(product))
	}
	return result, nil
}
