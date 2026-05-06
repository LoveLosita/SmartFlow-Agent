package rpc

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/userauth/rpc/pb"
	userauthsv "github.com/LoveLosita/smartflow/backend/services/userauth/sv"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/userauth"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Handler struct {
	pb.UnimplementedUserAuthServer
	svc *userauthsv.Service
}

func NewHandler(svc *userauthsv.Service) *Handler {
	return &Handler{svc: svc}
}

// Register 负责把 user/auth 的注册请求从 gRPC 协议转成内部服务调用。
//
// 职责边界：
// 1. 只做 transport -> service 的参数搬运，不碰 DAO/Redis/JWT 细节；
// 2. 业务错误统一转成 gRPC status，让 client 侧继续使用 `res, err :=`；
// 3. 成功时只回传业务数据，不再在 payload 里塞 status/info。
func (h *Handler) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("userauth service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	resp, err := h.svc.Register(ctx, contracts.RegisterRequest{
		Username:    req.Username,
		Password:    req.Password,
		PhoneNumber: req.PhoneNumber,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.RegisterResponse{Id: uint64(resp.ID)}, nil
}

func (h *Handler) Login(ctx context.Context, req *pb.LoginRequest) (*pb.TokensResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("userauth service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	resp, err := h.svc.Login(ctx, contracts.LoginRequest{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.TokensResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
	}, nil
}

func (h *Handler) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.TokensResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("userauth service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	resp, err := h.svc.RefreshToken(ctx, contracts.RefreshTokenRequest{
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.TokensResponse{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
	}, nil
}

func (h *Handler) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.StatusResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("userauth service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingToken)
	}

	if err := h.svc.LogoutByAccessToken(ctx, req.AccessToken); err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) ValidateAccessToken(ctx context.Context, req *pb.ValidateAccessTokenRequest) (*pb.ValidateAccessTokenResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("userauth service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingToken)
	}

	resp, err := h.svc.ValidateAccessToken(ctx, contracts.ValidateAccessTokenRequest{
		AccessToken: req.AccessToken,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.ValidateAccessTokenResponse{
		Valid:             resp.Valid,
		UserId:            int64(resp.UserID),
		TokenType:         resp.TokenType,
		Jti:               resp.JTI,
		ExpiresAtUnixNano: timeToUnixNano(resp.ExpiresAt),
	}, nil
}

func (h *Handler) CheckTokenQuota(ctx context.Context, req *pb.CheckTokenQuotaRequest) (*pb.CheckTokenQuotaResponse, error) {
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "legacy token quota API has been removed")
}

func (h *Handler) AdjustTokenUsage(ctx context.Context, req *pb.AdjustTokenUsageRequest) (*pb.CheckTokenQuotaResponse, error) {
	_ = ctx
	_ = req
	return nil, status.Error(codes.Unimplemented, "legacy token usage adjust API has been removed")
}

func timeToUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}
