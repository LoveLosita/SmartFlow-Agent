package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	tokenstorerpc "github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc"
	tokenstoresv "github.com/LoveLosita/smartflow/backend/services/tokenstore/sv"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
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

	db, err := tokenstoredao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect tokenstore database: %v", err)
	}

	var creditCache *tokenstoredao.CreditCacheDAO
	rdb, err := tokenstoredao.OpenRedisFromConfig()
	if err != nil {
		log.Printf("tokenstore redis is unavailable, credit cache disabled: %v", err)
	} else {
		creditCache = tokenstoredao.NewCreditCacheDAO(rdb)
		log.Println("Tokenstore credit cache enabled")
	}

	svc := tokenstoresv.New(tokenstoresv.Options{
		DB:          db,
		CreditCache: creditCache,
	})

	outboxRepo := outboxinfra.NewRepository(db)
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkabus.LoadConfig())
	if err != nil {
		log.Fatalf("failed to initialize tokenstore outbox bus: %v", err)
	}
	if eventBus != nil {
		if err := tokenstoresv.RegisterForumRewardHandlers(eventBus, outboxRepo, svc); err != nil {
			log.Fatalf("failed to register tokenstore outbox handlers: %v", err)
		}
		if err := tokenstoresv.RegisterCreditChargeHandlers(eventBus, outboxRepo, svc); err != nil {
			log.Fatalf("failed to register credit charge handlers: %v", err)
		}
		eventBus.Start(ctx)
		defer eventBus.Close()
		log.Println("Tokenstore outbox consumer started")
	} else {
		log.Println("Tokenstore outbox consumer is disabled")
	}

	server, listenOn, err := tokenstorerpc.NewServer(tokenstorerpc.ServerOptions{
		ListenOn: viper.GetString("tokenstore.rpc.listenOn"),
		Timeout:  viper.GetDuration("tokenstore.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build tokenstore zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("tokenstore zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("tokenstore service stopping")
}
