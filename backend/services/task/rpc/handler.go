package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/task/rpc/pb"
	tasksv "github.com/LoveLosita/smartflow/backend/services/task/sv"
	taskcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/task"
)

type Handler struct {
	pb.UnimplementedTaskServer
	svc *tasksv.TaskService
}

func NewHandler(svc *tasksv.TaskService) *Handler {
	return &Handler{svc: svc}
}

// Ping 供调用方在启动期确认 task zrpc 已可用。
func (h *Handler) Ping(ctx context.Context, req *pb.StatusResponse) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) AddTask(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.AddTaskRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.AddTask(ctx, &model.UserAddTaskRequest{
		Title:              contractReq.Title,
		PriorityGroup:      contractReq.PriorityGroup,
		EstimatedSections:  contractReq.EstimatedSections,
		DeadlineAt:         contractReq.DeadlineAt,
		UrgencyThresholdAt: contractReq.UrgencyThresholdAt,
	}, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) GetUserTasks(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.UserRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.GetUserTasks(ctx, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) BatchTaskStatus(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.BatchTaskStatusRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.BatchTaskStatus(ctx, &model.BatchTaskStatusRequest{IDs: contractReq.IDs}, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) CompleteTask(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.CompleteTaskRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.CompleteTask(ctx, &model.UserCompleteTaskRequest{TaskID: contractReq.TaskID}, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) UndoCompleteTask(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.UndoCompleteTaskRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.UndoCompleteTask(ctx, &model.UserUndoCompleteTaskRequest{TaskID: contractReq.TaskID}, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) UpdateTask(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.UpdateTaskRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.UpdateTask(ctx, &model.UserUpdateTaskRequest{
		TaskID:             contractReq.TaskID,
		Title:              contractReq.Title,
		PriorityGroup:      contractReq.PriorityGroup,
		DeadlineAt:         contractReq.DeadlineAt,
		UrgencyThresholdAt: contractReq.UrgencyThresholdAt,
	}, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) DeleteTask(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.DeleteTaskRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	taskID, err := h.svc.DeleteTask(ctx, &model.UserCompleteTaskRequest{TaskID: contractReq.TaskID}, contractReq.UserID)
	return jsonResponse(map[string]int{"task_id": taskID}, err)
}

func (h *Handler) GetTaskForActiveSchedule(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskcontracts.TaskFactRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	task, found, err := h.svc.GetTaskForActiveSchedule(ctx, contractReq)
	return jsonResponse(taskcontracts.TaskFactResponse{Task: task, Found: found}, err)
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(errors.New("task service dependency not initialized"))
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
