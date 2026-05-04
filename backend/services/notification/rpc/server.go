package rpc

import (
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/notification/rpc/pb"
	notificationsv "github.com/LoveLosita/smartflow/backend/services/notification/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9082"
	defaultTimeout  = 6 * time.Second
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *notificationsv.Service
}

func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("notification service dependency not initialized")
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
			Name: "notification.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterNotificationServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	return server, listenOn, nil
}
