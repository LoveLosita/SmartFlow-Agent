package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	notificationdao "github.com/LoveLosita/smartflow/backend/services/notification/dao"
	notificationrpc "github.com/LoveLosita/smartflow/backend/services/notification/rpc"
	notificationsv "github.com/LoveLosita/smartflow/backend/services/notification/sv"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := notificationdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect notification database: %v", err)
	}

	channelDAO := notificationdao.NewChannelDAO(db)
	recordDAO := notificationdao.NewRecordDAO(db)
	svc, err := notificationsv.NewNotificationServiceWithFeishuWebhook(recordDAO, channelDAO, notificationsv.FeishuWebhookProviderOptions{
		FrontendBaseURL: viper.GetString("notification.frontendBaseURL"),
	}, notificationsv.ServiceOptions{})
	if err != nil {
		log.Fatalf("failed to initialize notification service: %v", err)
	}

	outboxRepo := outboxinfra.NewRepository(db)
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkabus.LoadConfig())
	if err != nil {
		log.Fatalf("failed to initialize notification outbox bus: %v", err)
	}
	if eventBus != nil {
		if err := notificationsv.RegisterFeishuRequestedHandler(eventBus, outboxRepo, svc); err != nil {
			log.Fatalf("failed to register notification outbox handler: %v", err)
		}
		eventBus.Start(ctx)
		defer eventBus.Close()
		log.Println("Notification outbox consumer started")
	} else {
		log.Println("Notification outbox consumer is disabled")
	}

	svc.StartRetryLoop(ctx, viper.GetDuration("notification.retryScanEvery"), viper.GetInt("notification.retryBatchSize"))
	log.Println("Notification retry scanner started")

	server, listenOn, err := notificationrpc.NewServer(notificationrpc.ServerOptions{
		ListenOn: viper.GetString("notification.rpc.listenOn"),
		Timeout:  viper.GetDuration("notification.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build notification zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("notification zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("notification service stopping")
}
