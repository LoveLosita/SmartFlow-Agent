package sv

import (
	"context"
	"strings"
	"time"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	storemodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

// CreateCreditOrder 创建 Credit 商品订单。
func (s *Service) CreateCreditOrder(ctx context.Context, req creditcontracts.CreateCreditOrderRequest) (*creditcontracts.CreditOrderView, error) {
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
		existing, err := s.creditDAO.FindOrderByUserIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return s.creditOrderViewByID(ctx, req.ActorUserID, existing.ID)
		}
	}

	product, err := s.creditDAO.FindActiveProductByID(ctx, req.ProductID)
	if err != nil {
		return nil, err
	}
	if product == nil {
		return nil, tokenStoreBadRequest("Credit 商品不存在或已下架")
	}

	snapshot, err := buildCreditProductSnapshot(*product)
	if err != nil {
		return nil, err
	}

	order := storemodel.CreditOrder{
		OrderNo:             newCreditOrderNo(),
		UserID:              req.ActorUserID,
		ProductID:           product.ID,
		ProductSKU:          product.SKU,
		ProductName:         product.Name,
		ProductSnapshotJSON: snapshot,
		Quantity:            req.Quantity,
		CreditAmount:        product.CreditAmount * int64(req.Quantity),
		AmountCent:          product.PriceCent * int64(req.Quantity),
		Currency:            product.Currency,
		Status:              storemodel.CreditOrderStatusPending,
		PaymentMode:         "mock",
		IdempotencyKey:      stringPtrFromNonEmpty(idempotencyKey),
	}
	if err := s.creditDAO.CreateOrder(ctx, &order); err != nil {
		if idempotencyKey != "" && isDuplicateKeyError(err) {
			existing, findErr := s.creditDAO.FindOrderByUserIdempotencyKey(ctx, req.ActorUserID, idempotencyKey)
			if findErr != nil {
				return nil, findErr
			}
			if existing != nil {
				return s.creditOrderViewByID(ctx, req.ActorUserID, existing.ID)
			}
		}
		return nil, err
	}

	return s.creditOrderViewByID(ctx, req.ActorUserID, order.ID)
}

// ListCreditOrders 按用户分页查询 Credit 订单。
func (s *Service) ListCreditOrders(ctx context.Context, req creditcontracts.ListCreditOrdersRequest) ([]creditcontracts.CreditOrderView, creditcontracts.PageResult, error) {
	if err := s.Ready(); err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	if req.ActorUserID == 0 {
		return nil, creditcontracts.PageResult{}, respond.MissingParam
	}

	page, pageSize := normalizePage(req.Page, req.PageSize)
	query := tokenstoredao.ListCreditOrdersQuery{
		UserID:   req.ActorUserID,
		Page:     page,
		PageSize: pageSize,
		Status:   strings.TrimSpace(req.Status),
	}

	total, err := s.creditDAO.CountOrders(ctx, query)
	if err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	orders, err := s.creditDAO.ListOrders(ctx, query)
	if err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	if len(orders) == 0 {
		return []creditcontracts.CreditOrderView{}, creditPageResult(page, pageSize, total), nil
	}

	result := make([]creditcontracts.CreditOrderView, 0, len(orders))
	for _, order := range orders {
		result = append(result, creditOrderViewFromModel(order))
	}
	return result, creditPageResult(page, pageSize, total), nil
}

// GetCreditOrder 查询单个 Credit 订单详情。
func (s *Service) GetCreditOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*creditcontracts.CreditOrderView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if actorUserID == 0 || orderID == 0 {
		return nil, respond.MissingParam
	}
	return s.creditOrderViewByID(ctx, actorUserID, orderID)
}

// MockPaidCreditOrder 在同步事务里完成 mock paid 和 Credit 入账。
func (s *Service) MockPaidCreditOrder(ctx context.Context, req creditcontracts.MockPaidCreditOrderRequest) (*creditcontracts.CreditOrderView, error) {
	if err := s.Ready(); err != nil {
		return nil, err
	}
	if req.ActorUserID == 0 || req.OrderID == 0 {
		return nil, respond.MissingParam
	}

	var resultOrder storemodel.CreditOrder
	err := s.creditDAO.Transaction(ctx, func(txDAO *tokenstoredao.CreditStoreDAO) error {
		now := time.Now()

		order, err := txDAO.LockOrderByID(ctx, req.OrderID)
		if err != nil {
			return normalizeCreditRecordNotFound(err, tokenStoreBadRequest("Credit 订单不存在"))
		}
		if order.UserID != req.ActorUserID {
			return tokenStoreBadRequest("Credit 订单不属于当前用户")
		}

		switch order.Status {
		case storemodel.CreditOrderStatusPending, storemodel.CreditOrderStatusPaid, storemodel.CreditOrderStatusCredited:
		case storemodel.CreditOrderStatusClosed:
			return tokenStoreBadRequest("Credit 订单已关闭，不能执行 mock paid")
		default:
			return tokenStoreBadRequest("Credit 订单状态不支持执行 mock paid")
		}

		eventID := creditOrderLedgerEventID(order.ID)
		paymentMode := paymentModeOrDefault(order.PaymentMode, req.MockChannel)
		ledger, _, err := s.applyCreditLedgerWithDAO(ctx, txDAO, applyCreditLedgerRequest{
			EventID:      eventID,
			UserID:       order.UserID,
			Source:       storemodel.CreditLedgerSourcePurchase,
			SourceLabel:  creditSourceLabel(storemodel.CreditLedgerSourcePurchase, ""),
			Direction:    storemodel.CreditLedgerDirectionIncome,
			OrderID:      &order.ID,
			Amount:       order.CreditAmount,
			Status:       storemodel.CreditLedgerStatusApplied,
			Description:  creditPurchaseDescription(order.ProductName),
			MetadataJSON: creditMetadataJSON(map[string]any{"order_no": order.OrderNo, "payment_mode": paymentMode}),
			CreatedAt:    now,
		})
		if err != nil {
			return err
		}

		paidAt := order.PaidAt
		if paidAt == nil || paidAt.IsZero() {
			paidAt = &now
		}
		creditedAt := order.CreditedAt
		if creditedAt == nil || creditedAt.IsZero() {
			ledgerCreatedAt := ledger.CreatedAt
			if ledgerCreatedAt.IsZero() {
				ledgerCreatedAt = now
			}
			creditedAt = &ledgerCreatedAt
		}

		if err := txDAO.UpdateOrderState(ctx, order.ID, storemodel.CreditOrderStatusCredited, paidAt, creditedAt, paymentMode); err != nil {
			return err
		}

		order.Status = storemodel.CreditOrderStatusCredited
		order.PaidAt = paidAt
		order.CreditedAt = creditedAt
		order.PaymentMode = paymentMode
		resultOrder = *order
		return nil
	})
	if err != nil {
		return nil, err
	}

	s.syncCreditCacheBestEffort(ctx, req.ActorUserID, nil, nil)
	view := creditOrderViewFromModel(resultOrder)
	return &view, nil
}

func (s *Service) creditOrderViewByID(ctx context.Context, actorUserID uint64, orderID uint64) (*creditcontracts.CreditOrderView, error) {
	order, err := s.creditDAO.FindOrderByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, tokenStoreBadRequest("Credit 订单不存在")
	}
	if order.UserID != actorUserID {
		return nil, tokenStoreBadRequest("Credit 订单不属于当前用户")
	}
	view := creditOrderViewFromModel(*order)
	return &view, nil
}

func creditPurchaseDescription(productName string) string {
	trimmed := strings.TrimSpace(productName)
	if trimmed == "" {
		return "购买 Credit 商品"
	}
	return "购买" + trimmed
}

func paymentModeOrDefault(current string, fallback string) string {
	if trimmed := strings.TrimSpace(current); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(fallback); trimmed != "" {
		return trimmed
	}
	return "mock"
}
