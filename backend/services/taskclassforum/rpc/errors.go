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

const taskClassForumErrorDomain = "smartflow.taskclassforum"

// grpcErrorFromServiceError 负责把计划广场内部错误收口成 gRPC status。
//
// 职责边界：
// 1. 只做 service error -> gRPC error 的传输适配；
// 2. 不负责 HTTP 响应，gateway client 后续会把 gRPC error 反解成 respond.Response；
// 3. 普通内部错误只暴露统一文案，避免把 DAO / SQL 细节透给前端。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}

	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	log.Printf("taskclassforum rpc internal error: %v", err)
	return status.Error(codes.Internal, "taskclassforum service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}

	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: taskClassForumErrorDomain,
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
	case respond.MissingToken.Status, respond.InvalidToken.Status, respond.InvalidClaims.Status, respond.ErrUnauthorized.Status:
		return codes.Unauthenticated
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status, respond.WrongUserID.Status:
		return codes.InvalidArgument
	case respond.UserTaskClassNotFound.Status:
		return codes.NotFound
	case respond.UserTaskClassForbidden.Status, respond.TaskClassItemNotBelongToUser.Status:
		return codes.PermissionDenied
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
