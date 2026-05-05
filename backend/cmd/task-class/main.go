package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	rootdao "github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	taskclassdao "github.com/LoveLosita/smartflow/backend/services/task_class/dao"
	taskclassrpc "github.com/LoveLosita/smartflow/backend/services/task_class/rpc"
	taskclasssv "github.com/LoveLosita/smartflow/backend/services/task_class/sv"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	gormcache "github.com/LoveLosita/smartflow/backend/shared/infra/gormcache"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := taskclassdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect task-class database: %v", err)
	}
	redisClient, err := taskclassdao.OpenRedisFromConfig()
	if err != nil {
		log.Fatalf("failed to connect task-class redis: %v", err)
	}
	defer redisClient.Close()

	cacheRepo := rootdao.NewCacheDAO(redisClient)
	if err := db.Use(gormcache.NewGormCachePlugin(cacheRepo)); err != nil {
		log.Fatalf("failed to initialize task-class cache deleter: %v", err)
	}

	// 1. task-class 自有 DAO 使用新服务目录下的实现，保持所有权边界清晰。
	// 2. scheduleRepo / RepoManager 属于迁移期桥接依赖，用来保留原 insert/apply 的本地事务语义。
	taskClassRepo := taskclassdao.NewTaskClassDAO(db)
	scheduleRepo := rootdao.NewScheduleDAO(db)
	manager := rootdao.NewManager(db)
	svc := taskclasssv.NewTaskClassService(taskClassRepo, cacheRepo, scheduleRepo, manager)

	server, listenOn, err := taskclassrpc.NewServer(taskclassrpc.ServerOptions{
		ListenOn: viper.GetString("taskClass.rpc.listenOn"),
		Timeout:  viper.GetDuration("taskClass.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build task-class zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("task-class zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("task-class service stopping")
}
