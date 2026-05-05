package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	memorymodule "github.com/LoveLosita/smartflow/backend/services/memory"
	memorydao "github.com/LoveLosita/smartflow/backend/services/memory/dao"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	memoryrpc "github.com/LoveLosita/smartflow/backend/services/memory/rpc"
	memorysv "github.com/LoveLosita/smartflow/backend/services/memory/sv"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
	ragconfig "github.com/LoveLosita/smartflow/backend/services/rag/config"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	einoinfra "github.com/LoveLosita/smartflow/backend/shared/infra/eino"
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

	db, err := memorydao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect memory database: %v", err)
	}

	llmClient, err := buildMemoryLLMClient()
	if err != nil {
		log.Fatalf("failed to initialize memory LLM client: %v", err)
	}

	ragRuntime, err := buildMemoryRAGRuntime(ctx)
	if err != nil {
		log.Fatalf("failed to initialize memory RAG runtime: %v", err)
	}

	memoryCfg := memorymodule.LoadConfigFromViper()
	memoryObserver := memoryobserve.NewLoggerObserver(log.Default())
	memoryMetrics := memoryobserve.NewMetricsRegistry()
	module := memorymodule.NewModuleWithObserve(
		db,
		llmClient,
		ragRuntime,
		memoryCfg,
		memorymodule.ObserveDeps{
			Observer: memoryObserver,
			Metrics:  memoryMetrics,
		},
	)

	outboxRepo := outboxinfra.NewRepository(db)
	svc, err := memorysv.NewService(memorysv.Options{
		Module:      module,
		OutboxRepo:  outboxRepo,
		KafkaConfig: kafkabus.LoadConfig(),
	})
	if err != nil {
		log.Fatalf("failed to initialize memory service: %v", err)
	}
	defer svc.Close()

	svc.StartWorkers(ctx)

	server, listenOn, err := memoryrpc.NewServer(memoryrpc.ServerOptions{
		ListenOn: viper.GetString("memory.rpc.listenOn"),
		Timeout:  viper.GetDuration("memory.rpc.timeout"),
		Service:  svc,
	})
	if err != nil {
		log.Fatalf("failed to build memory zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("memory zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("memory service stopping")
}

// buildMemoryLLMClient 初始化 memory 抽取链路使用的模型客户端。
//
// 说明：
// 1. CP1 先复用既有 llm-service canonical 入口，不在 memory 服务里重建模型调用封装；
// 2. 当前启动入口与 cmd/start.go / cmd/active-scheduler 都需要 Eino 初始化，后续若出现第三处重复装配，应抽公共 bootstrap；
// 3. 返回 ProClient 是因为现有 memory.Module 只需要 llmservice.Client，不需要完整 Service。
func buildMemoryLLMClient() (*llmservice.Client, error) {
	aiHub, err := einoinfra.InitEino()
	if err != nil {
		return nil, err
	}
	llmService := llmservice.New(llmservice.Options{
		AIHub:             aiHub,
		APIKey:            os.Getenv("ARK_API_KEY"),
		BaseURL:           viper.GetString("agent.baseURL"),
		CourseVisionModel: viper.GetString("courseImport.visionModel"),
	})
	return llmService.ProClient(), nil
}

// buildMemoryRAGRuntime 初始化 memory 检索与向量同步使用的 RAG Runtime。
//
// 暂不抽公共层原因：
// 1. 本轮只迁 memory 一个能力域，避免同时调整 cmd/start.go 的既有装配路径；
// 2. RAG 的 canonical 入口已在 services/rag 内，当前函数只做启动层配置读取与日志包装；
// 3. 等 agent 服务也迁出后，再统一评估 llm/rag 启动装配的公共 bootstrap。
func buildMemoryRAGRuntime(ctx context.Context) (ragservice.Runtime, error) {
	ragCfg := ragconfig.LoadFromViper()
	if !ragCfg.Enabled {
		log.Println("RAG service is disabled for memory")
		return nil, nil
	}

	ragLogger := log.Default()
	ragService, err := ragservice.NewFromConfig(ctx, ragCfg, ragservice.FactoryDeps{
		Logger:   ragLogger,
		Observer: ragservice.NewLoggerObserver(ragLogger),
	})
	if err != nil {
		return nil, fmt.Errorf("build memory RAG service failed: %w", err)
	}
	log.Printf("Memory RAG runtime initialized: store=%s embed=%s reranker=%s", ragCfg.Store, ragCfg.EmbedProvider, ragCfg.RerankerProvider)
	return ragService.Runtime(), nil
}
