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

var (
	errMemoryServiceNotReady = errors.New("memory service dependency not initialized")
)

const memoryErrorDomain = "smartflow.memory"

// grpcErrorFromServiceError 负责把 memory 内部错误转换为 gRPC status。
//
// 职责边界：
// 1. respond.Response 保留项目内部 status/info，供 gateway 反解；
// 2. 未分类错误只暴露通用内部错误，详细信息留在服务日志；
// 3. 不在 RPC 层重判业务规则，业务语义仍由 memory.Module 决定。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}
	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	log.Printf("memory rpc internal error: %v", err)
	return status.Error(codes.Internal, "memory service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}
	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: memoryErrorDomain,
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
	case respond.MemoryItemNotFound.Status:
		return codes.NotFound
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status,
		respond.WrongUserID.Status, respond.MemoryInvalidType.Status, respond.MemoryInvalidContent.Status:
		return codes.InvalidArgument
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
