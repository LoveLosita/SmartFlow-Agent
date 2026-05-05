package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	activeadapters "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/adapters"
	activeschedulerdao "github.com/LoveLosita/smartflow/backend/services/active_scheduler/dao"
	activeschedulerrpc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/rpc"
	activeschedulersv "github.com/LoveLosita/smartflow/backend/services/active_scheduler/sv"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	einoinfra "github.com/LoveLosita/smartflow/backend/shared/infra/eino"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := activeschedulerdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect active-scheduler database: %v", err)
	}

	aiHub, err := einoinfra.InitEino()
	if err != nil {
		log.Fatalf("failed to initialize active-scheduler Eino runtime: %v", err)
	}
	llmService := llmservice.New(llmservice.Options{
		AIHub:             aiHub,
		APIKey:            os.Getenv("ARK_API_KEY"),
		BaseURL:           viper.GetString("agent.baseURL"),
		CourseVisionModel: viper.GetString("courseImport.visionModel"),
	})

	svc, err := activeschedulersv.New(db, llmService, activeschedulersv.Options{
		JobScanEvery: viper.GetDuration("activeScheduler.jobScanEvery"),
		JobScanLimit: viper.GetInt("activeScheduler.jobScanLimit"),
		KafkaConfig:  kafkabus.LoadConfig(),
		TaskRPC: activeadapters.TaskRPCConfig{
			Endpoints: viper.GetStringSlice("task.rpc.endpoints"),
			Target:    viper.GetString("task.rpc.target"),
			Timeout:   viper.GetDuration("task.rpc.timeout"),
		},
		ScheduleRPC: activeadapters.ScheduleRPCConfig{
			Endpoints: viper.GetStringSlice("schedule.rpc.endpoints"),
			Target:    viper.GetString("schedule.rpc.target"),
			Timeout:   viper.GetDuration("schedule.rpc.timeout"),
		},
	})
	if err != nil {
		log.Fatalf("failed to initialize active-scheduler service: %v", err)
	}
	defer svc.Close()
	svc.StartWorkers(ctx)
	log.Println("Active-scheduler outbox consumer and due job scanner started")

	server, listenOn, err := activeschedulerrpc.NewServer(activeschedulerrpc.ServerOptions{
		ListenOn: viper.GetString("activeScheduler.rpc.listenOn"),
		Timeout:  viper.GetDuration("activeScheduler.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build active-scheduler zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("active-scheduler zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("active-scheduler service stopping")
}
