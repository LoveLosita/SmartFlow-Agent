package memory

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	memorypb "github.com/LoveLosita/smartflow/backend/services/memory/rpc/pb"
	memorycontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/memory"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultEndpoint = "127.0.0.1:9088"
	defaultTimeout  = 6 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 访问 memory zrpc 的最小适配层。
//
// 职责边界：
// 1. 只负责跨进程 gRPC 调用和 JSON 透传，不触碰 memory repo、worker 或 outbox；
// 2. HTTP 入参仍由 gateway/api 做基础绑定，业务校验交给 memory 服务；
// 3. 复杂响应不在 gateway 重建模型，避免 DTO 复制扩散。
type Client struct {
	rpc memorypb.MemoryClient
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
	// 1. 这里不在构造期 Ping memory 服务，避免 cmd/memory 短暂不可用时拖垮整个 gateway/worker 启动。
	// 2. 真正的可用性检查延迟到各个 RPC 调用，由 `/api/v1/memory/*` 自己返回局部错误。
	client := &Client{rpc: memorypb.NewMemoryClient(zclient.Conn())}
	return client, nil
}

// Retrieve 调用 memory 服务完成 agent 记忆读取。
//
// 职责边界：
// 1. 只负责跨进程 JSON 编解码和 gRPC 错误还原；
// 2. 不在 gateway 侧重做召回、过滤或 prompt 渲染；
// 3. 返回 ItemDTO 给 agent 适配器继续转换为内部模型。
func (c *Client) Retrieve(ctx context.Context, req memorycontracts.RetrieveRequest) ([]memorycontracts.ItemDTO, error) {
	resp, err := c.callJSON(ctx, c.rpc.Retrieve, req)
	raw, err := jsonFromResponse(resp, err)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var items []memorycontracts.ItemDTO
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func (c *Client) ListItems(ctx context.Context, req memorycontracts.ListItemsRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.ListItems, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) GetItem(ctx context.Context, req memorycontracts.GetItemRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.GetItem, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) CreateItem(ctx context.Context, req memorycontracts.CreateItemRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.CreateItem, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) UpdateItem(ctx context.Context, req memorycontracts.UpdateItemRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.UpdateItem, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) DeleteItem(ctx context.Context, req memorycontracts.DeleteItemRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.DeleteItem, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) RestoreItem(ctx context.Context, req memorycontracts.RestoreItemRequest) (json.RawMessage, error) {
	resp, err := c.callJSON(ctx, c.rpc.RestoreItem, req)
	return jsonFromResponse(resp, err)
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("memory zrpc client is not initialized")
	}
	return nil
}

func (c *Client) callJSON(ctx context.Context, fn func(context.Context, *memorypb.JSONRequest, ...grpc.CallOption) (*memorypb.JSONResponse, error), payload any) (*memorypb.JSONResponse, error) {
	if err := c.ensureReady(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return fn(ctx, &memorypb.JSONRequest{PayloadJson: raw})
}

func jsonFromResponse(resp *memorypb.JSONResponse, rpcErr error) (json.RawMessage, error) {
	if rpcErr != nil {
		return nil, responseFromRPCError(rpcErr)
	}
	if resp == nil {
		return nil, errors.New("memory zrpc service returned empty JSON response")
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
