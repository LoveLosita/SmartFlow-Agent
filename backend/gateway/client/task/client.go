package task

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	taskpb "github.com/LoveLosita/smartflow/backend/services/task/rpc/pb"
	taskcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/task"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultEndpoint = "127.0.0.1:9085"
	defaultTimeout  = 6 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 侧 task zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和 JSON 透传，不碰 DAO、outbox 或 active-scheduler job；
// 2. HTTP 入参仍由 gateway/api 做基础绑定，业务校验交给 task 服务；
// 3. 复杂响应不在 gateway 重建模型，避免 DTO 复制扩散。
type Client struct {
	rpc taskpb.TaskClient
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
	client := &Client{rpc: taskpb.NewTaskClient(zclient.Conn())}
	if err := client.ping(timeout); err != nil {
		return nil, err
	}
	return client, nil
}

func (c *Client) AddTask(ctx context.Context, req taskcontracts.AddTaskRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.AddTask, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetUserTasks(ctx context.Context, userID int) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetUserTasks, taskcontracts.UserRequest{UserID: userID})
	return jsonFromResponse(resp, err)
}

func (c *Client) BatchTaskStatus(ctx context.Context, req taskcontracts.BatchTaskStatusRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.BatchTaskStatus, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) CompleteTask(ctx context.Context, req taskcontracts.CompleteTaskRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.CompleteTask, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) UndoCompleteTask(ctx context.Context, req taskcontracts.UndoCompleteTaskRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.UndoCompleteTask, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) UpdateTask(ctx context.Context, req taskcontracts.UpdateTaskRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.UpdateTask, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) DeleteTask(ctx context.Context, req taskcontracts.DeleteTaskRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.DeleteTask, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("task zrpc client is not initialized")
	}
	return nil
}

func (c *Client) ping(timeout time.Duration) error {
	if err := c.ensureReady(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err := c.rpc.Ping(ctx, &taskpb.StatusResponse{})
	return responseFromRPCError(err)
}

func (c *Client) callJSON(ctx context.Context, fn func(context.Context, *taskpb.JSONRequest, ...grpc.CallOption) (*taskpb.JSONResponse, error), payload any) (*taskpb.JSONResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return fn(ctx, &taskpb.JSONRequest{PayloadJson: raw})
}

func jsonFromResponse(resp *taskpb.JSONResponse, rpcErr error) (json.RawMessage, error) {
	if rpcErr != nil {
		return nil, responseFromRPCError(rpcErr)
	}
	if resp == nil {
		return nil, errors.New("task zrpc service returned empty JSON response")
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
