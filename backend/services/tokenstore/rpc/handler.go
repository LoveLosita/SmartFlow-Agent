package rpc

import (
	"context"
	"errors"

	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	tokenstoresv "github.com/LoveLosita/smartflow/backend/services/tokenstore/sv"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	pb.UnimplementedTokenStoreServiceServer
	svc *tokenstoresv.Service
}

func NewHandler(svc *tokenstoresv.Service) *Handler {
	return &Handler{svc: svc}
}

// service 负责统一校验 RPC 层依赖是否已经注入。
func (h *Handler) service() (*tokenstoresv.Service, error) {
	if h == nil || h.svc == nil {
		return nil, errors.New("tokenstore service dependency not initialized")
	}
	return h.svc, nil
}

// GetSummary 保留旧 token RPC 方法壳，统一返回已下线。
func (h *Handler) GetSummary(context.Context, *pb.GetTokenSummaryRequest) (*pb.GetTokenSummaryResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) ListProducts(context.Context, *pb.ListTokenProductsRequest) (*pb.ListTokenProductsResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) CreateOrder(context.Context, *pb.CreateTokenOrderRequest) (*pb.CreateTokenOrderResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) ListOrders(context.Context, *pb.ListTokenOrdersRequest) (*pb.ListTokenOrdersResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) GetOrder(context.Context, *pb.GetTokenOrderRequest) (*pb.GetTokenOrderResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) MockPaidOrder(context.Context, *pb.MockPaidOrderRequest) (*pb.MockPaidOrderResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) ListGrants(context.Context, *pb.ListTokenGrantsRequest) (*pb.ListTokenGrantsResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func (h *Handler) RecordForumRewardGrant(context.Context, *pb.RecordForumRewardGrantRequest) (*pb.RecordForumRewardGrantResponse, error) {
	return nil, legacyTokenMethodRemoved()
}

func legacyTokenMethodRemoved() error {
	return status.Error(codes.Unimplemented, "legacy token API has been removed; use credit APIs instead")
}
