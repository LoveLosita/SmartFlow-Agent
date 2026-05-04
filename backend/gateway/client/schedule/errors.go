package schedule

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// responseFromRPCError 负责把 schedule 的 gRPC 错误反解回项目内错误。
//
// 职责边界：
// 1. 只在 gateway 边缘层使用，不下沉到 schedule 服务实现里；
// 2. 业务错误尽量恢复成 respond.Response，方便 API 层继续复用 DealWithError；
// 3. 服务不可用或未知内部错误包装成普通 error，避免误报成用户可修正的参数问题。
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
			msg = "schedule zrpc service internal error"
		}
		return wrapRPCError(errors.New(msg))
	}

	msg := strings.TrimSpace(st.Message())
	if msg == "" {
		msg = "schedule zrpc service rejected request"
	}
	return respond.Response{Status: grpcCodeToRespondStatus(st.Code()), Info: msg}
}

func responseFromStatus(st *status.Status) (respond.Response, bool) {
	if st == nil {
		return respond.Response{}, false
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		statusValue := strings.TrimSpace(info.Reason)
		if statusValue == "" {
			statusValue = grpcCodeToRespondStatus(st.Code())
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
	return fmt.Errorf("调用 schedule zrpc 服务失败: %w", err)
}
