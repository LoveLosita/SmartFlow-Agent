package rpc

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc/pb"
	tokenstoresv "github.com/LoveLosita/smartflow/backend/services/tokenstore/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9095"
	defaultTimeout  = 2 * time.Second
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *tokenstoresv.Service
}

// Start 启动 token-store zrpc 服务。
//
// 职责边界：
// 1. 只负责装配 go-zero zrpc server 和注册 protobuf service；
// 2. 不创建 DB 连接，也不接入 user/auth 授额出口，这些依赖由 cmd 入口注入；
// 3. 启动后阻塞当前进程，保持后续“一服务一进程”的迁移方向。
func Start(opts ServerOptions) {
	server, listenOn, err := NewServer(opts)
	if err != nil {
		log.Fatalf("failed to build tokenstore zrpc server: %v", err)
	}
	defer server.Stop()

	log.Printf("tokenstore zrpc service starting on %s", listenOn)
	server.Start()
}

// NewServer 负责创建 token-store RPC server，供 cmd 启动和测试复用。
func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("tokenstore service dependency not initialized")
	}

	listenOn := strings.TrimSpace(opts.ListenOn)
	if listenOn == "" {
		listenOn = defaultListenOn
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	server, err := zrpc.NewServer(zrpc.RpcServerConf{
		ServiceConf: service.ServiceConf{
			Name: "tokenstore.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterTokenStoreServiceServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	return server, listenOn, nil
}
