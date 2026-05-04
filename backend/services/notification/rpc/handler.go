package rpc

import (
	"context"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/notification/rpc/pb"
	notificationsv "github.com/LoveLosita/smartflow/backend/services/notification/sv"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/notification"
)

type Handler struct {
	pb.UnimplementedNotificationServer
	svc *notificationsv.Service
}

func NewHandler(svc *notificationsv.Service) *Handler {
	return &Handler{svc: svc}
}

// GetFeishuWebhook 负责把配置查询请求从 gRPC 协议转成内部服务调用。
//
// 职责边界：
// 1. 只做 transport -> service 的参数搬运，不碰 DAO/provider/outbox 细节；
// 2. 业务错误统一转成 gRPC status，让 client 侧继续使用 `res, err :=`；
// 3. 成功时只回传业务数据，不在 payload 里塞 status/info。
func (h *Handler) GetFeishuWebhook(ctx context.Context, req *pb.GetFeishuWebhookRequest) (*pb.ChannelResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("notification service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	resp, err := h.svc.GetFeishuWebhook(ctx, int(req.UserId))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return channelToPB(resp), nil
}

func (h *Handler) SaveFeishuWebhook(ctx context.Context, req *pb.SaveFeishuWebhookRequest) (*pb.ChannelResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("notification service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	resp, err := h.svc.SaveFeishuWebhook(ctx, int(req.UserId), contracts.SaveFeishuWebhookRequest{
		UserID:      int(req.UserId),
		Enabled:     req.Enabled,
		WebhookURL:  req.WebhookUrl,
		AuthType:    req.AuthType,
		BearerToken: req.BearerToken,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return channelToPB(resp), nil
}

func (h *Handler) DeleteFeishuWebhook(ctx context.Context, req *pb.DeleteFeishuWebhookRequest) (*pb.StatusResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("notification service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	if err := h.svc.DeleteFeishuWebhook(ctx, int(req.UserId)); err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) TestFeishuWebhook(ctx context.Context, req *pb.TestFeishuWebhookRequest) (*pb.TestResult, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("notification service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}

	resp, err := h.svc.TestFeishuWebhook(ctx, int(req.UserId))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return testResultToPB(resp), nil
}

func channelToPB(resp contracts.ChannelResponse) *pb.ChannelResponse {
	return &pb.ChannelResponse{
		Channel:            resp.Channel,
		Enabled:            resp.Enabled,
		Configured:         resp.Configured,
		WebhookUrlMask:     resp.WebhookURLMask,
		AuthType:           resp.AuthType,
		HasBearerToken:     resp.HasBearerToken,
		LastTestStatus:     resp.LastTestStatus,
		LastTestError:      resp.LastTestError,
		LastTestAtUnixNano: timePtrToUnixNano(resp.LastTestAt),
	}
}

func testResultToPB(resp contracts.TestResult) *pb.TestResult {
	return &pb.TestResult{
		Channel:        channelToPB(resp.Channel),
		Status:         resp.Status,
		Outcome:        resp.Outcome,
		Message:        resp.Message,
		TraceId:        resp.TraceID,
		SentAtUnixNano: timeToUnixNano(resp.SentAt),
		Skipped:        resp.Skipped,
		Provider:       resp.Provider,
	}
}

func timePtrToUnixNano(value *time.Time) int64 {
	if value == nil || value.IsZero() {
		return 0
	}
	return value.UnixNano()
}

func timeToUnixNano(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UnixNano()
}
