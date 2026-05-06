package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	tokenstoreclient "github.com/LoveLosita/smartflow/backend/client/tokenstore"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	llmdao "github.com/LoveLosita/smartflow/backend/services/llm/dao"
	llmrpc "github.com/LoveLosita/smartflow/backend/services/llm/rpc"
	creditcontracts "github.com/LoveLosita/smartflow/backend/shared/contracts/creditstore"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	einoinfra "github.com/LoveLosita/smartflow/backend/shared/infra/eino"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	redisinfra "github.com/LoveLosita/smartflow/backend/shared/infra/redis"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := llmdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect llm database: %v", err)
	}

	redisClient, err := redisinfra.OpenRedisFromConfig()
	if err != nil {
		log.Fatalf("failed to connect llm redis: %v", err)
	}
	defer redisClient.Close()

	aiHub, err := einoinfra.InitEino()
	if err != nil {
		log.Fatalf("failed to initialize llm Eino runtime: %v", err)
	}

	legacyService := llmservice.New(llmservice.Options{
		AIHub:             aiHub,
		APIKey:            os.Getenv("ARK_API_KEY"),
		BaseURL:           viper.GetString("agent.baseURL"),
		CourseVisionModel: viper.GetString("courseImport.visionModel"),
	})

	balanceSnapshotProvider := &tokenStoreSnapshotProvider{
		cfg: tokenstoreclient.ClientConfig{
			Endpoints: viper.GetStringSlice("tokenstore.rpc.endpoints"),
			Target:    viper.GetString("tokenstore.rpc.target"),
			Timeout:   viper.GetDuration("tokenstore.rpc.timeout"),
		},
	}

	outboxRepo := outboxinfra.NewRepository(db)
	priceRuleDAO := llmdao.NewPriceRuleDAO(db)
	dispatchEngine, err := buildLLMOutboxDispatchEngine(outboxRepo)
	if err != nil {
		log.Fatalf("failed to initialize llm outbox dispatch engine: %v", err)
	}
	if dispatchEngine != nil {
		dispatchEngine.StartDispatch(ctx)
		defer dispatchEngine.Close()
		log.Println("llm outbox dispatch started")
	} else {
		log.Println("llm outbox dispatch is disabled")
	}

	runtimeService, err := llmservice.NewRuntimeService(llmservice.RuntimeServiceOptions{
		LegacyService:     legacyService,
		CacheDAO:          llmdao.NewCacheDAO(redisClient),
		PriceRuleDAO:      priceRuleDAO,
		SnapshotProvider:  balanceSnapshotProvider,
		OutboxRepo:        outboxRepo,
		OutboxMaxRetry:    kafkabus.LoadConfig().MaxRetry,
		ProviderName:      viper.GetString("llm.providerName"),
		LiteModelName:     viper.GetString("agent.liteModel"),
		ProModelName:      viper.GetString("agent.proModel"),
		MaxModelName:      viper.GetString("agent.maxModel"),
		CourseVisionModel: viper.GetString("courseImport.visionModel"),
	})
	if err != nil {
		log.Fatalf("failed to initialize llm runtime service: %v", err)
	}

	server, listenOn, err := llmrpc.NewServer(llmrpc.ServerOptions{
		ListenOn: viper.GetString("llm.rpc.listenOn"),
		Timeout:  viper.GetDuration("llm.rpc.timeout"),
		Service:  runtimeService,
	})
	if err != nil {
		log.Fatalf("failed to build llm zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("llm zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("llm service stopping")
}

func buildLLMOutboxDispatchEngine(outboxRepo *outboxinfra.Repository) (*outboxinfra.Engine, error) {
	kafkaCfg := kafkabus.LoadConfig()
	if !kafkaCfg.Enabled || outboxRepo == nil {
		return nil, nil
	}

	// 1. LLM 进程这里只启动“LLM 自己 outbox 的 dispatch”，不应复用 kafka.LoadConfig() 里的全局默认 topic/group。
	// 2. 全局默认值当前仍兼容指向 agent outbox；若只改 ServiceName 不同步改 Topic/GroupID，llm.exe 会误入 agent consumer group。
	// 3. 因此这里显式绑定 llm 服务目录，确保 dispatch engine 只触达 llm_outbox_messages / smartflow.llm.outbox。
	route, _ := outboxinfra.ResolveServiceRoute(outboxinfra.ServiceLLM)
	kafkaCfg.ServiceName = route.ServiceName
	kafkaCfg.Topic = route.Topic
	kafkaCfg.GroupID = route.GroupID
	return outboxinfra.NewEngine(outboxRepo.WithRoute(route), kafkaCfg)
}

type tokenStoreSnapshotProvider struct {
	cfg tokenstoreclient.ClientConfig

	mu     sync.Mutex
	client *tokenstoreclient.Client
}

func (p *tokenStoreSnapshotProvider) GetCreditBalanceSnapshot(ctx context.Context, userID uint64) (*creditcontracts.CreditBalanceSnapshot, error) {
	client, err := p.ensureClient()
	if err != nil {
		return nil, err
	}
	return client.GetCreditBalanceSnapshot(ctx, userID)
}

func (p *tokenStoreSnapshotProvider) ensureClient() (*tokenstoreclient.Client, error) {
	if p == nil {
		return nil, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.client != nil {
		return p.client, nil
	}

	client, err := tokenstoreclient.NewClient(p.cfg)
	if err != nil {
		return nil, err
	}
	p.client = client
	return p.client, nil
}
