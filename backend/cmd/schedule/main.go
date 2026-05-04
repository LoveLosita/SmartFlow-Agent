package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	rootdao "github.com/LoveLosita/smartflow/backend/dao"
	rootmiddleware "github.com/LoveLosita/smartflow/backend/middleware"
	"github.com/LoveLosita/smartflow/backend/services/schedule/core/applyadapter"
	scheduledao "github.com/LoveLosita/smartflow/backend/services/schedule/dao"
	schedulerpc "github.com/LoveLosita/smartflow/backend/services/schedule/rpc"
	schedulesv "github.com/LoveLosita/smartflow/backend/services/schedule/sv"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := scheduledao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect schedule database: %v", err)
	}
	redisClient, err := scheduledao.OpenRedisFromConfig()
	if err != nil {
		log.Fatalf("failed to connect schedule redis: %v", err)
	}
	defer redisClient.Close()

	cacheRepo := rootdao.NewCacheDAO(redisClient)
	if err := db.Use(rootmiddleware.NewGormCachePlugin(cacheRepo)); err != nil {
		log.Fatalf("failed to initialize schedule cache deleter: %v", err)
	}

	svc := schedulesv.NewScheduleService(
		scheduledao.NewScheduleDAO(db),
		rootdao.NewTaskClassDAO(db),
		rootdao.NewManager(db),
		cacheRepo,
	)
	svc.SetApplyAdapter(applyadapter.NewGormApplyAdapter(db))

	server, listenOn, err := schedulerpc.NewServer(schedulerpc.ServerOptions{
		ListenOn: viper.GetString("schedule.rpc.listenOn"),
		Timeout:  viper.GetDuration("schedule.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build schedule zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("schedule zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("schedule service stopping")
}
