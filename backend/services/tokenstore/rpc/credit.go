package rpc

import (
	"context"

	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

func (h *Handler) GetCreditBalanceSnapshot(ctx context.Context, req *pb.GetCreditBalanceSnapshotRequest) (*pb.GetCreditBalanceSnapshotResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	snapshot, err := svc.GetCreditBalanceSnapshot(ctx, req.UserId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.GetCreditBalanceSnapshotResponse{Snapshot: creditBalanceSnapshotToPB(snapshot)}, nil
}

func (h *Handler) GetCreditConsumptionDashboard(ctx context.Context, req *pb.GetCreditConsumptionDashboardRequest) (*pb.GetCreditConsumptionDashboardResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	dashboard, err := svc.GetCreditConsumptionDashboard(ctx, creditcontracts.GetCreditConsumptionDashboardRequest{
		ActorUserID: req.ActorUserId,
		Period:      req.Period,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.GetCreditConsumptionDashboardResponse{Dashboard: creditConsumptionDashboardToPB(dashboard)}, nil
}

func (h *Handler) ListCreditProducts(ctx context.Context, req *pb.ListCreditProductsRequest) (*pb.ListCreditProductsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, err := svc.ListCreditProducts(ctx, req.ActorUserId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListCreditProductsResponse{Items: creditProductsToPB(items)}, nil
}

func (h *Handler) CreateCreditOrder(ctx context.Context, req *pb.CreateCreditOrderRequest) (*pb.CreateCreditOrderResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	order, err := svc.CreateCreditOrder(ctx, creditcontracts.CreateCreditOrderRequest{
		ActorUserID:    req.ActorUserId,
		ProductID:      req.ProductId,
		Quantity:       int(req.Quantity),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.CreateCreditOrderResponse{Order: creditOrderToPB(order)}, nil
}

func (h *Handler) ListCreditOrders(ctx context.Context, req *pb.ListCreditOrdersRequest) (*pb.ListCreditOrdersResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, page, err := svc.ListCreditOrders(ctx, creditcontracts.ListCreditOrdersRequest{
		ActorUserID: req.ActorUserId,
		Page:        int(req.Page),
		PageSize:    int(req.PageSize),
		Status:      req.Status,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListCreditOrdersResponse{
		Items: creditOrdersToPB(items),
		Page:  creditPageToPB(page),
	}, nil
}

func (h *Handler) GetCreditOrder(ctx context.Context, req *pb.GetCreditOrderRequest) (*pb.GetCreditOrderResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	order, err := svc.GetCreditOrder(ctx, req.ActorUserId, req.OrderId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.GetCreditOrderResponse{Order: creditOrderToPB(order)}, nil
}

func (h *Handler) MockPaidCreditOrder(ctx context.Context, req *pb.MockPaidCreditOrderRequest) (*pb.MockPaidCreditOrderResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	order, err := svc.MockPaidCreditOrder(ctx, creditcontracts.MockPaidCreditOrderRequest{
		ActorUserID:    req.ActorUserId,
		OrderID:        req.OrderId,
		MockChannel:    req.MockChannel,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.MockPaidCreditOrderResponse{Order: creditOrderToPB(order)}, nil
}

func (h *Handler) ListCreditTransactions(ctx context.Context, req *pb.ListCreditTransactionsRequest) (*pb.ListCreditTransactionsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, page, err := svc.ListCreditTransactions(ctx, creditcontracts.ListCreditTransactionsRequest{
		ActorUserID: req.ActorUserId,
		Page:        int(req.Page),
		PageSize:    int(req.PageSize),
		Source:      req.Source,
		Direction:   req.Direction,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListCreditTransactionsResponse{
		Items: creditTransactionsToPB(items),
		Page:  creditPageToPB(page),
	}, nil
}

func (h *Handler) ListCreditPriceRules(ctx context.Context, req *pb.ListCreditPriceRulesRequest) (*pb.ListCreditPriceRulesResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, err := svc.ListCreditPriceRules(ctx, creditcontracts.ListCreditPriceRulesRequest{
		Scene:        req.Scene,
		ProviderName: req.ProviderName,
		ModelName:    req.ModelName,
		Status:       req.Status,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListCreditPriceRulesResponse{Items: creditPriceRulesToPB(items)}, nil
}

func (h *Handler) ListCreditRewardRules(ctx context.Context, req *pb.ListCreditRewardRulesRequest) (*pb.ListCreditRewardRulesResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, err := svc.ListCreditRewardRules(ctx, creditcontracts.ListCreditRewardRulesRequest{
		Source: req.Source,
		Status: req.Status,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListCreditRewardRulesResponse{Items: creditRewardRulesToPB(items)}, nil
}

func creditPageToPB(page creditcontracts.PageResult) *pb.PageResponse {
	return &pb.PageResponse{
		Page:     int32(page.Page),
		PageSize: int32(page.PageSize),
		Total:    int32(page.Total),
		HasMore:  page.HasMore,
	}
}

func creditBalanceSnapshotToPB(snapshot *creditcontracts.CreditBalanceSnapshot) *pb.CreditBalanceSnapshotView {
	if snapshot == nil {
		return nil
	}
	return &pb.CreditBalanceSnapshotView{
		UserId:         snapshot.UserID,
		Balance:        snapshot.Balance,
		TotalRecharged: snapshot.TotalRecharged,
		TotalRewarded:  snapshot.TotalRewarded,
		TotalConsumed:  snapshot.TotalConsumed,
		IsBlocked:      snapshot.IsBlocked,
		SnapshotSource: snapshot.SnapshotSource,
		UpdatedAt:      snapshot.UpdatedAt,
	}
}

func creditConsumptionDashboardToPB(view *creditcontracts.CreditConsumptionDashboardView) *pb.CreditConsumptionDashboardView {
	if view == nil {
		return nil
	}
	return &pb.CreditConsumptionDashboardView{
		Period:         view.Period,
		CreditConsumed: view.CreditConsumed,
		TokenConsumed:  view.TokenConsumed,
	}
}

func creditProductToPB(product creditcontracts.CreditProductView) *pb.CreditProductView {
	return &pb.CreditProductView{
		ProductId:         product.ProductID,
		Name:              product.Name,
		Description:       product.Description,
		CreditAmount:      product.CreditAmount,
		PriceCent:         product.PriceCent,
		OriginalPriceCent: product.OriginalPriceCent,
		PriceText:         product.PriceText,
		Currency:          product.Currency,
		Badge:             product.Badge,
		Status:            product.Status,
		SortOrder:         int32(product.SortOrder),
	}
}

func creditProductsToPB(items []creditcontracts.CreditProductView) []*pb.CreditProductView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.CreditProductView, 0, len(items))
	for i := range items {
		result = append(result, creditProductToPB(items[i]))
	}
	return result
}

func creditOrderToPB(order *creditcontracts.CreditOrderView) *pb.CreditOrderView {
	if order == nil {
		return nil
	}
	return &pb.CreditOrderView{
		OrderId:         order.OrderID,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		CreditAmount:    order.CreditAmount,
		AmountCent:      order.AmountCent,
		PriceText:       order.PriceText,
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		CreatedAt:       order.CreatedAt,
		PaidAt:          tokenStringFromPtr(order.PaidAt),
		CreditedAt:      tokenStringFromPtr(order.CreditedAt),
		ProductSnapshot: order.ProductSnapshot,
		ProductName:     order.ProductName,
		Quantity:        int32(order.Quantity),
	}
}

func creditOrdersToPB(items []creditcontracts.CreditOrderView) []*pb.CreditOrderView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.CreditOrderView, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, creditOrderToPB(&item))
	}
	return result
}

func creditTransactionToPB(item creditcontracts.CreditTransactionView) *pb.CreditTransactionView {
	result := &pb.CreditTransactionView{
		TransactionId: item.TransactionID,
		EventId:       item.EventID,
		Source:        item.Source,
		SourceLabel:   item.SourceLabel,
		Direction:     item.Direction,
		Amount:        item.Amount,
		BalanceAfter:  item.BalanceAfter,
		Status:        item.Status,
		Description:   item.Description,
		MetadataJson:  item.MetadataJSON,
		CreatedAt:     item.CreatedAt,
	}
	if item.OrderID != nil {
		result.OrderId = *item.OrderID
	}
	return result
}

func creditTransactionsToPB(items []creditcontracts.CreditTransactionView) []*pb.CreditTransactionView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.CreditTransactionView, 0, len(items))
	for i := range items {
		result = append(result, creditTransactionToPB(items[i]))
	}
	return result
}

func creditPriceRuleToPB(rule creditcontracts.CreditPriceRuleView) *pb.CreditPriceRuleView {
	return &pb.CreditPriceRuleView{
		RuleId:                     rule.RuleID,
		Scene:                      rule.Scene,
		ProviderName:               rule.ProviderName,
		ModelName:                  rule.ModelName,
		InputPriceMicros:           rule.InputPriceMicros,
		OutputPriceMicros:          rule.OutputPriceMicros,
		CachedPriceMicros:          rule.CachedPriceMicros,
		ReasoningPriceMicros:       rule.ReasoningPriceMicros,
		CreditPerYuan:              rule.CreditPerYuan,
		ProfitRateBps:              rule.ProfitRateBps,
		ChargeInputPriceMicros:     rule.ChargeInputPriceMicros,
		ChargeOutputPriceMicros:    rule.ChargeOutputPriceMicros,
		ChargeCachedPriceMicros:    rule.ChargeCachedPriceMicros,
		ChargeReasoningPriceMicros: rule.ChargeReasoningPriceMicros,
		Status:                     rule.Status,
		Priority:                   int32(rule.Priority),
		Description:                rule.Description,
	}
}

func creditPriceRulesToPB(items []creditcontracts.CreditPriceRuleView) []*pb.CreditPriceRuleView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.CreditPriceRuleView, 0, len(items))
	for i := range items {
		result = append(result, creditPriceRuleToPB(items[i]))
	}
	return result
}

func creditRewardRuleToPB(rule creditcontracts.CreditRewardRuleView) *pb.CreditRewardRuleView {
	return &pb.CreditRewardRuleView{
		RuleId:      rule.RuleID,
		Source:      rule.Source,
		Name:        rule.Name,
		Amount:      rule.Amount,
		Status:      rule.Status,
		Description: rule.Description,
	}
}

func creditRewardRulesToPB(items []creditcontracts.CreditRewardRuleView) []*pb.CreditRewardRuleView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.CreditRewardRuleView, 0, len(items))
	for i := range items {
		result = append(result, creditRewardRuleToPB(items[i]))
	}
	return result
}

func tokenStringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
