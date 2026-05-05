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

const notificationErrorDomain = "smartflow.notification"

// grpcErrorFromServiceError 负责把 notification 内部错误收口成 gRPC status。
//
// 职责边界：
// 1. 只负责把本服务内部的 respond.Response / 普通 error 转成 gRPC 可传输错误；
// 2. 不负责决定 HTTP 语义，也不负责写回前端响应体；
// 3. 上层 handler 只要直接 return 这个结果，就能让 client 侧按 `res, err :=` 的方式接收。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}

	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	log.Printf("notification rpc internal error: %v", err)
	return status.Error(codes.Internal, "notification service internal error")
}

// grpcErrorFromResponse 负责把项目内业务响应映射成 gRPC status。
//
// 职责边界：
// 1. 只处理 notification 这组响应码到 gRPC code 的映射；
// 2. 业务码和业务文案通过 ErrorInfo 附带，方便 gateway 再反解回 respond.Response；
// 3. 失败时退化为普通 gRPC status，不阻断请求链路。
func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}

	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: notificationErrorDomain,
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
	case respond.MissingParam.Status, respond.WrongParamType.Status, respond.ParamTooLong.Status:
		return codes.InvalidArgument
	}

	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
