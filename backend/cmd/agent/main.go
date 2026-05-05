package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	agentrpc "github.com/LoveLosita/smartflow/backend/services/agent/rpc"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime, err := buildAgentRuntime(ctx)
	if err != nil {
		log.Fatalf("failed to initialize agent runtime: %v", err)
	}
	defer runtime.close()

	if err := runtime.startWorkers(ctx); err != nil {
		log.Fatalf("failed to start agent workers: %v", err)
	}

	server, listenOn, err := agentrpc.NewServer(agentrpc.ServerOptions{
		ListenOn: viper.GetString("agent.rpc.listenOn"),
		Timeout:  viper.GetDuration("agent.rpc.timeout"),
		Service:  runtime.service,
	})
	if err != nil {
		log.Fatalf("failed to build agent zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("agent zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("agent service stopping")
}
