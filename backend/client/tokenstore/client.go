package tokenstore

import (
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	"github.com/zeromicro/go-zero/zrpc"
)

const (
	defaultEndpoint = "127.0.0.1:9095"
	defaultTimeout  = 2 * time.Second
)

type ClientConfig struct {
	Endpoints []string
	Target    string
	Timeout   time.Duration
}

// Client 是 gateway 访问 tokenstore zrpc 的统一 Credit 语义适配层。
//
// 职责边界：
// 1. 只负责 HTTP gateway 与 tokenstore zrpc 之间的协议转译；
// 2. 不直连底层 credit_* 表，也不承载订单/充值/扣费业务规则；
// 3. gRPC 业务错误会在这里反解成普通 error / respond.Response，交给 HTTP 层统一处理。
type Client struct {
	rpc pb.TokenStoreServiceClient
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
	return &Client{rpc: pb.NewTokenStoreServiceClient(zclient.Conn())}, nil
}

func (c *Client) ensureReady() error {
	if c == nil || c.rpc == nil {
		return errors.New("tokenstore zrpc client is not initialized")
	}
	return nil
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

func stringPtrFromNonEmpty(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
