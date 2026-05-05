package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/task_class/rpc/pb"
	taskclasssv "github.com/LoveLosita/smartflow/backend/services/task_class/sv"
	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
)

const (
	taskClassCreate = 0
	taskClassUpdate = 1
)

type Handler struct {
	pb.UnimplementedTaskClassServer
	svc *taskclasssv.TaskClassService
}

func NewHandler(svc *taskclasssv.TaskClassService) *Handler {
	return &Handler{svc: svc}
}

// Ping 供调用方在启动期确认 task-class zrpc 已可用。
func (h *Handler) Ping(ctx context.Context, req *pb.StatusResponse) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) AddTaskClass(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.UpsertTaskClassRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	taskClassID, err := h.svc.AddOrUpdateTaskClass(ctx, toModelTaskClassRequest(contractReq), contractReq.UserID, taskClassCreate, 0)
	return jsonResponse(taskclasscontracts.UpsertTaskClassResponse{TaskClassID: taskClassID, Created: true}, err)
}

func (h *Handler) ListTaskClasses(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.UserRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.GetUserTaskClassInfos(ctx, contractReq.UserID)
	return jsonResponse(data, err)
}

func (h *Handler) GetTaskClass(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.GetTaskClassRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.GetUserCompleteTaskClass(ctx, contractReq.UserID, contractReq.TaskClassID)
	return jsonResponse(data, err)
}

func (h *Handler) UpdateTaskClass(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.UpsertTaskClassRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	taskClassID, err := h.svc.AddOrUpdateTaskClass(ctx, toModelTaskClassRequest(contractReq), contractReq.UserID, taskClassUpdate, contractReq.TaskClassID)
	return jsonResponse(taskclasscontracts.UpsertTaskClassResponse{TaskClassID: taskClassID, Created: false}, err)
}

func (h *Handler) GetAgentTaskClasses(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.AgentTaskClassesRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.GetAgentTaskClasses(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) InsertTaskClassItemIntoSchedule(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.InsertTaskClassItemIntoScheduleRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	err := h.svc.AddTaskClassItemIntoSchedule(ctx, toModelInsertTaskClassItemRequest(contractReq), contractReq.UserID, contractReq.TaskItemID)
	return jsonResponse(nil, err)
}

func (h *Handler) DeleteTaskClassItem(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.DeleteTaskClassItemRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	err := h.svc.DeleteTaskClassItem(ctx, contractReq.UserID, contractReq.TaskItemID)
	return jsonResponse(nil, err)
}

func (h *Handler) DeleteTaskClass(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.DeleteTaskClassRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	err := h.svc.DeleteTaskClass(ctx, contractReq.UserID, contractReq.TaskClassID)
	return jsonResponse(nil, err)
}

func (h *Handler) ApplyBatchIntoSchedule(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq taskclasscontracts.ApplyBatchIntoScheduleRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	err := h.svc.BatchApplyPlans(ctx, contractReq.TaskClassID, contractReq.UserID, toModelBatchRequest(contractReq))
	return jsonResponse(nil, err)
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(errors.New("task-class service dependency not initialized"))
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

func toModelTaskClassRequest(req taskclasscontracts.UpsertTaskClassRequest) *model.UserAddTaskClassRequest {
	items := make([]model.UserAddTaskClassItemRequest, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, model.UserAddTaskClassItemRequest{
			ID:           item.ID,
			Order:        item.Order,
			Content:      item.Content,
			EmbeddedTime: toModelTargetTime(item.EmbeddedTime),
		})
	}
	return &model.UserAddTaskClassRequest{
		Name:               req.Name,
		StartDate:          req.StartDate,
		EndDate:            req.EndDate,
		Mode:               req.Mode,
		SubjectType:        req.SubjectType,
		DifficultyLevel:    req.DifficultyLevel,
		CognitiveIntensity: req.CognitiveIntensity,
		Config: model.UserAddTaskClassConfig{
			TotalSlots:         req.Config.TotalSlots,
			AllowFillerCourse:  req.Config.AllowFillerCourse,
			Strategy:           req.Config.Strategy,
			ExcludedSlots:      append([]int(nil), req.Config.ExcludedSlots...),
			ExcludedDaysOfWeek: append([]int(nil), req.Config.ExcludedDaysOfWeek...),
		},
		Items: items,
	}
}

func toModelTargetTime(value *taskclasscontracts.TargetTime) *model.TargetTime {
	if value == nil {
		return nil
	}
	return &model.TargetTime{
		Week:        value.Week,
		DayOfWeek:   value.DayOfWeek,
		SectionFrom: value.SectionFrom,
		SectionTo:   value.SectionTo,
	}
}

func toModelInsertTaskClassItemRequest(req taskclasscontracts.InsertTaskClassItemIntoScheduleRequest) *model.UserInsertTaskClassItemToScheduleRequest {
	return &model.UserInsertTaskClassItemToScheduleRequest{
		Week:               req.Week,
		DayOfWeek:          req.DayOfWeek,
		StartSection:       req.StartSection,
		EndSection:         req.EndSection,
		EmbedCourseEventID: req.EmbedCourseEventID,
	}
}

func toModelBatchRequest(req taskclasscontracts.ApplyBatchIntoScheduleRequest) *model.UserInsertTaskClassItemToScheduleRequestBatch {
	items := make([]model.SingleTaskClassItem, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, model.SingleTaskClassItem{
			TaskItemID:         item.TaskItemID,
			Week:               item.Week,
			DayOfWeek:          item.DayOfWeek,
			StartSection:       item.StartSection,
			EndSection:         item.EndSection,
			EmbedCourseEventID: item.EmbedCourseEventID,
		})
	}
	return &model.UserInsertTaskClassItemToScheduleRequestBatch{
		TaskClassID: req.TaskClassID,
		Items:       items,
	}
}
