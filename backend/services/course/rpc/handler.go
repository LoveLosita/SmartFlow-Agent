package rpc

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/LoveLosita/smartflow/backend/services/course/rpc/pb"
	coursesv "github.com/LoveLosita/smartflow/backend/services/course/sv"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	coursecontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/course"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

type Handler struct {
	pb.UnimplementedCourseServer
	svc *coursesv.CourseService
}

func NewHandler(svc *coursesv.CourseService) *Handler {
	return &Handler{svc: svc}
}

// Ping 供调用方在启动期确认 course zrpc 已可用。
func (h *Handler) Ping(ctx context.Context, req *pb.StatusResponse) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	return &pb.StatusResponse{}, nil
}

func (h *Handler) ValidateCourse(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq coursecontracts.UserCheckCourseRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	if !coursesv.CheckSingleCourse(toModelCheckCourseRequest(contractReq)) {
		return nil, grpcErrorFromServiceError(respond.WrongCourseInfo)
	}
	return jsonResponse(nil, nil)
}

func (h *Handler) ImportCourses(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	var contractReq coursecontracts.UserImportCoursesRequest
	if err := json.Unmarshal(req.PayloadJson, &contractReq); err != nil {
		return nil, grpcErrorFromServiceError(respond.WrongParamType)
	}
	conflicts, err := h.svc.AddUserCourses(ctx, toModelImportCoursesRequest(contractReq), contractReq.UserID)
	if errors.Is(err, respond.ScheduleConflict) {
		rawConflicts, marshalErr := json.Marshal(conflicts)
		if marshalErr != nil {
			return nil, grpcErrorFromServiceError(marshalErr)
		}
		return jsonResponse(coursecontracts.ImportCoursesResult{
			Conflict:  true,
			Conflicts: rawConflicts,
		}, nil)
	}
	return jsonResponse(coursecontracts.ImportCoursesResult{Conflict: false}, err)
}

func (h *Handler) ParseCourseImage(ctx context.Context, req *pb.CourseImageRequest) (*pb.JSONResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	draft, err := h.svc.ParseCourseTableImage(ctx, model.CourseImageParseRequest{
		UserID:     int(req.UserId),
		Filename:   req.Filename,
		MIMEType:   req.MimeType,
		ImageBytes: req.ImageBytes,
	})
	return jsonResponse(draft, err)
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(errors.New("course service dependency not initialized"))
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

func toModelImportCoursesRequest(req coursecontracts.UserImportCoursesRequest) model.UserImportCoursesRequest {
	courses := make([]model.UserCheckCourseRequest, 0, len(req.Courses))
	for _, course := range req.Courses {
		courses = append(courses, toModelCheckCourseRequest(course))
	}
	return model.UserImportCoursesRequest{Courses: courses}
}

func toModelCheckCourseRequest(req coursecontracts.UserCheckCourseRequest) model.UserCheckCourseRequest {
	arrangements := make([]struct {
		StartWeek    int    `json:"start_week"`
		EndWeek      int    `json:"end_week"`
		DayOfWeek    int    `json:"day_of_week"`
		StartSection int    `json:"start_section"`
		EndSection   int    `json:"end_section"`
		WeekType     string `json:"week_type"`
	}, 0, len(req.Arrangements))
	for _, arrangement := range req.Arrangements {
		arrangements = append(arrangements, struct {
			StartWeek    int    `json:"start_week"`
			EndWeek      int    `json:"end_week"`
			DayOfWeek    int    `json:"day_of_week"`
			StartSection int    `json:"start_section"`
			EndSection   int    `json:"end_section"`
			WeekType     string `json:"week_type"`
		}{
			StartWeek:    arrangement.StartWeek,
			EndWeek:      arrangement.EndWeek,
			DayOfWeek:    arrangement.DayOfWeek,
			StartSection: arrangement.StartSection,
			EndSection:   arrangement.EndSection,
			WeekType:     arrangement.WeekType,
		})
	}
	return model.UserCheckCourseRequest{
		CourseName:   req.CourseName,
		Location:     req.Location,
		IsAllowTasks: req.IsAllowTasks,
		Arrangements: arrangements,
	}
}
