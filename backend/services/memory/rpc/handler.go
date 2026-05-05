package rpc

import (
	"context"
	"encoding/json"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/memory/rpc/pb"
	memorysv "github.com/LoveLosita/smartflow/backend/services/memory/sv"
	memorycontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/memory"
)

type Handler struct {
	pb.UnimplementedMemoryServer
	svc *memorysv.Service
}

func NewHandler(svc *memorysv.Service) *Handler {
	return &Handler{svc: svc}
}

// Ping 供调用方在启动期确认 memory zrpc 已可用。
func (h *Handler) Ping(ctx context.Context, req *pb.StatusResponse) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	if err := h.svc.Ping(ctx); err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) Retrieve(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.RetrieveRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.Retrieve(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) ListItems(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.ListItemsRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.ListItems(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) GetItem(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.GetItemRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.GetItem(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) CreateItem(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.CreateItemRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.CreateItem(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) UpdateItem(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.UpdateItemRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.UpdateItem(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) DeleteItem(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.DeleteItemRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.DeleteItem(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) RestoreItem(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq memorycontracts.RestoreItemRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.RestoreItem(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(errMemoryServiceNotReady)
	}
	if req == nil {
		return grpcErrorFromServiceError(respond.MissingParam)
	}
	return nil
}

func jsonResponse(value any, err error) (*pb.JSONResponse, error) {
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.JSONResponse{DataJson: raw}, nil
}
