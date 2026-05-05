package rpc

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	tokenstoresv "github.com/LoveLosita/smartflow/backend/services/tokenstore/sv"
	tokencontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/tokenstore"
)

type Handler struct {
	pb.UnimplementedTokenStoreServiceServer
	svc *tokenstoresv.Service
}

func NewHandler(svc *tokenstoresv.Service) *Handler {
	return &Handler{svc: svc}
}

// service 负责统一校验 RPC 层依赖是否已经注入。
//
// 职责边界：
// 1. 只判断 handler 自身和业务 service 是否可用；
// 2. 不负责支付状态流转、订单幂等和 grant 账本写入；
// 3. 失败时返回可直接转成 gRPC status 的业务错误。
func (h *Handler) service() (*tokenstoresv.Service, error) {
	if h == nil || h.svc == nil {
		return nil, errors.New("tokenstore service dependency not initialized")
	}
	return h.svc, nil
}

// GetSummary 负责把 Token 概览请求从 gRPC 协议转成内部服务调用。
func (h *Handler) GetSummary(ctx context.Context, req *pb.GetTokenSummaryRequest) (*pb.GetTokenSummaryResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	summary, err := svc.GetSummary(ctx, req.ActorUserId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.GetTokenSummaryResponse{Summary: tokenSummaryToPB(summary)}, nil
}

func (h *Handler) ListProducts(ctx context.Context, req *pb.ListTokenProductsRequest) (*pb.ListTokenProductsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, err := svc.ListProducts(ctx, req.ActorUserId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListTokenProductsResponse{Items: tokenProductsToPB(items)}, nil
}

func (h *Handler) CreateOrder(ctx context.Context, req *pb.CreateTokenOrderRequest) (*pb.CreateTokenOrderResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	order, err := svc.CreateOrder(ctx, tokencontracts.CreateTokenOrderRequest{
		ActorUserID:    req.ActorUserId,
		ProductID:      req.ProductId,
		Quantity:       int(req.Quantity),
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.CreateTokenOrderResponse{Order: tokenOrderToPB(order)}, nil
}

func (h *Handler) ListOrders(ctx context.Context, req *pb.ListTokenOrdersRequest) (*pb.ListTokenOrdersResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, page, err := svc.ListOrders(ctx, tokencontracts.ListTokenOrdersRequest{
		ActorUserID: req.ActorUserId,
		Page:        int(req.Page),
		PageSize:    int(req.PageSize),
		Status:      req.Status,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListTokenOrdersResponse{
		Items: tokenOrdersToPB(items),
		Page:  tokenPageToPB(page),
	}, nil
}

func (h *Handler) GetOrder(ctx context.Context, req *pb.GetTokenOrderRequest) (*pb.GetTokenOrderResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	order, err := svc.GetOrder(ctx, req.ActorUserId, req.OrderId)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.GetTokenOrderResponse{Order: tokenOrderToPB(order)}, nil
}

func (h *Handler) MockPaidOrder(ctx context.Context, req *pb.MockPaidOrderRequest) (*pb.MockPaidOrderResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	order, err := svc.MockPaidOrder(ctx, tokencontracts.MockPaidOrderRequest{
		ActorUserID:    req.ActorUserId,
		OrderID:        req.OrderId,
		MockChannel:    req.MockChannel,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.MockPaidOrderResponse{Order: tokenOrderToPB(order)}, nil
}

func (h *Handler) ListGrants(ctx context.Context, req *pb.ListTokenGrantsRequest) (*pb.ListTokenGrantsResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	items, page, err := svc.ListGrants(ctx, tokencontracts.ListTokenGrantsRequest{
		ActorUserID: req.ActorUserId,
		Page:        int(req.Page),
		PageSize:    int(req.PageSize),
		Source:      req.Source,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ListTokenGrantsResponse{
		Items: tokenGrantsToPB(items),
		Page:  tokenPageToPB(page),
	}, nil
}

// RecordForumRewardGrant 负责把论坛 outbox 奖励事件转成 token-store 内部账本写入调用。
func (h *Handler) RecordForumRewardGrant(ctx context.Context, req *pb.RecordForumRewardGrantRequest) (*pb.RecordForumRewardGrantResponse, error) {
	svc, err := h.service()
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	grant, err := svc.RecordForumRewardGrant(ctx, tokencontracts.RecordForumRewardGrantRequest{
		EventID:        req.EventId,
		ReceiverUserID: req.ReceiverUserId,
		Source:         req.Source,
		SourceRefID:    req.SourceRefId,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.RecordForumRewardGrantResponse{Grant: tokenGrantToPB(grant)}, nil
}

func tokenPageToPB(page tokencontracts.PageResult) *pb.PageResponse {
	return &pb.PageResponse{
		Page:     int32(page.Page),
		PageSize: int32(page.PageSize),
		Total:    int32(page.Total),
		HasMore:  page.HasMore,
	}
}

func tokenSummaryToPB(summary *tokencontracts.TokenSummary) *pb.TokenSummary {
	if summary == nil {
		return nil
	}
	return &pb.TokenSummary{
		RecordedTokenTotal:     summary.RecordedTokenTotal,
		AppliedTokenTotal:      summary.AppliedTokenTotal,
		PendingApplyTokenTotal: summary.PendingApplyTokenTotal,
		QuotaSyncStatus:        summary.QuotaSyncStatus,
		Tip:                    summary.Tip,
	}
}

func tokenProductToPB(product tokencontracts.TokenProductView) *pb.TokenProductView {
	return &pb.TokenProductView{
		ProductId:   product.ProductID,
		Name:        product.Name,
		Description: product.Description,
		TokenAmount: product.TokenAmount,
		PriceCent:   product.PriceCent,
		PriceText:   product.PriceText,
		Currency:    product.Currency,
		Badge:       product.Badge,
		Status:      product.Status,
		SortOrder:   int32(product.SortOrder),
	}
}

func tokenProductsToPB(items []tokencontracts.TokenProductView) []*pb.TokenProductView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.TokenProductView, 0, len(items))
	for i := range items {
		result = append(result, tokenProductToPB(items[i]))
	}
	return result
}

func tokenGrantToPB(grant *tokencontracts.TokenGrantView) *pb.TokenGrantView {
	if grant == nil {
		return nil
	}
	return &pb.TokenGrantView{
		GrantId:      grant.GrantID,
		EventId:      grant.EventID,
		Source:       grant.Source,
		SourceLabel:  grant.SourceLabel,
		Amount:       grant.Amount,
		Status:       grant.Status,
		QuotaApplied: grant.QuotaApplied,
		Description:  grant.Description,
		CreatedAt:    grant.CreatedAt,
	}
}

func tokenGrantsToPB(items []tokencontracts.TokenGrantView) []*pb.TokenGrantView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.TokenGrantView, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, tokenGrantToPB(&item))
	}
	return result
}

func tokenOrderToPB(order *tokencontracts.TokenOrderView) *pb.TokenOrderView {
	if order == nil {
		return nil
	}
	return &pb.TokenOrderView{
		OrderId:         order.OrderID,
		OrderNo:         order.OrderNo,
		Status:          order.Status,
		TokenAmount:     order.TokenAmount,
		AmountCent:      order.AmountCent,
		PriceText:       order.PriceText,
		Currency:        order.Currency,
		PaymentMode:     order.PaymentMode,
		Grant:           tokenGrantToPB(order.Grant),
		CreatedAt:       order.CreatedAt,
		PaidAt:          tokenStringFromPtr(order.PaidAt),
		GrantedAt:       tokenStringFromPtr(order.GrantedAt),
		ProductSnapshot: order.ProductSnapshot,
		ProductName:     order.ProductName,
		Quantity:        int32(order.Quantity),
	}
}

func tokenOrdersToPB(items []tokencontracts.TokenOrderView) []*pb.TokenOrderView {
	if len(items) == 0 {
		return nil
	}
	result := make([]*pb.TokenOrderView, 0, len(items))
	for i := range items {
		item := items[i]
		result = append(result, tokenOrderToPB(&item))
	}
	return result
}

func tokenStringFromPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
