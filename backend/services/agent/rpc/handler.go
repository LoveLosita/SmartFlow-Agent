package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
	"github.com/LoveLosita/smartflow/backend/services/agent/rpc/pb"
	agentsv "github.com/LoveLosita/smartflow/backend/services/agent/sv"
	agentcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/agent"
)

type Handler struct {
	pb.UnimplementedAgentServer
	svc *agentsv.AgentService
}

func NewHandler(svc *agentsv.AgentService) *Handler {
	return &Handler{svc: svc}
}

// Ping 供调用方在启动期确认 agent zrpc 已可用。
func (h *Handler) Ping(ctx context.Context, req *pb.StatusResponse) (*pb.StatusResponse, error) {
	if err := h.ensureReady(req); err != nil {
		return nil, err
	}
	return &pb.StatusResponse{}, nil
}

// Chat 把 agent 内部 channel 输出适配为 gRPC server-stream。
//
// 职责边界：
// 1. RPC 层只负责协议转换，不改写 agent/sv 的图编排、工具调用和持久化语义；
// 2. AgentService 内部仍使用 channel 解耦节点输出，跨进程边界统一转换为 stream.Send；
// 3. 业务错误通过 error_json chunk 传给 Gateway，由 Gateway 保持原 SSE 错误体输出。
func (h *Handler) Chat(req *pb.ChatRequest, stream pb.Agent_ChatServer) error {
	if err := h.ensureReady(req); err != nil {
		return err
	}
	extra, err := decodeExtra(req.ExtraJson)
	if err != nil {
		return grpcErrorFromServiceError(respond.WrongParamType)
	}

	outChan, errChan := h.svc.AgentChat(
		stream.Context(),
		req.Message,
		req.Thinking,
		req.Model,
		int(req.UserId),
		req.ConversationId,
		extra,
	)

	for outChan != nil || errChan != nil {
		select {
		case err, ok := <-errChan:
			if !ok {
				// 1. errChan 关闭表示当前没有更多异步错误；置 nil 后让 select 不再命中该分支。
				// 2. 若继续读取已关闭 channel，会形成忙等并拖慢长连接 stream。
				errChan = nil
				continue
			}
			if err == nil {
				continue
			}
			errorJSON := buildStreamErrorJSON(err)
			return stream.Send(&pb.ChatChunk{Done: true, ErrorJson: errorJSON})
		case payload, ok := <-outChan:
			if !ok {
				outChan = nil
				return stream.Send(&pb.ChatChunk{Done: true})
			}
			if err := stream.Send(&pb.ChatChunk{Payload: payload}); err != nil {
				return err
			}
			if strings.TrimSpace(payload) == "[DONE]" {
				// 1. AgentService 旧链路已经把 OpenAI 兼容的 [DONE] 当作普通 payload 推给前端。
				// 2. RPC 层只负责跨进程透传；这里直接结束 stream，避免 Gateway 再补一帧重复 [DONE]。
				return nil
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
	return stream.Send(&pb.ChatChunk{Done: true})
}

// GetConversationMeta 透传查询单个会话元信息。
//
// 职责边界：
// 1. RPC 层只负责 JSON 契约反序列化和响应序列化；
// 2. 会话归属、404 语义和 DTO 组装继续由 AgentService 决定；
// 3. Gateway 仍负责 HTTP query 绑定和最终响应包装。
func (h *Handler) GetConversationMeta(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	var payload agentcontracts.ConversationQueryRequest
	if err := h.decodeJSONRequest(req, &payload); err != nil {
		return nil, err
	}
	resp, err := h.svc.GetConversationMeta(ctx, payload.UserID, payload.ConversationID)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponseFromPayload(resp)
}

// GetConversationList 透传查询当前用户会话列表。
func (h *Handler) GetConversationList(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	var payload agentcontracts.ConversationListRequest
	if err := h.decodeJSONRequest(req, &payload); err != nil {
		return nil, err
	}
	resp, err := h.svc.GetConversationList(ctx, payload.UserID, payload.Page, payload.PageSize, payload.Status)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponseFromPayload(resp)
}

// GetConversationTimeline 透传查询会话时间线。
func (h *Handler) GetConversationTimeline(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	var payload agentcontracts.ConversationQueryRequest
	if err := h.decodeJSONRequest(req, &payload); err != nil {
		return nil, err
	}
	resp, err := h.svc.GetConversationTimeline(ctx, payload.UserID, payload.ConversationID)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponseFromPayload(resp)
}

// GetSchedulePlanPreview 透传查询会话内排程预览。
func (h *Handler) GetSchedulePlanPreview(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	var payload agentcontracts.ConversationQueryRequest
	if err := h.decodeJSONRequest(req, &payload); err != nil {
		return nil, err
	}
	resp, err := h.svc.GetSchedulePlanPreview(ctx, payload.UserID, payload.ConversationID)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return jsonResponseFromPayload(resp)
}

// GetContextStats 透传查询会话上下文 token 统计 JSON。
func (h *Handler) GetContextStats(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	var payload agentcontracts.ConversationQueryRequest
	if err := h.decodeJSONRequest(req, &payload); err != nil {
		return nil, err
	}
	statsJSON, err := h.svc.GetContextStats(ctx, payload.UserID, payload.ConversationID)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.JSONResponse{DataJson: []byte(strings.TrimSpace(statsJSON))}, nil
}

// SaveScheduleState 透传保存会话内排程拖拽状态。
//
// 职责边界：
// 1. RPC 层只把跨进程契约转换为 AgentService 既有模型；
// 2. 快照读取、归属校验、坐标转换和 Redis 回写仍由 AgentService 完成；
// 3. 成功时返回空 JSON 响应，Gateway 继续保持 data=null 的 HTTP 语义。
func (h *Handler) SaveScheduleState(ctx context.Context, req *pb.JSONRequest) (*pb.JSONResponse, error) {
	var payload agentcontracts.SaveScheduleStateRequest
	if err := h.decodeJSONRequest(req, &payload); err != nil {
		return nil, err
	}
	if err := h.svc.SaveScheduleState(ctx, payload.UserID, payload.ConversationID, toModelScheduleStateItems(payload.Items)); err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.JSONResponse{}, nil
}

func (h *Handler) ensureReady(req any) error {
	if h == nil || h.svc == nil {
		return grpcErrorFromServiceError(errAgentServiceNotReady)
	}
	if req == nil {
		return grpcErrorFromServiceError(respond.MissingParam)
	}
	return nil
}

func (h *Handler) decodeJSONRequest(req *pb.JSONRequest, out any) error {
	if err := h.ensureReady(req); err != nil {
		return err
	}
	if len(req.PayloadJson) == 0 {
		return grpcErrorFromServiceError(respond.MissingParam)
	}
	if err := json.Unmarshal(req.PayloadJson, out); err != nil {
		return grpcErrorFromServiceError(respond.WrongParamType)
	}
	return nil
}

func jsonResponseFromPayload(payload any) (*pb.JSONResponse, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, grpcErrorFromServiceError(err)
	}
	return &pb.JSONResponse{DataJson: raw}, nil
}

func toModelScheduleStateItems(items []agentcontracts.SaveScheduleStatePlacedItem) []model.SaveScheduleStatePlacedItem {
	if len(items) == 0 {
		return nil
	}
	result := make([]model.SaveScheduleStatePlacedItem, 0, len(items))
	for _, item := range items {
		result = append(result, model.SaveScheduleStatePlacedItem{
			TaskItemID:         item.TaskItemID,
			Week:               item.Week,
			DayOfWeek:          item.DayOfWeek,
			StartSection:       item.StartSection,
			EndSection:         item.EndSection,
			EmbedCourseEventID: item.EmbedCourseEventID,
		})
	}
	return result
}

func decodeExtra(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var extra map[string]any
	if err := json.Unmarshal(raw, &extra); err != nil {
		return nil, err
	}
	return extra, nil
}

func buildStreamErrorJSON(err error) []byte {
	errorBody := map[string]any{
		"message": err.Error(),
		"type":    "server_error",
	}
	var respErr respond.Response
	if errors.As(err, &respErr) {
		errorBody["code"] = respErr.Status
		if respErr.Info != "" {
			errorBody["message"] = respErr.Info
		}
	}
	raw, marshalErr := json.Marshal(map[string]any{"error": errorBody})
	if marshalErr != nil {
		return []byte(`{"error":{"message":"agent stream error","type":"server_error"}}`)
	}
	return raw
}
