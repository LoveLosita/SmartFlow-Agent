package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	rootdao "github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	taskdao "github.com/LoveLosita/smartflow/backend/services/task/dao"
	taskrpc "github.com/LoveLosita/smartflow/backend/services/task/rpc"
	tasksv "github.com/LoveLosita/smartflow/backend/services/task/sv"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	gormcache "github.com/LoveLosita/smartflow/backend/shared/infra/gormcache"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := taskdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect task database: %v", err)
	}
	redisClient, err := taskdao.OpenRedisFromConfig()
	if err != nil {
		log.Fatalf("failed to connect task redis: %v", err)
	}
	defer redisClient.Close()

	cacheRepo := rootdao.NewCacheDAO(redisClient)
	if err := db.Use(gormcache.NewGormCachePlugin(cacheRepo)); err != nil {
		log.Fatalf("failed to initialize task cache deleter: %v", err)
	}

	taskRepo := taskdao.NewTaskDAO(db)
	outboxRepo := outboxinfra.NewRepository(db)
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkabus.LoadConfig())
	if err != nil {
		log.Fatalf("failed to initialize task outbox bus: %v", err)
	}

	svc := tasksv.NewTaskService(taskRepo, cacheRepo, eventBus)
	// 迁移期 task 服务仍 best-effort 维护 active-scheduler due job，后续改成 RPC/事件后再移除该跨域 DAO。
	svc.SetActiveScheduleDAO(rootdao.NewActiveScheduleDAO(db))

	if eventBus != nil {
		if err := tasksv.RegisterTaskUrgencyPromoteHandler(eventBus, outboxRepo, taskRepo); err != nil {
			log.Fatalf("failed to register task outbox handler: %v", err)
		}
		eventBus.Start(ctx)
		defer eventBus.Close()
		log.Println("Task outbox consumer started")
	} else {
		log.Println("Task outbox consumer is disabled")
	}

	server, listenOn, err := taskrpc.NewServer(taskrpc.ServerOptions{
		ListenOn: viper.GetString("task.rpc.listenOn"),
		Timeout:  viper.GetDuration("task.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build task zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("task zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("task service stopping")
}
