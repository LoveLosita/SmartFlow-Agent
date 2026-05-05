package rpc

import (
	"errors"
	"log"
	"strings"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	tokenstoresv "github.com/LoveLosita/smartflow/backend/services/tokenstore/sv"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const tokenStoreErrorDomain = "smartflow.tokenstore"

// grpcErrorFromServiceError 负责把 token-store 内部错误收口成 gRPC status。
//
// 职责边界：
// 1. 只处理服务内部错误到跨进程错误的转换；
// 2. 不决定 HTTP 状态码，也不直接写前端响应；
// 3. 未识别错误统一按 Internal 处理，避免泄露数据库或支付细节。
func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}

	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}
	if errors.Is(err, tokenstoresv.ErrNotImplemented) {
		return status.Error(codes.Unimplemented, err.Error())
	}
	log.Printf("tokenstore rpc internal error: %v", err)
	return status.Error(codes.Internal, "tokenstore service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}

	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: tokenStoreErrorDomain,
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
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
