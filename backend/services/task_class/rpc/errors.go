package rpc

import (
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const taskClassErrorDomain = "smartflow.taskclass"

// grpcErrorFromServiceError 负责把 task-class 内部错误转换为 gRPC status。
//
// 职责边界：
// 1. respond.Response 保留项目内部 status/info，供 gateway 反解；
// 2. 未分类错误只暴露通用内部错误，详细信息留在服务日志；
// 3. 不在 RPC 层重判业务规则，业务语义仍由 sv/dao 决定。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	log.Printf("task-class rpc internal error: %v", err)
	return status.Error(codes.Internal, "task-class service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}
	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: taskClassErrorDomain,
		Reason: resp.Status,
		Metadata: map[string]string{
			"info": resp.Info,
		},
	}
	withDetails, err := st.WithDetails(detail)
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func grpcCodeFromRespondStatus(statusValue string) codes.Code {
	switch strings.TrimSpace(statusValue) {
	case respond.MissingToken.Status, respond.InvalidToken.Status, respond.InvalidClaims.Status,
		respond.ErrUnauthorized.Status, respond.WrongTokenType.Status, respond.UserLoggedOut.Status:
		return codes.Unauthenticated
	case respond.UserTaskClassForbidden.Status, respond.TaskClassNotBelongToUser.Status,
		respond.TaskClassItemNotBelongToUser.Status, respond.CourseNotBelongToUser.Status:
		return codes.PermissionDenied
	case respond.UserTaskClassNotFound.Status, respond.TaskClassItemNotFound.Status:
		return codes.NotFound
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status,
		respond.WrongUserID.Status, respond.WrongTaskClassID.Status, respond.WrongTaskID.Status,
		respond.InvalidSectionNumber.Status, respond.InvalidWeekOrDayOfWeek.Status,
		respond.InvalidSectionRange.Status, respond.MissingParamForAutoScheduling.Status,
		respond.InvalidDateRange.Status, respond.TaskClassModeNotAuto.Status,
		respond.TimeNotEnoughForAutoScheduling.Status, respond.TaskClassItemNotBelongToTaskClass.Status,
		respond.TaskClassItemTryingToInsertOutOfTimeRange.Status, respond.TaskClassItemAlreadyArranged.Status,
		respond.CourseAlreadyEmbeddedByOtherTaskBlock.Status, respond.ScheduleConflict.Status,
		respond.WrongCourseID.Status, respond.CourseTimeNotMatch.Status, respond.InsertCourseTwice.Status,
		respond.WeekOutOfRange.Status, respond.WrongScheduleEventID.Status,
		respond.TargetScheduleNotHaveEmbeddedTask.Status, respond.TargetTaskNotEmbeddedInAnySchedule.Status,
		respond.TimeOutOfRangeOfThisSemester.Status:
		return codes.InvalidArgument
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
