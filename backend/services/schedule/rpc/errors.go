package rpc

import (
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/services/schedule/core/applyadapter"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	scheduleErrorDomain      = "smartflow.schedule"
	scheduleApplyErrorDomain = "smartflow.schedule.apply"
)

// grpcErrorFromServiceError 负责把 schedule 内部错误转换为 gRPC status。
//
// 职责边界：
// 1. apply 业务错误保留错误码，供 active-scheduler 反解后继续按原 confirm 语义处理；
// 2. respond.Response 继续传输项目内部 status/info；
// 3. 未分类错误只暴露通用内部错误，详细信息留在服务日志。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	var applyErr *applyadapter.ApplyError
	if errors.As(err, &applyErr) {
		return grpcErrorFromApplyError(applyErr)
	}
	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	log.Printf("schedule rpc internal error: %v", err)
	return status.Error(codes.Internal, "schedule service internal error")
}

func grpcErrorFromApplyError(applyErr *applyadapter.ApplyError) error {
	if applyErr == nil {
		return status.Error(codes.Internal, "schedule apply error")
	}
	message := strings.TrimSpace(applyErr.Message)
	if message == "" {
		message = strings.TrimSpace(applyErr.Code)
	}
	st := status.New(grpcCodeFromApplyErrorCode(applyErr.Code), message)
	detail := &errdetails.ErrorInfo{
		Domain: scheduleApplyErrorDomain,
		Reason: applyErr.Code,
		Metadata: map[string]string{
			"info": message,
		},
	}
	withDetails, err := st.WithDetails(detail)
	if err != nil {
		return st.Err()
	}
	return withDetails.Err()
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}
	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: scheduleErrorDomain,
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

func grpcCodeFromApplyErrorCode(code string) codes.Code {
	switch strings.TrimSpace(code) {
	case applyadapter.ErrorCodeTargetNotFound:
		return codes.NotFound
	case applyadapter.ErrorCodeDBError:
		return codes.Internal
	case applyadapter.ErrorCodeTargetCompleted,
		applyadapter.ErrorCodeTargetAlreadyScheduled,
		applyadapter.ErrorCodeSlotConflict:
		return codes.FailedPrecondition
	default:
		return codes.InvalidArgument
	}
}

func grpcCodeFromRespondStatus(statusValue string) codes.Code {
	switch strings.TrimSpace(statusValue) {
	case respond.MissingToken.Status, respond.InvalidToken.Status, respond.InvalidClaims.Status,
		respond.ErrUnauthorized.Status, respond.WrongTokenType.Status, respond.UserLoggedOut.Status:
		return codes.Unauthenticated
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status:
		return codes.InvalidArgument
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
