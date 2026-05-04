package notification

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// responseFromRPCError 负责把 notification 的 gRPC 错误反解回项目内的 respond.Response。
//
// 职责边界：
// 1. 只在 gateway 边缘层使用，不下沉到服务实现里；
// 2. 业务错误尽量恢复成 respond.Response，方便 API 层继续复用现有 DealWithError；
// 3. 只要拿不到业务语义，就退化成普通 error，让上层按 500 处理。
func responseFromRPCError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return wrapRPCError(err)
	}

	if resp, ok := responseFromStatus(st); ok {
		return resp
	}

	switch st.Code() {
	case codes.Internal, codes.Unknown, codes.Unavailable, codes.DeadlineExceeded, codes.DataLoss, codes.Unimplemented:
		msg := strings.TrimSpace(st.Message())
		if msg == "" {
			msg = "notification zrpc service internal error"
		}
		return wrapRPCError(errors.New(msg))
	}

	msg := strings.TrimSpace(st.Message())
	if msg == "" {
		msg = "notification zrpc service rejected request"
	}
	return respond.Response{
		Status: grpcCodeToRespondStatus(st.Code()),
		Info:   msg,
	}
}

func responseFromStatus(st *status.Status) (respond.Response, bool) {
	if st == nil {
		return respond.Response{}, false
	}

	if resp, ok := responseFromStatusDetails(st); ok {
		return resp, true
	}
	if resp, ok := responseFromLegacyStatus(st.Code(), st.Message()); ok {
		return resp, true
	}
	return respond.Response{}, false
}

func responseFromStatusDetails(st *status.Status) (respond.Response, bool) {
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}

		statusValue := strings.TrimSpace(info.Reason)
		if statusValue == "" {
			statusValue = grpcCodeToRespondStatus(st.Code())
		}
		if statusValue == "" {
			return respond.Response{}, false
		}

		message := strings.TrimSpace(st.Message())
		if message == "" && info.Metadata != nil {
			message = strings.TrimSpace(info.Metadata["info"])
		}
		if message == "" {
			message = statusValue
		}
		return respond.Response{Status: statusValue, Info: message}, true
	}
	return respond.Response{}, false
}

func responseFromLegacyStatus(code codes.Code, message string) (respond.Response, bool) {
	trimmed := strings.TrimSpace(message)
	if resp, ok := respondResponseByMessage(trimmed); ok {
		return resp, true
	}

	switch code {
	case codes.Unauthenticated:
		if trimmed == "" {
			trimmed = "unauthorized"
		}
		return respond.Response{Status: respond.ErrUnauthorized.Status, Info: trimmed}, true
	case codes.InvalidArgument:
		if trimmed == "" {
			trimmed = "invalid argument"
		}
		return respond.Response{Status: respond.MissingParam.Status, Info: trimmed}, true
	case codes.Internal, codes.Unknown, codes.DataLoss:
		if trimmed == "" {
			trimmed = "notification service internal error"
		}
		return respond.InternalError(errors.New(trimmed)), true
	}

	return respond.Response{}, false
}

func respondResponseByMessage(message string) (respond.Response, bool) {
	switch strings.TrimSpace(message) {
	case respond.MissingParam.Info:
		return respond.MissingParam, true
	case respond.WrongParamType.Info:
		return respond.WrongParamType, true
	case respond.ErrUnauthorized.Info:
		return respond.ErrUnauthorized, true
	}
	return respond.Response{}, false
}

func grpcCodeToRespondStatus(code codes.Code) string {
	switch code {
	case codes.Unauthenticated:
		return respond.ErrUnauthorized.Status
	case codes.InvalidArgument:
		return respond.MissingParam.Status
	case codes.Internal, codes.Unknown, codes.DataLoss:
		return "500"
	default:
		return "400"
	}
}

func wrapRPCError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("调用 notification zrpc 服务失败: %w", err)
}
