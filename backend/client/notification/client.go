package notification

import (
	"context"
	"errors"
	"strings"
	"time"

	notificationpb "github.com/LoveLosita/smartflow/backend/services/notification/rpc/pb"
	contracts "github.com/LoveLosita/smartflow/backend/shared/contracts/notification"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9082"
	defaultTimeout  = 6 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 侧 notification zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和响应转译，不碰 DB / provider / outbox 细节；
// 2. 服务端业务错误先通过 gRPC status 传输，再在这里反解回 respond.Response 风格；
// 3. 上层调用方仍然可以保持 `res, err :=` 的统一用法。
type Client struct {
	rpc notificationpb.NotificationClient
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
	return &Client{rpc: notificationpb.NewNotificationClient(zclient.Conn())}, nil
}

func (c *Client) GetFeishuWebhook(ctx context.Context, req contracts.GetFeishuWebhookRequest) (*contracts.ChannelResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.GetFeishuWebhook(ctx, &notificationpb.GetFeishuWebhookRequest{
		UserId: int64(req.UserID),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return channelFromResponse(resp)
}

func (c *Client) SaveFeishuWebhook(ctx context.Context, req contracts.SaveFeishuWebhookRequest) (*contracts.ChannelResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.SaveFeishuWebhook(ctx, &notificationpb.SaveFeishuWebhookRequest{
		UserId:      int64(req.UserID),
		Enabled:     req.Enabled,
		WebhookUrl:  req.WebhookURL,
		AuthType:    req.AuthType,
		BearerToken: req.BearerToken,
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return channelFromResponse(resp)
}

func (c *Client) DeleteFeishuWebhook(ctx context.Context, req contracts.DeleteFeishuWebhookRequest) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	resp, err := c.rpc.DeleteFeishuWebhook(ctx, &notificationpb.DeleteFeishuWebhookRequest{
		UserId: int64(req.UserID),
	})
	if err != nil {
		return responseFromRPCError(err)
	}
	if resp == nil {
		return errors.New("notification zrpc service returned empty delete response")
	}
	return nil
}

func (c *Client) TestFeishuWebhook(ctx context.Context, req contracts.TestFeishuWebhookRequest) (*contracts.TestResult, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	resp, err := c.rpc.TestFeishuWebhook(ctx, &notificationpb.TestFeishuWebhookRequest{
		UserId: int64(req.UserID),
	})
	if err != nil {
		return nil, responseFromRPCError(err)
	}
	return testResultFromResponse(resp)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("notification zrpc client is not initialized")
	}
	return nil
}

func channelFromResponse(resp *notificationpb.ChannelResponse) (*contracts.ChannelResponse, error) {
	if resp == nil {
		return nil, errors.New("notification zrpc service returned empty channel response")
	}
	var lastTestAt *time.Time
	if value := timeFromUnixNano(resp.LastTestAtUnixNano); !value.IsZero() {
		lastTestAt = &value
	}
	return &contracts.ChannelResponse{
		Channel:        resp.Channel,
		Enabled:        resp.Enabled,
		Configured:     resp.Configured,
		WebhookURLMask: resp.WebhookUrlMask,
		AuthType:       resp.AuthType,
		HasBearerToken: resp.HasBearerToken,
		LastTestStatus: resp.LastTestStatus,
		LastTestError:  resp.LastTestError,
		LastTestAt:     lastTestAt,
	}, nil
}

func testResultFromResponse(resp *notificationpb.TestResult) (*contracts.TestResult, error) {
	if resp == nil {
		return nil, errors.New("notification zrpc service returned empty test response")
	}
	channel, err := channelFromResponse(resp.Channel)
	if err != nil {
		return nil, err
	}
	return &contracts.TestResult{
		Channel:  *channel,
		Status:   resp.Status,
		Outcome:  resp.Outcome,
		Message:  resp.Message,
		TraceID:  resp.TraceId,
		SentAt:   timeFromUnixNano(resp.SentAtUnixNano),
		Skipped:  resp.Skipped,
		Provider: resp.Provider,
	}, nil
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

func timeFromUnixNano(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.Unix(0, value)
}
