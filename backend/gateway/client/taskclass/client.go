package taskclass

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	taskclasspb "github.com/LoveLosita/smartflow/backend/services/task_class/rpc/pb"
	taskclasscontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/taskclass"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultEndpoint = "127.0.0.1:9086"
	defaultTimeout  = 6 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 访问 task-class zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和 JSON 透传，不触碰 DAO 或迁移期 schedule 直写细节；
// 2. HTTP 入参仍由 gateway/api 做基础绑定，业务校验交给 task-class 服务；
// 3. 复杂响应不在 gateway 重建模型，避免 DTO 复制扩散。
type Client struct {
	rpc taskclasspb.TaskClassClient
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
	client := &Client{rpc: taskclasspb.NewTaskClassClient(zclient.Conn())}
	if err := client.ping(timeout); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) AddTaskClass(ctx context.Context, req taskclasscontracts.UpsertTaskClassRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.AddTaskClass, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) ListTaskClasses(ctx context.Context, userID int) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.ListTaskClasses, taskclasscontracts.UserRequest{UserID: userID})
	return jsonFromResponse(resp, err)
}

func (c *Client) GetTaskClass(ctx context.Context, req taskclasscontracts.GetTaskClassRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetTaskClass, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) UpdateTaskClass(ctx context.Context, req taskclasscontracts.UpsertTaskClassRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.UpdateTaskClass, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetAgentTaskClasses(ctx context.Context, req taskclasscontracts.AgentTaskClassesRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetAgentTaskClasses, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) InsertTaskClassItemIntoSchedule(ctx context.Context, req taskclasscontracts.InsertTaskClassItemIntoScheduleRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.InsertTaskClassItemIntoSchedule, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) DeleteTaskClassItem(ctx context.Context, req taskclasscontracts.DeleteTaskClassItemRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.DeleteTaskClassItem, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) DeleteTaskClass(ctx context.Context, req taskclasscontracts.DeleteTaskClassRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.DeleteTaskClass, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) ApplyBatchIntoSchedule(ctx context.Context, req taskclasscontracts.ApplyBatchIntoScheduleRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.ApplyBatchIntoSchedule, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("task-class zrpc client is not initialized")
	}
	return nil
}

func (c *Client) ping(timeout time.Duration) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := c.rpc.Ping(ctx, &taskclasspb.StatusResponse{})
	return responseFromRPCError(err)
}

func (c *Client) callJSON(ctx context.Context, fn func(context.Context, *taskclasspb.JSONRequest, ...grpc.CallOption) (*taskclasspb.JSONResponse, error), payload any) (*taskclasspb.JSONResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return fn(ctx, &taskclasspb.JSONRequest{PayloadJson: raw})
}

func jsonFromResponse(resp *taskclasspb.JSONResponse, rpcErr error) (json.RawMessage, error) {
	if rpcErr != nil {
		return nil, responseFromRPCError(rpcErr)
	}
	if resp == nil {
		return nil, errors.New("task-class zrpc service returned empty JSON response")
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
