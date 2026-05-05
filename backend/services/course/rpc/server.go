package rpc

import (
	"errors"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/course/rpc/pb"
	coursesv "github.com/LoveLosita/smartflow/backend/services/course/sv"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn          = "0.0.0.0:9087"
	defaultTimeout           = 10 * time.Second
	defaultMaxRPCMessageSize = 8 * 1024 * 1024
	rpcMessageSizePadding    = 1024 * 1024
)

type ServerOptions struct {
	ListenOn      string
	Timeout       time.Duration
	MaxImageBytes int64
	Service       *coursesv.CourseService
}

// NewServer 创建 course zrpc 服务端。
//
// 职责边界：
// 1. 只负责 zrpc server 配置与 gRPC handler 注册；
// 2. 不创建数据库、模型客户端或业务服务，它们由 cmd/course 管理；
// 3. 图片解析走 bytes 请求，需按 maxImageBytes 抬高 gRPC 消息上限。
func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("course service dependency not initialized")
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
			Name: "course.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		pb.RegisterCourseServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}

	maxMessageSize := normalizeMaxRPCMessageSize(opts.MaxImageBytes)
	server.AddOptions(grpc.MaxRecvMsgSize(maxMessageSize), grpc.MaxSendMsgSize(maxMessageSize))
	return server, listenOn, nil
}

func normalizeMaxRPCMessageSize(maxImageBytes int64) int {
	if maxImageBytes <= 0 {
		return defaultMaxRPCMessageSize
	}
	size := maxImageBytes + rpcMessageSizePadding
	if size < defaultMaxRPCMessageSize {
		return defaultMaxRPCMessageSize
	}
	return int(size)
}
