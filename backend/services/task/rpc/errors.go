package rpc

import (
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const taskErrorDomain = "smartflow.task"

// grpcErrorFromServiceError 负责把 task 内部错误转换为 gRPC status。
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
	log.Printf("task rpc internal error: %v", err)
	return status.Error(codes.Internal, "task service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}
	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: taskErrorDomain,
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
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status,
		respond.WrongTaskID.Status, respond.WrongUserID.Status, respond.InvalidPriority.Status:
		return codes.InvalidArgument
	case respond.UserTasksEmpty.Status:
		return codes.NotFound
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
