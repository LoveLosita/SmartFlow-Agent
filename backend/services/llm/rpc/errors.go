package rpc

import (
	"errors"
	"log"
	"strings"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const llmErrorDomain = "smartflow.llm"

func grpcErrorFromServiceError(err error) error {
	if err == nil {
		return nil
	}

	var resp respond.Response
	if errors.As(err, &resp) {
		return grpcErrorFromResponse(resp)
	}

	switch {
	case errors.Is(err, llmservice.ErrUnsupportedModelAlias):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, llmservice.ErrCreditBalanceBlocked):
		return status.Error(codes.ResourceExhausted, err.Error())
	case errors.Is(err, llmservice.ErrRuntimeServiceNotReady):
		return status.Error(codes.FailedPrecondition, err.Error())
	}

	log.Printf("llm rpc internal error: %v", err)
	return status.Error(codes.Internal, "llm service internal error")
}

func grpcErrorFromResponse(resp respond.Response) error {
	code := grpcCodeFromRespondStatus(resp.Status)
	message := strings.TrimSpace(resp.Info)
	if message == "" {
		message = strings.TrimSpace(resp.Status)
	}

	st := status.New(code, message)
	detail := &errdetails.ErrorInfo{
		Domain: llmErrorDomain,
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
	case respond.MissingParam.Status, respond.WrongParamType.Status:
		return codes.InvalidArgument
	}
	if strings.HasPrefix(strings.TrimSpace(statusValue), "5") {
		return codes.Internal
	}
	return codes.InvalidArgument
}
