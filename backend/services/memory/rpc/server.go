package rpc

import (
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/memory/rpc/pb"
	memorysv "github.com/LoveLosita/smartflow/backend/services/memory/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9088"
	defaultTimeout  = 6 * time.Second
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *memorysv.Service
}

// NewServer 创建 memory zrpc 服务端。
//
// 职责边界：
// 1. 只负责 zrpc server 配置与 gRPC handler 注册；
// 2. 不创建数据库、LLM、RAG、outbox 或 worker，它们由 cmd/memory 管理；
// 3. 返回 listenOn 供进程入口打印启动日志。
func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("memory service dependency not initialized")
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
			Name: "memory.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterMemoryServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	return server, listenOn, nil
}
