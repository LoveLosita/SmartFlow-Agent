package course

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CourseImportConflictError 表示课程导入和已有非课程日程冲突。
//
// 职责边界：
// 1. 只在 gateway 边缘层用于恢复旧 HTTP 409 + conflicts 响应；
// 2. 不承载冲突计算逻辑，冲突详情由 course 服务生成；
// 3. ConflictsJSON 返回原始 JSON，避免 gateway 复制 schedule 冲突 DTO。
type CourseImportConflictError struct {
	conflicts json.RawMessage
}

func (e CourseImportConflictError) Error() string {
	return respond.ScheduleConflict.Info
}

func (e CourseImportConflictError) ConflictsJSON() json.RawMessage {
	if len(e.conflicts) == 0 {
		return json.RawMessage("[]")
	}
	return e.conflicts
}

// responseFromRPCError 负责把 course 的 gRPC 错误反解回项目内错误。
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
			msg = "course zrpc service internal error"
		}
		return wrapRPCError(errors.New(msg))
	}

	msg := strings.TrimSpace(st.Message())
	if msg == "" {
		msg = "course zrpc service rejected request"
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
	case codes.PermissionDenied:
		return respond.ErrUnauthorized.Status
	case codes.InvalidArgument:
		return respond.WrongParamType.Status
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
	return fmt.Errorf("调用 course zrpc 服务失败: %w", err)
}
