package rpc

import (
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	coursesv "github.com/LoveLosita/smartflow/backend/services/course/sv"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const courseErrorDomain = "smartflow.course"

// grpcErrorFromServiceError 负责把 course 内部错误转换为 gRPC status。
//
// 职责边界：
// 1. respond.Response 保留项目内部 status/info，供 gateway 反解；
// 2. 图片解析哨兵错误转换为历史 HTTP 兼容错误码；
// 3. 未分类错误只暴露通用内部错误，详细信息留在服务日志。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	switch {
	case errors.Is(err, coursesv.ErrCourseImageParserUnavailable):
		return grpcErrorFromResponse(respond.Response{Status: "50003", Info: "course image parser is not configured"})
	case errors.Is(err, coursesv.ErrCourseImageTooLarge):
		return grpcErrorFromResponse(respond.Response{Status: "40064", Info: "course image too large"})
	case errors.Is(err, coursesv.ErrCourseImageUnsupportedMIME):
		return grpcErrorFromResponse(respond.Response{Status: "40065", Info: "unsupported course image format"})
	case errors.Is(err, coursesv.ErrCourseImageEmpty):
		return grpcErrorFromResponse(respond.Response{Status: "40066", Info: "course image is empty"})
	}
	log.Printf("course rpc internal error: %v", err)
	return status.Error(codes.Internal, "course service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}
	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: courseErrorDomain,
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
	case respond.CourseNotBelongToUser.Status:
		return codes.PermissionDenied
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status,
		respond.WrongUserID.Status, respond.WrongCourseID.Status, respond.WrongCourseInfo.Status,
		respond.InsertCourseTwice.Status, respond.ScheduleConflict.Status, respond.InvalidSectionNumber.Status,
		respond.InvalidWeekOrDayOfWeek.Status, respond.InvalidSectionRange.Status,
		respond.TimeOutOfRangeOfThisSemester.Status:
		return codes.InvalidArgument
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
