package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/schedule/rpc/pb"
	schedulesv "github.com/LoveLosita/smartflow/backend/services/schedule/sv"
	schedulecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/schedule"
)

type Handler struct {
	pb.UnimplementedScheduleServer
	svc *schedulesv.ScheduleService
}

func NewHandler(svc *schedulesv.ScheduleService) *Handler {
	return &Handler{svc: svc}
}

// Ping 供调用方在启动期确认 schedule zrpc 已可用。
func (h *Handler) Ping(ctx context.Context, req *pb.StatusResponse) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) GetToday(ctx context.Context, req *pb.UserRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	data, err := h.svc.GetUserTodaySchedule(ctx, int(req.UserId))
	return jsonResponse(data, err)
}

func (h *Handler) GetWeek(ctx context.Context, req *pb.WeekRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	data, err := h.svc.GetUserWeeklySchedule(ctx, int(req.UserId), int(req.Week))
	return jsonResponse(data, err)
}

func (h *Handler) DeleteEvents(ctx context.Context, req *pb.DeleteEventsRequest) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var events []schedulecontracts.UserDeleteScheduleEvent
	if err := json.Unmarshal(req.EventsJson, &events); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	err := h.svc.DeleteScheduleEventByContract(ctx, schedulecontracts.DeleteScheduleEventsRequest{
		UserID: int(req.UserId),
		Events: events,
	})
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) GetRecentCompleted(ctx context.Context, req *pb.RecentCompletedRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	data, err := h.svc.GetUserRecentCompletedSchedules(ctx, int(req.UserId), int(req.Index), int(req.Limit))
	return jsonResponse(data, err)
}

func (h *Handler) GetCurrent(ctx context.Context, req *pb.UserRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	data, err := h.svc.GetUserOngoingSchedule(ctx, int(req.UserId))
	return jsonResponse(data, err)
}

func (h *Handler) RevokeTaskItem(ctx context.Context, req *pb.RevokeTaskItemRequest) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	if err := h.svc.RevocateUserTaskClassItem(ctx, int(req.UserId), int(req.EventId)); err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) SmartPlanning(ctx context.Context, req *pb.SmartPlanningRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	data, err := h.svc.SmartPlanning(ctx, int(req.UserId), int(req.TaskClassId))
	return jsonResponse(data, err)
}

func (h *Handler) SmartPlanningMulti(ctx context.Context, req *pb.SmartPlanningMultiRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	taskClassIDs := make([]int, 0, len(req.TaskClassIds))
	for _, id := range req.TaskClassIds {
		taskClassIDs = append(taskClassIDs, int(id))
	}
	data, err := h.svc.SmartPlanningMulti(ctx, int(req.UserId), taskClassIDs)
	return jsonResponse(data, err)
}

func (h *Handler) GetScheduleFactsByWindow(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq schedulecontracts.ScheduleWindowRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.GetScheduleFactsByWindow(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) GetFeedbackSignal(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq schedulecontracts.FeedbackRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	feedback, found, err := h.svc.GetFeedbackSignal(ctx, contractReq)
	return jsonResponse(schedulecontracts.FeedbackResponse{Feedback: feedback, Found: found}, err)
}

func (h *Handler) ApplyActiveScheduleChanges(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq schedulecontracts.ApplyActiveScheduleRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	data, err := h.svc.ApplyActiveScheduleChanges(ctx, contractReq)
	return jsonResponse(data, err)
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(errors.New("schedule service dependency not initialized"))
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
