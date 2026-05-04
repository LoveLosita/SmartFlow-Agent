package rpc

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/userauth/rpc/pb"
	userauthsv "github.com/LoveLosita/smartflow/backend/services/userauth/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9081"
	defaultTimeout  = 2 * time.Second
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *userauthsv.Service
}

// Start 启动 user/auth zrpc 服务。
//
// 职责边界：
// 1. 只负责装配 gozero zrpc server 和注册 protobuf service；
// 2. 不创建 DB/Redis 连接，这些依赖由 cmd/userauth 入口注入；
// 3. 阻塞直到进程收到退出信号，保持一个服务一个独立进程的迁移方向。
func Start(opts ServerOptions) {
	server, listenOn, err := NewServer(opts)
	if err != nil {
		log.Fatalf("failed to build userauth zrpc server: %v", err)
	}
	defer server.Stop()

	log.Printf("userauth zrpc service starting on %s", listenOn)
	server.Start()
}

func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("userauth service dependency not initialized")
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
			Name: "userauth.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterUserAuthServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	return server, listenOn, nil
}
