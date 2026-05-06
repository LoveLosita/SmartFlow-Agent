package tokenstore

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
)

func (c *Client) GetCreditBalanceSnapshot(ctx context.Context, userID uint64) (*creditcontracts.CreditBalanceSnapshot, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetCreditBalanceSnapshot(ctx, &pb.GetCreditBalanceSnapshotRequest{UserId: userID})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty get credit balance snapshot response")
	}
	snapshot := creditBalanceSnapshotFromPB(resp.Snapshot)
	return &snapshot, nil
}

func (c *Client) GetCreditConsumptionDashboard(ctx context.Context, req creditcontracts.GetCreditConsumptionDashboardRequest) (*creditcontracts.CreditConsumptionDashboardView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetCreditConsumptionDashboard(ctx, &pb.GetCreditConsumptionDashboardRequest{
		ActorUserId: req.ActorUserID,
		Period:      req.Period,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty get credit consumption dashboard response")
	}
	dashboard := creditConsumptionDashboardFromPB(resp.Dashboard)
	return &dashboard, nil
}

func (c *Client) ListCreditProducts(ctx context.Context, actorUserID uint64) ([]creditcontracts.CreditProductView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ListCreditProducts(ctx, &pb.ListCreditProductsRequest{ActorUserId: actorUserID})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty list credit products response")
	}
	return creditProductsFromPB(resp.Items), nil
}

func (c *Client) CreateCreditOrder(ctx context.Context, req creditcontracts.CreateCreditOrderRequest) (*creditcontracts.CreditOrderView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.CreateCreditOrder(ctx, &pb.CreateCreditOrderRequest{
		ActorUserId:    req.ActorUserID,
		ProductId:      req.ProductID,
		Quantity:       int32(req.Quantity),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty create credit order response")
	}
	order := creditOrderFromPB(resp.Order)
	return &order, nil
}

func (c *Client) ListCreditOrders(ctx context.Context, req creditcontracts.ListCreditOrdersRequest) ([]creditcontracts.CreditOrderView, creditcontracts.PageResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	resp, err := c.rpc.ListCreditOrders(ctx, &pb.ListCreditOrdersRequest{
		ActorUserId: req.ActorUserID,
		Page:        int32(req.Page),
		PageSize:    int32(req.PageSize),
		Status:      req.Status,
	})
	if err != nil {
		return nil, creditcontracts.PageResult{}, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, creditcontracts.PageResult{}, errors.New("tokenstore zrpc service returned empty list credit orders response")
	}
	return creditOrdersFromPB(resp.Items), creditPageFromPB(resp.Page), nil
}

func (c *Client) GetCreditOrder(ctx context.Context, actorUserID uint64, orderID uint64) (*creditcontracts.CreditOrderView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetCreditOrder(ctx, &pb.GetCreditOrderRequest{
		ActorUserId: actorUserID,
		OrderId:     orderID,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty get credit order response")
	}
	order := creditOrderFromPB(resp.Order)
	return &order, nil
}

func (c *Client) MockPaidCreditOrder(ctx context.Context, req creditcontracts.MockPaidCreditOrderRequest) (*creditcontracts.CreditOrderView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.MockPaidCreditOrder(ctx, &pb.MockPaidCreditOrderRequest{
		ActorUserId:    req.ActorUserID,
		OrderId:        req.OrderID,
		MockChannel:    req.MockChannel,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty mock paid credit order response")
	}
	order := creditOrderFromPB(resp.Order)
	return &order, nil
}

func (c *Client) ListCreditTransactions(ctx context.Context, req creditcontracts.ListCreditTransactionsRequest) ([]creditcontracts.CreditTransactionView, creditcontracts.PageResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, creditcontracts.PageResult{}, err
	}
	resp, err := c.rpc.ListCreditTransactions(ctx, &pb.ListCreditTransactionsRequest{
		ActorUserId: req.ActorUserID,
		Page:        int32(req.Page),
		PageSize:    int32(req.PageSize),
		Source:      req.Source,
		Direction:   req.Direction,
	})
	if err != nil {
		return nil, creditcontracts.PageResult{}, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, creditcontracts.PageResult{}, errors.New("tokenstore zrpc service returned empty list credit transactions response")
	}
	return creditTransactionsFromPB(resp.Items), creditPageFromPB(resp.Page), nil
}

func (c *Client) ListCreditPriceRules(ctx context.Context, req creditcontracts.ListCreditPriceRulesRequest) ([]creditcontracts.CreditPriceRuleView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ListCreditPriceRules(ctx, &pb.ListCreditPriceRulesRequest{
		Scene:        req.Scene,
		ProviderName: req.ProviderName,
		ModelName:    req.ModelName,
		Status:       req.Status,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty list credit price rules response")
	}
	return creditPriceRulesFromPB(resp.Items), nil
}

func (c *Client) ListCreditRewardRules(ctx context.Context, req creditcontracts.ListCreditRewardRulesRequest) ([]creditcontracts.CreditRewardRuleView, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ListCreditRewardRules(ctx, &pb.ListCreditRewardRulesRequest{
		Source: req.Source,
		Status: req.Status,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	if resp == nil {
		return nil, errors.New("tokenstore zrpc service returned empty list credit reward rules response")
	}
	return creditRewardRulesFromPB(resp.Items), nil
}

func creditBalanceSnapshotFromPB(snapshot *pb.CreditBalanceSnapshotView) creditcontracts.CreditBalanceSnapshot {
	if snapshot == nil {
		return creditcontracts.CreditBalanceSnapshot{}
	}
	return creditcontracts.CreditBalanceSnapshot{
		UserID:         snapshot.UserId,
		Balance:        snapshot.Balance,
		TotalRecharged: snapshot.TotalRecharged,
		TotalRewarded:  snapshot.TotalRewarded,
		TotalConsumed:  snapshot.TotalConsumed,
		IsBlocked:      snapshot.IsBlocked,
		SnapshotSource: snapshot.SnapshotSource,
		UpdatedAt:      snapshot.UpdatedAt,
	}
}

func creditConsumptionDashboardFromPB(view *pb.CreditConsumptionDashboardView) creditcontracts.CreditConsumptionDashboardView {
	if view == nil {
		return creditcontracts.CreditConsumptionDashboardView{}
	}
	return creditcontracts.CreditConsumptionDashboardView{
		Period:         view.Period,
		CreditConsumed: view.CreditConsumed,
		TokenConsumed:  view.TokenConsumed,
	}
}

func creditProductFromPB(product *pb.CreditProductView) creditcontracts.CreditProductView {
	if product == nil {
		return creditcontracts.CreditProductView{}
	}
	return creditcontracts.CreditProductView{
		ProductID:         product.ProductId,
		Name:              product.Name,
		Description:       product.Description,
		CreditAmount:      product.CreditAmount,
		PriceCent:         product.PriceCent,
		OriginalPriceCent: product.OriginalPriceCent,
		PriceText:         product.PriceText,
		Currency:          product.Currency,
		Badge:             product.Badge,
		Status:            product.Status,
		SortOrder:         int(product.SortOrder),
	}
}

func creditProductsFromPB(items []*pb.CreditProductView) []creditcontracts.CreditProductView {
	if len(items) == 0 {
		return []creditcontracts.CreditProductView{}
	}
	result := make([]creditcontracts.CreditProductView, 0, len(items))
	for _, item := range items {
		result = append(result, creditProductFromPB(item))
	}
	return result
}

func creditOrderFromPB(order *pb.CreditOrderView) creditcontracts.CreditOrderView {
	if order == nil {
		return creditcontracts.CreditOrderView{}
	}
	return creditcontracts.CreditOrderView{
		OrderID:         order.OrderId,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		ProductSnapshot: order.ProductSnapshot,
		ProductName:     order.ProductName,
		Quantity:        int(order.Quantity),
		CreditAmount:    order.CreditAmount,
		AmountCent:      order.AmountCent,
		PriceText:       order.PriceText,
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		CreatedAt:       order.CreatedAt,
		PaidAt:          stringPtrFromNonEmpty(order.PaidAt),
		CreditedAt:      stringPtrFromNonEmpty(order.CreditedAt),
	}
}

func creditOrdersFromPB(items []*pb.CreditOrderView) []creditcontracts.CreditOrderView {
	if len(items) == 0 {
		return []creditcontracts.CreditOrderView{}
	}
	result := make([]creditcontracts.CreditOrderView, 0, len(items))
	for _, item := range items {
		result = append(result, creditOrderFromPB(item))
	}
	return result
}

func creditTransactionFromPB(item *pb.CreditTransactionView) creditcontracts.CreditTransactionView {
	if item == nil {
		return creditcontracts.CreditTransactionView{}
	}
	var orderID *uint64
	if item.OrderId > 0 {
		value := item.OrderId
		orderID = &value
	}
	return creditcontracts.CreditTransactionView{
		TransactionID: item.TransactionId,
		EventID:       item.EventId,
		Source:        item.Source,
		SourceLabel:   item.SourceLabel,
		Direction:     item.Direction,
		Amount:        item.Amount,
		BalanceAfter:  item.BalanceAfter,
		Status:        item.Status,
		Description:   item.Description,
		MetadataJSON:  item.MetadataJson,
		CreatedAt:     item.CreatedAt,
		OrderID:       orderID,
	}
}

func creditTransactionsFromPB(items []*pb.CreditTransactionView) []creditcontracts.CreditTransactionView {
	if len(items) == 0 {
		return []creditcontracts.CreditTransactionView{}
	}
	result := make([]creditcontracts.CreditTransactionView, 0, len(items))
	for _, item := range items {
		result = append(result, creditTransactionFromPB(item))
	}
	return result
}

func creditPriceRuleFromPB(item *pb.CreditPriceRuleView) creditcontracts.CreditPriceRuleView {
	if item == nil {
		return creditcontracts.CreditPriceRuleView{}
	}
	return creditcontracts.CreditPriceRuleView{
		RuleID:               item.RuleId,
		Scene:                item.Scene,
		ProviderName:         item.ProviderName,
		ModelName:            item.ModelName,
		InputPriceMicros:     item.InputPriceMicros,
		OutputPriceMicros:    item.OutputPriceMicros,
		CachedPriceMicros:    item.CachedPriceMicros,
		ReasoningPriceMicros: item.ReasoningPriceMicros,
		CreditPerYuan:        item.CreditPerYuan,
		Status:               item.Status,
		Priority:             int(item.Priority),
		Description:          item.Description,
	}
}

func creditPriceRulesFromPB(items []*pb.CreditPriceRuleView) []creditcontracts.CreditPriceRuleView {
	if len(items) == 0 {
		return []creditcontracts.CreditPriceRuleView{}
	}
	result := make([]creditcontracts.CreditPriceRuleView, 0, len(items))
	for _, item := range items {
		result = append(result, creditPriceRuleFromPB(item))
	}
	return result
}

func creditRewardRuleFromPB(item *pb.CreditRewardRuleView) creditcontracts.CreditRewardRuleView {
	if item == nil {
		return creditcontracts.CreditRewardRuleView{}
	}
	return creditcontracts.CreditRewardRuleView{
		RuleID:      item.RuleId,
		Source:      item.Source,
		Name:        item.Name,
		Amount:      item.Amount,
		Status:      item.Status,
		Description: item.Description,
	}
}

func creditRewardRulesFromPB(items []*pb.CreditRewardRuleView) []creditcontracts.CreditRewardRuleView {
	if len(items) == 0 {
		return []creditcontracts.CreditRewardRuleView{}
	}
	result := make([]creditcontracts.CreditRewardRuleView, 0, len(items))
	for _, item := range items {
		result = append(result, creditRewardRuleFromPB(item))
	}
	return result
}

func creditPageFromPB(page *pb.PageResponse) creditcontracts.PageResult {
	if page == nil {
		return creditcontracts.PageResult{}
	}
	return creditcontracts.PageResult{
		Page:     int(page.Page),
		PageSize: int(page.PageSize),
		Total:    int(page.Total),
		HasMore:  page.HasMore,
	}
}
