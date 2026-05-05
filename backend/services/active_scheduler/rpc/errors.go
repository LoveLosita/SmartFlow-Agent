package rpc

import (
	"errors"
	"log"
	"strings"

	activeapply "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/apply"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/activescheduler"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	activeSchedulerErrorDomain      = "smartflow.active_scheduler"
	activeSchedulerApplyErrorDomain = "smartflow.active_scheduler.apply"
)

// grpcErrorFromServiceError 负责把 active-scheduler 内部错误收口成 gRPC status。
//
// 职责边界：
// 1. apply 业务错误保留 error_code，供 gateway 恢复 confirm/apply 的 HTTP 语义；
// 2. respond.Response 继续按项目内业务码传输；
// 3. 未分类错误只暴露通用内部错误，详细信息留在服务日志。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	if applyErr, ok := activeapply.AsApplyError(err); ok {
		return grpcErrorFromApplyError(applyErr)
	}

	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}

	log.Printf("active-scheduler rpc internal error: %v", err)
	return status.Error(codes.Internal, "active-scheduler service internal error")
}

func grpcErrorFromApplyError(applyErr *activeapply.ApplyError) error {
	if applyErr == nil {
		return status.Error(codes.Internal, "active-scheduler apply error")
	}
	message := strings.TrimSpace(applyErr.Message)
	if message == "" {
		message = string(applyErr.Code)
	}
	st := status.New(grpcCodeFromApplyErrorCode(applyErr.Code), message)
	detail := &errdetails.ErrorInfo{
		Domain: activeSchedulerApplyErrorDomain,
		Reason: string(applyErr.Code),
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
		Domain: activeSchedulerErrorDomain,
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

func grpcCodeFromApplyErrorCode(code activeapply.ErrorCode) codes.Code {
	switch contracts.ApplyErrorCode(code) {
	case contracts.ApplyErrorCodeForbidden:
		return codes.PermissionDenied
	case contracts.ApplyErrorCodeTargetNotFound:
		return codes.NotFound
	case contracts.ApplyErrorCodeDBError:
		return codes.Internal
	case contracts.ApplyErrorCodeExpired,
		contracts.ApplyErrorCodeIdempotencyConflict,
		contracts.ApplyErrorCodeBaseVersionChanged,
		contracts.ApplyErrorCodeTargetCompleted,
		contracts.ApplyErrorCodeTargetAlreadySchedule,
		contracts.ApplyErrorCodeSlotConflict,
		contracts.ApplyErrorCodeAlreadyApplied:
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
