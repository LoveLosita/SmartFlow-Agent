package sv

import (
	"context"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	tokenmodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
)

// CreateOrder 创建 Token 商品订单。
//
// 职责边界：
// 1. 校验 actor_user_id、product_id、quantity 与幂等键。
// 2. 只生成 pending 订单和商品快照，不触发真实支付或 user/auth 同步。
// 3. 并发冲突时优先按 user_id + idempotency_key 回查旧单，保证 P0 幂等语义。
func (s *Service) CreateOrder(ctx context.Context, req tokencontracts.CreateTokenOrderRequest) (*tokencontracts.TokenOrderView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 || req.ProductID == 0 {
		return nil, respond.MissingParam
	}
	if req.Quantity < 1 || req.Quantity > 99 {
		return nil, tokenStoreBadRequest("quantity 仅支持 1 到 99")
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey != "" {
		existing, err := s.tokenDAO.FindOrderByUserIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return s.orderViewByID(ctx, req.ActorUserID, existing.ID)
		}
	}

	product, err := s.tokenDAO.FindActiveProductByID(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, tokenStoreBadRequest("商品不存在或已下架")
	}

	productSnapshot, err := buildProductSnapshot(*product)
	if err != nil {
		return nil, err
	}

	order := tokenmodel.TokenOrder{
		OrderNo:             newOrderNo(),
		UserID:              req.ActorUserID,
		ProductID:           product.ID,
		ProductSKU:          product.SKU,
		ProductName:         product.Name,
		ProductSnapshotJSON: productSnapshot,
		Quantity:            req.Quantity,
		TokenAmount:         product.TokenAmount * int64(req.Quantity),
		AmountCent:          product.PriceCent * int64(req.Quantity),
		Currency:            product.Currency,
		Status:              tokenmodel.TokenOrderStatusPending,
		PaymentMode:         "mock",
		IdempotencyKey:      stringPtrFromNonEmpty(idempotencyKey),
	}
	if err := s.tokenDAO.CreateOrder(ctx, &order); err != nil {
		if idempotencyKey != "" && isDuplicateKeyError(err) {
			existing, findErr := s.tokenDAO.FindOrderByUserIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return s.orderViewByID(ctx, req.ActorUserID, existing.ID)
			}
		}
		return nil, err
	}

	return s.orderViewByID(ctx, req.ActorUserID, order.ID)
}

// ListOrders 按用户分页查询订单列表。
//
// 职责边界：
// 1. 只支持当前用户维度分页，不做跨用户检索。
// 2. status 为空时不过滤，非空时按精确值过滤。
// 3. 负责把订单与 grant 账本拼装成统一视图。
func (s *Service) ListOrders(ctx context.Context, req tokencontracts.ListTokenOrdersRequest) ([]tokencontracts.TokenOrderView, tokencontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	if req.ActorUserID == 0 {
		return nil, tokencontracts.PageResult{}, respond.MissingParam
	}

	page, pageSize := normalizePage(req.Page, req.PageSize)
	query := tokenstoredao.ListTokenOrdersQuery{
		UserID:   req.ActorUserID,
		Page:     page,
		PageSize: pageSize,
		Status:   strings.TrimSpace(req.Status),
	}

	total, err := s.tokenDAO.CountOrders(ctx, query)
	if err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	orders, err := s.tokenDAO.ListOrders(ctx, query)
	if err != nil {
		return nil, tokencontracts.PageResult{}, err
	}
	if len(orders) == 0 {
		return []tokencontracts.TokenOrderView{}, pageResult(page, pageSize, total), nil
	}

	grantMap, err := s.orderGrantMap(ctx, collectOrderIDs(orders))
	if err != nil {
		return nil, tokencontracts.PageResult{}, err
	}

	result := make([]tokencontracts.TokenOrderView, 0, len(orders))
	for _, order := range orders {
		result = append(result, orderViewFromModel(order, grantMap[order.ID]))
	}
	return result, pageResult(page, pageSize, total), nil
}

// GetOrder 查询单个订单详情，并校验归属用户。
func (s *Service) GetOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*tokencontracts.TokenOrderView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if actorUserID == 0 || orderID == 0 {
		return nil, respond.MissingParam
	}
	return s.orderViewByID(ctx, actorUserID, orderID)
}

// MockPaidOrder 在同步事务里完成 mock paid 和 grant 入账。
//
// 职责边界：
// 1. 只处理订单状态流转与 token_grants 幂等写入，不调用 user/auth。
// 2. event_id 固定为 order:{order_id}:paid，作为最终 grant 幂等边界。
// 3. 重复调用优先复用既有 grant，再把订单补齐到 granted，避免重复写账本。
func (s *Service) MockPaidOrder(ctx context.Context, req tokencontracts.MockPaidOrderRequest) (*tokencontracts.TokenOrderView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 || req.OrderID == 0 {
		return nil, respond.MissingParam
	}

	var resultOrder tokenmodel.TokenOrder
	var resultGrant *tokenmodel.TokenGrant
	err := s.tokenDAO.Transaction(ctx, func(txDAO *tokenstoredao.TokenStoreDAO) error {
		now := time.Now()

		// 1. 先锁订单并校验归属，避免并发 mock paid 重复写 grant。
		// 2. 订单不存在直接返回；订单不属于当前用户时明确拒绝。
		// 3. closed 状态不允许继续支付，避免把关闭单重新拉回可用态。
		order, err := txDAO.LockOrderByID(ctx, req.OrderID)
		if err != nil {
			return normalizeRecordNotFound(err, tokenStoreBadRequest("订单不存在"))
		}
		if order.UserID != req.ActorUserID {
			return tokenStoreBadRequest("订单不属于当前用户")
		}
		switch order.Status {
		case tokenmodel.TokenOrderStatusPending, tokenmodel.TokenOrderStatusPaid, tokenmodel.TokenOrderStatusGranted:
		case tokenmodel.TokenOrderStatusClosed:
			return tokenStoreBadRequest("订单已关闭，不能执行 mock paid")
		default:
			return tokenStoreBadRequest("订单状态不支持执行 mock paid")
		}

		eventID := purchaseGrantEventID(order.ID)
		grant, err := txDAO.FindGrantByEventID(ctx, eventID)
		if err != nil {
			return err
		}

		// 1. grant 不存在时才尝试创建，保证账本幂等写入边界只在 event_id。
		// 2. 即使因为历史脏数据或极端并发触发唯一冲突，也要立刻按 event_id 反查旧 grant。
		// 3. 这里不写 user/auth，只把 token-store 自己的账本事实补齐。
		if grant == nil {
			sourceRefID := order.ID
			orderID := order.ID
			newGrant := &tokenmodel.TokenGrant{
				EventID:      eventID,
				UserID:       order.UserID,
				Source:       tokenmodel.TokenGrantSourcePurchase,
				SourceLabel:  grantSourceLabel(tokenmodel.TokenGrantSourcePurchase, ""),
				SourceRefID:  &sourceRefID,
				OrderID:      &orderID,
				Amount:       order.TokenAmount,
				Status:       tokenmodel.TokenGrantStatusRecorded,
				QuotaApplied: false,
				Description:  purchaseGrantDescription(order.ProductName),
			}
			if err := txDAO.CreateGrant(ctx, newGrant); err != nil {
				if !isDuplicateKeyError(err) {
					return err
				}
				newGrant, err = txDAO.FindGrantByEventID(ctx, eventID)
				if err != nil {
					return err
				}
				if newGrant == nil {
					return tokenStoreBadRequest("Token 发放记录创建后未找到")
				}
			}
			grant = newGrant
		}

		// 1. 无论订单原来是 pending、paid 还是 granted，只要 grant 已确定，就把订单补齐到 granted。
		// 2. paid_at 缺失时使用本次确认时间；granted_at 缺失时优先复用 grant.created_at，保证链路时间可追溯。
		// 3. 这样即便出现“grant 已有、订单未完成切流”的历史半状态，也能在重复调用时自愈。
		paidAt := order.PaidAt
		if paidAt == nil || paidAt.IsZero() {
			paidAt = &now
		}
		grantedAt := order.GrantedAt
		if grantedAt == nil || grantedAt.IsZero() {
			if grant != nil && !grant.CreatedAt.IsZero() {
				grantCreatedAt := grant.CreatedAt
				grantedAt = &grantCreatedAt
			} else {
				grantedAt = &now
			}
		}
		paymentMode := strings.TrimSpace(order.PaymentMode)
		if paymentMode == "" {
			paymentMode = strings.TrimSpace(req.MockChannel)
		}
		if paymentMode == "" {
			paymentMode = "mock"
		}
		if err := txDAO.UpdateOrderState(ctx, order.ID, tokenmodel.TokenOrderStatusGranted, paidAt, grantedAt, paymentMode); err != nil {
			return err
		}

		order.Status = tokenmodel.TokenOrderStatusGranted
		order.PaidAt = paidAt
		order.GrantedAt = grantedAt
		order.PaymentMode = paymentMode
		resultOrder = *order
		resultGrant = grant
		return nil
	})
	if err != nil {
		return nil, err
	}

	view := orderViewFromModel(resultOrder, resultGrant)
	return &view, nil
}

func (s *Service) orderViewByID(ctx context.Context, actorUserID uint64, orderID uint64) (*tokencontracts.TokenOrderView, error) {
	order, err := s.tokenDAO.FindOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, tokenStoreBadRequest("订单不存在")
	}
	if order.UserID != actorUserID {
		return nil, tokenStoreBadRequest("订单不属于当前用户")
	}

	grant, err := s.tokenDAO.FindGrantByOrderID(ctx, order.ID)
	if err != nil {
		return nil, err
	}
	view := orderViewFromModel(*order, grant)
	return &view, nil
}

func (s *Service) orderGrantMap(ctx context.Context, orderIDs []uint64) (map[uint64]*tokenmodel.TokenGrant, error) {
	result := make(map[uint64]*tokenmodel.TokenGrant, len(orderIDs))
	if len(orderIDs) == 0 {
		return result, nil
	}

	grants, err := s.tokenDAO.ListGrantsByOrderIDs(ctx, orderIDs)
	if err != nil {
		return nil, err
	}
	for i := range grants {
		grant := grants[i]
		if grant.OrderID == nil {
			continue
		}
		if _, exists := result[*grant.OrderID]; exists {
			continue
		}
		grantCopy := grant
		result[*grant.OrderID] = &grantCopy
	}
	return result, nil
}

func collectOrderIDs(orders []tokenmodel.TokenOrder) []uint64 {
	result := make([]uint64, 0, len(orders))
	for _, order := range orders {
		result = append(result, order.ID)
	}
	return result
}
