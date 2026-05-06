package llm

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func responseFromRPCError(err error) error {
	if err == nil {
		return nil
	}

	st, ok := status.FromError(err)
	if !ok {
		return wrapRPCError(err)
	}
	if message, ok := messageFromStatus(st); ok {
		switch st.Code() {
		case codes.InvalidArgument, codes.ResourceExhausted, codes.FailedPrecondition:
			return errors.New(message)
		}
	}

	switch st.Code() {
	case codes.InvalidArgument, codes.ResourceExhausted, codes.FailedPrecondition:
		return errors.New(strings.TrimSpace(st.Message()))
	case codes.Internal, codes.Unknown, codes.Unavailable, codes.DeadlineExceeded, codes.DataLoss, codes.Unimplemented:
		msg := strings.TrimSpace(st.Message())
		if msg == "" {
			msg = "llm zrpc service internal error"
		}
		return wrapRPCError(errors.New(msg))
	default:
		msg := strings.TrimSpace(st.Message())
		if msg == "" {
			msg = "llm zrpc service rejected request"
		}
		return errors.New(msg)
	}
}

func messageFromStatus(st *status.Status) (string, bool) {
	if st == nil {
		return "", false
	}
	for _, detail := range st.Details() {
		info, ok := detail.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		message := strings.TrimSpace(st.Message())
		if message == "" && info.Metadata != nil {
			message = strings.TrimSpace(info.Metadata["info"])
		}
		if message == "" {
			message = strings.TrimSpace(info.Reason)
		}
		return message, message != ""
	}
	return "", false
}

func wrapRPCError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("调用 llm zrpc 服务失败: %w", err)
}
