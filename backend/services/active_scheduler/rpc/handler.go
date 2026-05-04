package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/active_scheduler/rpc/pb"
	activeschedulersv "github.com/LoveLosita/smartflow/backend/services/active_scheduler/sv"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/activescheduler"
)

type Handler struct {
	pb.UnimplementedActiveSchedulerServer
	svc *activeschedulersv.Service
}

func NewHandler(svc *activeschedulersv.Service) *Handler {
	return &Handler{svc: svc}
}

// DryRun 负责把 gRPC 请求转换为主动调度 dry-run 服务调用。
func (h *Handler) DryRun(ctx context.Context, req *pb.ActiveScheduleRequest) (*pb.JSONResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("active-scheduler service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}
	data, err := h.svc.DryRun(ctx, activeScheduleRequestFromPB(req))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponse(data), nil
}

func (h *Handler) Trigger(ctx context.Context, req *pb.ActiveScheduleRequest) (*pb.TriggerResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("active-scheduler service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}
	resp, err := h.svc.Trigger(ctx, activeScheduleRequestFromPB(req))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return triggerResponseToPB(resp), nil
}

func (h *Handler) CreatePreview(ctx context.Context, req *pb.ActiveScheduleRequest) (*pb.JSONResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("active-scheduler service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}
	data, err := h.svc.CreatePreview(ctx, activeScheduleRequestFromPB(req))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponse(data), nil
}

func (h *Handler) GetPreview(ctx context.Context, req *pb.GetPreviewRequest) (*pb.JSONResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("active-scheduler service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}
	data, err := h.svc.GetPreview(ctx, contracts.GetPreviewRequest{
		UserID:    int(req.UserId),
		PreviewID: req.PreviewId,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponse(data), nil
}

func (h *Handler) ConfirmPreview(ctx context.Context, req *pb.ConfirmPreviewRequest) (*pb.JSONResponse, error) {
	if h == nil || h.svc == nil {
		return nil, grpcErrorFromServiceError(errors.New("active-scheduler service dependency not initialized"))
	}
	if req == nil {
		return nil, grpcErrorFromServiceError(respond.MissingParam)
	}
	data, err := h.svc.ConfirmPreview(ctx, confirmRequestFromPB(req))
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponse(data), nil
}

func activeScheduleRequestFromPB(req *pb.ActiveScheduleRequest) contracts.ActiveScheduleRequest {
	var mockNow *time.Time
	if req.MockNowUnixNano > 0 {
		value := time.Unix(0, req.MockNowUnixNano)
		mockNow = &value
	}
	return contracts.ActiveScheduleRequest{
		UserID:         int(req.UserId),
		TriggerType:    req.TriggerType,
		TargetType:     req.TargetType,
		TargetID:       int(req.TargetId),
		FeedbackID:     req.FeedbackId,
		IdempotencyKey: req.IdempotencyKey,
		MockNow:        mockNow,
		Payload:        json.RawMessage(req.PayloadJson),
	}
}

func confirmRequestFromPB(req *pb.ConfirmPreviewRequest) contracts.ConfirmPreviewRequest {
	requestedAt := time.Time{}
	if req.RequestedAtUnixNano > 0 {
		requestedAt = time.Unix(0, req.RequestedAtUnixNano)
	}
	return contracts.ConfirmPreviewRequest{
		UserID:         int(req.UserId),
		PreviewID:      req.PreviewId,
		CandidateID:    req.CandidateId,
		Action:         req.Action,
		EditedChanges:  json.RawMessage(req.EditedChangesJson),
		IdempotencyKey: req.IdempotencyKey,
		RequestedAt:    requestedAt,
		TraceID:        req.TraceId,
	}
}

func triggerResponseToPB(resp *contracts.TriggerResponse) *pb.TriggerResponse {
	if resp == nil {
		return &pb.TriggerResponse{}
	}
	previewID := ""
	hasPreviewID := false
	if resp.PreviewID != nil {
		previewID = *resp.PreviewID
		hasPreviewID = previewID != ""
	}
	return &pb.TriggerResponse{
		TriggerId:    resp.TriggerID,
		Status:       resp.Status,
		PreviewId:    previewID,
		HasPreviewId: hasPreviewID,
		DedupeHit:    resp.DedupeHit,
		TraceId:      resp.TraceID,
	}
}

func jsonResponse(data json.RawMessage) *pb.JSONResponse {
	return &pb.JSONResponse{DataJson: []byte(data)}
}
