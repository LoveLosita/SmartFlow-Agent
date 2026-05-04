package rpc

import (
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/schedule/rpc/pb"
	schedulesv "github.com/LoveLosita/smartflow/backend/services/schedule/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9084"
	defaultTimeout  = 6 * time.Second
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *schedulesv.ScheduleService
}

// NewServer 创建 schedule zrpc 服务端。
//
// 职责边界：
// 1. 只负责 zrpc server 配置与 gRPC handler 注册；
// 2. 不创建数据库、Redis 或业务服务，它们由 cmd/schedule 管理；
// 3. 返回 listenOn 供进程入口打印启动日志。
func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("schedule service dependency not initialized")
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
			Name: "schedule.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterScheduleServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	return server, listenOn, nil
}
