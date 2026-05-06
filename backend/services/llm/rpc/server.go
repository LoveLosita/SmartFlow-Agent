package rpc

import (
	"errors"
	"strings"
	"time"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/zrpc"
	"google.golang.org/grpc"
)

const (
	defaultListenOn = "0.0.0.0:9096"
	defaultTimeout  = 0
)

type ServerOptions struct {
	ListenOn string
	Timeout  time.Duration
	Service  *llmservice.RuntimeService
}

// NewServer 负责创建 LLM 独立进程的最小 zrpc server。
func NewServer(opts ServerOptions) (*zrpc.RpcServer, string, error) {
	if opts.Service == nil {
		return nil, "", errors.New("llm runtime service dependency not initialized")
	}

	listenOn := strings.TrimSpace(opts.ListenOn)
	if listenOn == "" {
		listenOn = defaultListenOn
	}
	timeout := opts.Timeout
	if timeout < 0 {
		timeout = defaultTimeout
	}

	server, err := zrpc.NewServer(zrpc.RpcServerConf{
		ServiceConf: service.ServiceConf{
			Name: "llm.rpc",
			Mode: service.DevMode,
		},
		ListenOn: listenOn,
		Timeout:  int64(timeout / time.Millisecond),
	}, func(grpcServer *grpc.Server) {
		RegisterLLMServer(grpcServer, NewHandler(opts.Service))
	})
	if err != nil {
		return nil, "", err
	}
	server.AddOptions(JSONCodecServerOption())
	return server, listenOn, nil
}
