package rpc

import (
	"errors"
	"log"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/rpc/pb"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9090"
	defaultTimeout  = 2 * time.Second
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *forumsv.Service
}

// Start 启动计划广场 zrpc 服务。
//
// 职责边界：
// 1. 只负责装配 go-zero zrpc server 和注册 protobuf service；
// 2. 不创建 DB 连接，也不装配 TaskClass legacy adapter，这些依赖由 cmd 入口注入；
// 3. 启动后阻塞当前进程，保持后续“一服务一进程”的迁移方向。
func Start(opts ServerOptions) {
	server, listenOn, err := NewServer(opts)
	if err != nil {
		log.Fatalf("failed to build taskclassforum zrpc server: %v", err)
	}
	defer server.Stop()

	log.Printf("taskclassforum zrpc service starting on %s", listenOn)
	server.Start()
}

// NewServer 负责创建计划广场 RPC server，供 cmd 启动和测试复用。
func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("taskclassforum service dependency not initialized")
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
			Name: "taskclassforum.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterTaskClassForumServiceServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	return server, listenOn, nil
}
