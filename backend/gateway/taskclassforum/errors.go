package taskclassforum

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// responseFromRPCError 把计划广场 zrpc 错误恢复成 HTTP 层可处理的业务错误。
//
// 职责边界：
// 1. 优先读取 taskclassforum RPC 写入的 ErrorInfo，恢复 respond.Response；
// 2. 对网络、超时、服务不可用等非业务错误保留为普通 error，让 HTTP 层按 500 处理；
// 3. 暂不复用 userauth/errors.go，因为 user/auth 还承担历史 legacy code 兼容，计划广场只消费新 ErrorInfo 协议。
func responseFromRPCError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return wrapRPCError(err)
	}
	if resp, ok := responseFromStatusDetails(st); ok {
		return resp
	}

	switch st.Code() {
	case codes.Internal, codes.Unknown, codes.Unavailable, codes.DeadlineExceeded, codes.DataLoss, codes.Unimplemented:
		msg := strings.TrimSpace(st.Message())
		if msg == "" {
			msg = "taskclassforum zrpc service internal error"
		}
		return wrapRPCError(errors.New(msg))
	case codes.NotFound:
		return responseWithFallback(st, respond.UserTaskClassNotFound)
	case codes.PermissionDenied, codes.Unauthenticated:
		return responseWithFallback(st, respond.ErrUnauthorized)
	case codes.InvalidArgument:
		return responseWithFallback(st, respond.MissingParam)
	}

	msg := strings.TrimSpace(st.Message())
	if msg == "" {
		msg = "taskclassforum zrpc service rejected request"
	}
	return respond.Response{Status: "400", Info: msg}
}

func responseFromStatusDetails(st *status.Status) (respond.Response, bool) {
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

func responseWithFallback(st *status.Status, fallback respond.Response) respond.Response {
	msg := strings.TrimSpace(st.Message())
	if msg == "" {
		msg = fallback.Info
	}
	return respond.Response{Status: fallback.Status, Info: msg}
}

func wrapRPCError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("调用 taskclassforum zrpc 服务失败: %w", err)
}
