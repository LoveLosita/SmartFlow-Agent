package activescheduler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	activepb "github.com/LoveLosita/smartflow/backend/services/active_scheduler/rpc/pb"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/activescheduler"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9083"
	defaultTimeout  = 8 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 侧 active-scheduler zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和响应 JSON 透传，不碰 DAO、graph、outbox 或 job scanner；
// 2. confirm/apply 业务拒绝从 gRPC status 反解成共享 ApplyError，便于 API 层维持既有响应形状；
// 3. 复杂响应不在 gateway 重新建模，避免主动调度 DTO 复制扩散。
type Client struct {
	rpc activepb.ActiveSchedulerClient
}

func NewClient(cfg ClientConfig) (*Client, error) {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	endpoints := normalizeEndpoints(cfg.Endpoints)
	target := strings.TrimSpace(cfg.Target)
	if len(endpoints) == 0 && target == "" {
		endpoints = []string{defaultEndpoint}
	}

	zclient, err := zrpc.NewClient(zrpc.RpcClientConf{
		Endpoints: endpoints,
		Target:    target,
		NonBlock:  true,
		Timeout:   int64(timeout / time.Millisecond),
	})
	if err != nil {
		return nil, err
	}
	return &Client{rpc: activepb.NewActiveSchedulerClient(zclient.Conn())}, nil
}

func (c *Client) DryRun(ctx context.Context, req contracts.ActiveScheduleRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.DryRun(ctx, requestToPB(req))
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return jsonFromResponse(resp)
}

func (c *Client) Trigger(ctx context.Context, req contracts.ActiveScheduleRequest) (*contracts.TriggerResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.Trigger(ctx, requestToPB(req))
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return triggerFromPB(resp), nil
}

func (c *Client) CreatePreview(ctx context.Context, req contracts.ActiveScheduleRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.CreatePreview(ctx, requestToPB(req))
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return jsonFromResponse(resp)
}

func (c *Client) GetPreview(ctx context.Context, req contracts.GetPreviewRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetPreview(ctx, &activepb.GetPreviewRequest{
		UserId:    int64(req.UserID),
		PreviewId: req.PreviewID,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return jsonFromResponse(resp)
}

func (c *Client) ConfirmPreview(ctx context.Context, req contracts.ConfirmPreviewRequest) (json.RawMessage, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.ConfirmPreview(ctx, confirmToPB(req))
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return jsonFromResponse(resp)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("active-scheduler zrpc client is not initialized")
	}
	return nil
}

func requestToPB(req contracts.ActiveScheduleRequest) *activepb.ActiveScheduleRequest {
	mockNowUnixNano := int64(0)
	if req.MockNow != nil && !req.MockNow.IsZero() {
		mockNowUnixNano = req.MockNow.UnixNano()
	}
	return &activepb.ActiveScheduleRequest{
		UserId:          int64(req.UserID),
		TriggerType:     req.TriggerType,
		TargetType:      req.TargetType,
		TargetId:        int64(req.TargetID),
		FeedbackId:      req.FeedbackID,
		IdempotencyKey:  req.IdempotencyKey,
		MockNowUnixNano: mockNowUnixNano,
		PayloadJson:     []byte(req.Payload),
	}
}

func confirmToPB(req contracts.ConfirmPreviewRequest) *activepb.ConfirmPreviewRequest {
	requestedAtUnixNano := int64(0)
	if !req.RequestedAt.IsZero() {
		requestedAtUnixNano = req.RequestedAt.UnixNano()
	}
	return &activepb.ConfirmPreviewRequest{
		UserId:              int64(req.UserID),
		PreviewId:           req.PreviewID,
		CandidateId:         req.CandidateID,
		Action:              req.Action,
		EditedChangesJson:   []byte(req.EditedChanges),
		IdempotencyKey:      req.IdempotencyKey,
		RequestedAtUnixNano: requestedAtUnixNano,
		TraceId:             req.TraceID,
	}
}

func triggerFromPB(resp *activepb.TriggerResponse) *contracts.TriggerResponse {
	if resp == nil {
		return &contracts.TriggerResponse{}
	}
	var previewID *string
	if resp.HasPreviewId {
		value := resp.PreviewId
		previewID = &value
	}
	return &contracts.TriggerResponse{
		TriggerID: resp.TriggerId,
		Status:    resp.Status,
		PreviewID: previewID,
		DedupeHit: resp.DedupeHit,
		TraceID:   resp.TraceId,
	}
}

func jsonFromResponse(resp *activepb.JSONResponse) (json.RawMessage, error) {
	if resp == nil {
		return nil, errors.New("active-scheduler zrpc service returned empty JSON response")
	}
	if len(resp.DataJson) == 0 {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(resp.DataJson), nil
}

func normalizeEndpoints(values []string) []string {
	endpoints := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			endpoints = append(endpoints, trimmed)
		}
	}
	return endpoints
}
