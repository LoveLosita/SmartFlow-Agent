package rpc

import (
	"context"
	"errors"
	"io"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	llmcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/llm"
	"github.com/LoveLosita/smartflow/backend/shared/respond"
)

type Handler struct {
	UnimplementedLLMServer
	svc *llmservice.RuntimeService
}

func NewHandler(svc *llmservice.RuntimeService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Ping(ctx context.Context, req *llmcontracts.PingRequest) (*llmcontracts.PingResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	return &llmcontracts.PingResponse{}, nil
}

func (h *Handler) GenerateText(ctx context.Context, req *llmcontracts.TextRequest) (*llmcontracts.TextResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	result, err := h.svc.GenerateText(ctx, *req)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &llmcontracts.TextResponse{Result: llmserviceToContractTextResult(result)}, nil
}

func (h *Handler) StreamText(req *llmcontracts.StreamTextRequest, stream LLM_StreamTextServer) error {
	if err := h.ensureReady(req); err != nil {
		return err
	}

	reader, err := h.svc.StreamText(stream.Context(), *req)
	if err != nil {
		return grpcErrorFromServiceError(err)
	}
	if reader == nil {
		return grpcErrorFromServiceError(llmservice.ErrRuntimeServiceNotReady)
	}
	defer reader.Close()

	for {
		message, recvErr := reader.Recv()
		if recvErr != nil {
			if errors.Is(recvErr, io.EOF) {
				return nil
			}
			return grpcErrorFromServiceError(recvErr)
		}
		if message == nil {
			continue
		}
		if err = stream.Send(&llmcontracts.StreamChunk{Message: message}); err != nil {
			return err
		}
	}
}

func (h *Handler) GenerateResponsesText(ctx context.Context, req *llmcontracts.ResponsesRequest) (*llmcontracts.ResponsesResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	result, err := h.svc.GenerateResponsesText(ctx, *req)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &llmcontracts.ResponsesResponse{Result: llmserviceToContractResponsesResult(result)}, nil
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(llmservice.ErrRuntimeServiceNotReady)
	}
	if req == nil {
		return grpcErrorFromServiceError(respond.MissingParam)
	}
	return nil
}

func llmserviceToContractTextResult(result *llmservice.TextResult) *llmcontracts.TextResult {
	if result == nil {
		return nil
	}
	return &llmcontracts.TextResult{
		Text:         result.Text,
		Usage:        llmservice.CloneUsage(result.Usage),
		FinishReason: result.FinishReason,
	}
}

func llmserviceToContractResponsesResult(result *llmservice.ArkResponsesResult) *llmcontracts.ResponsesResult {
	if result == nil {
		return nil
	}
	output := &llmcontracts.ResponsesResult{
		Text:             result.Text,
		Status:           result.Status,
		IncompleteReason: result.IncompleteReason,
		ErrorCode:        result.ErrorCode,
		ErrorMessage:     result.ErrorMessage,
	}
	if result.Usage != nil {
		output.Usage = &llmcontracts.ResponsesUsage{
			InputTokens:  result.Usage.InputTokens,
			OutputTokens: result.Usage.OutputTokens,
			TotalTokens:  result.Usage.TotalTokens,
		}
	}
	return output
}
