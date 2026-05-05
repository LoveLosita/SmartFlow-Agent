package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	memoryclient "github.com/LoveLosita/smartflow/backend/client/memory"
	scheduleclient "github.com/LoveLosita/smartflow/backend/client/schedule"
	taskclient "github.com/LoveLosita/smartflow/backend/client/task"
	taskclassclient "github.com/LoveLosita/smartflow/backend/client/taskclass"
	userauthclient "github.com/LoveLosita/smartflow/backend/client/userauth"
	activeadapters "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/adapters"
	activefeedbacklocate "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/feedbacklocate"
	activegraph "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/graph"
	activesel "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	activesvc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/service"
	agentsv "github.com/LoveLosita/smartflow/backend/services/agent/sv"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	"github.com/LoveLosita/smartflow/backend/services/agent/tools/web"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	memorymodule "github.com/LoveLosita/smartflow/backend/services/memory"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
	ragconfig "github.com/LoveLosita/smartflow/backend/services/rag/config"
	rootdao "github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	eventsvc "github.com/LoveLosita/smartflow/backend/services/runtime/eventsvc"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	scheduledao "github.com/LoveLosita/smartflow/backend/services/schedule/dao"
	schedulesv "github.com/LoveLosita/smartflow/backend/services/schedule/sv"
	taskdao "github.com/LoveLosita/smartflow/backend/services/task/dao"
	tasksv "github.com/LoveLosita/smartflow/backend/services/task/sv"
	einoinfra "github.com/LoveLosita/smartflow/backend/shared/infra/eino"
	gormcache "github.com/LoveLosita/smartflow/backend/shared/infra/gormcache"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	redisinfra "github.com/LoveLosita/smartflow/backend/shared/infra/redis"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

type agentRuntime struct {
	redisClient    *redis.Client
	eventBus       eventsvc.OutboxBus
	outboxRepo     *outboxinfra.Repository
	repoManager    *rootdao.RepoManager
	agentRepo      *rootdao.AgentDAO
	cacheRepo      *rootdao.CacheDAO
	userAuthClient *userauthclient.Client
	service        *agentsv.AgentService
	workersStarted bool
}

func buildAgentRuntime(ctx context.Context) (*agentRuntime, error) {
	db, err := openAgentDBFromConfig()
	if err != nil {
		return nil, fmt.Errorf("connect agent database failed: %w", err)
	}

	redisClient, err := redisinfra.OpenRedisFromConfig()
	if err != nil {
		return nil, fmt.Errorf("connect agent redis failed: %w", err)
	}
	fail := func(cause error) (*agentRuntime, error) {
		_ = redisClient.Close()
		return nil, cause
	}

	cacheRepo := rootdao.NewCacheDAO(redisClient)
	if err = db.Use(gormcache.NewGormCachePlugin(cacheRepo)); err != nil {
		return fail(fmt.Errorf("initialize agent cache deleter failed: %w", err))
	}

	// 说明：
	// 1. 本轮先在 cmd/agent 内平移一份启动装配，不直接改 cmd/start.go 的旧 gateway 本地链路。
	// 2. 这样可以把独立进程入口先稳定下来，同时避免和主代理并行接的 rpc/pb 改动发生交叉覆盖。
	// 3. 等阶段 6 的 agent/memory 启动边界都收稳后，再统一评估是否把 LLM/RAG/bootstrap 抽公共层。
	llmService, err := buildAgentLLMService()
	if err != nil {
		return fail(fmt.Errorf("initialize agent llm service failed: %w", err))
	}
	ragService, err := buildAgentRAGService(ctx)
	if err != nil {
		return fail(err)
	}
	ragRuntime := ragService.Runtime()

	memoryCfg := memorymodule.LoadConfigFromViper()
	memoryObserver := memoryobserve.NewLoggerObserver(log.Default())
	memoryMetrics := memoryobserve.NewMetricsRegistry()

	manager := rootdao.NewManager(db)
	agentRepo := rootdao.NewAgentDAO(db)
	taskRepo := rootdao.NewTaskDAO(db)
	taskServiceRepo := taskdao.NewTaskDAO(db)
	taskClassRepo := rootdao.NewTaskClassDAO(db)
	scheduleServiceRepo := scheduledao.NewScheduleDAO(db)
	agentCacheRepo := rootdao.NewAgentCache(redisClient)
	outboxRepo := outboxinfra.NewRepository(db)

	eventBus, err := buildAgentEventBus(outboxRepo)
	if err != nil {
		return fail(err)
	}
	if err = eventsvc.RegisterTaskUrgencyPromoteRoute(); err != nil {
		return fail(fmt.Errorf("register task outbox route failed: %w", err))
	}

	eventPublisher := buildAgentOutboxPublisher(outboxRepo)
	taskOutboxPublisher := buildTaskOutboxPublisher(outboxRepo)

	var userAuthClient *userauthclient.Client
	if eventBus != nil {
		userAuthClient, err = userauthclient.NewClient(userauthclient.ClientConfig{
			Endpoints: viper.GetStringSlice("userauth.rpc.endpoints"),
			Target:    viper.GetString("userauth.rpc.target"),
			Timeout:   viper.GetDuration("userauth.rpc.timeout"),
		})
		if err != nil {
			return fail(fmt.Errorf("initialize userauth zrpc client failed: %w", err))
		}
	}

	taskClient, err := taskclient.NewClient(taskclient.ClientConfig{
		Endpoints: viper.GetStringSlice("task.rpc.endpoints"),
		Target:    viper.GetString("task.rpc.target"),
		Timeout:   viper.GetDuration("task.rpc.timeout"),
	})
	if err != nil {
		return fail(fmt.Errorf("initialize task zrpc client failed: %w", err))
	}
	taskClassClient, err := taskclassclient.NewClient(taskclassclient.ClientConfig{
		Endpoints: viper.GetStringSlice("taskClass.rpc.endpoints"),
		Target:    viper.GetString("taskClass.rpc.target"),
		Timeout:   viper.GetDuration("taskClass.rpc.timeout"),
	})
	if err != nil {
		return fail(fmt.Errorf("initialize task-class zrpc client failed: %w", err))
	}
	scheduleClient, err := scheduleclient.NewClient(scheduleclient.ClientConfig{
		Endpoints: viper.GetStringSlice("schedule.rpc.endpoints"),
		Target:    viper.GetString("schedule.rpc.target"),
		Timeout:   viper.GetDuration("schedule.rpc.timeout"),
	})
	if err != nil {
		return fail(fmt.Errorf("initialize schedule zrpc client failed: %w", err))
	}
	memoryClient, err := memoryclient.NewClient(memoryclient.ClientConfig{
		Endpoints: viper.GetStringSlice("memory.rpc.endpoints"),
		Target:    viper.GetString("memory.rpc.target"),
		Timeout:   viper.GetDuration("memory.rpc.timeout"),
	})
	if err != nil {
		return fail(fmt.Errorf("initialize memory zrpc client failed: %w", err))
	}

	taskService := tasksv.NewTaskService(taskServiceRepo, cacheRepo, taskOutboxPublisher)
	taskService.SetActiveScheduleDAO(manager.ActiveSchedule)
	scheduleService := schedulesv.NewScheduleService(scheduleServiceRepo, taskClassRepo, manager, cacheRepo)
	agentService := agentsv.NewAgentService(
		llmService,
		agentRepo,
		taskRepo,
		cacheRepo,
		agentCacheRepo,
		manager.ActiveSchedule,
		manager.ActiveScheduleSession,
		eventPublisher,
	)

	// 1. 迁移期仍由独立入口注入旧 schedule/task 领域能力，避免 agent/sv 反向 import 旧 service 形成循环依赖。
	// 2. 等阶段 6 后续把这些残留 DAO 适配继续切成 RPC/read-model，再从这里移除注入点。
	agentService.SmartPlanningMultiRawFunc = scheduleService.SmartPlanningMultiRaw
	agentService.HybridScheduleWithPlanMultiFunc = scheduleService.HybridScheduleWithPlanMulti
	agentService.ResolvePlanningWindowFunc = scheduleService.ResolvePlanningWindowByTaskClasses
	agentService.GetTasksWithUrgencyPromotionFunc = taskService.GetTasksWithUrgencyPromotion

	configureAgentService(
		agentService,
		ragRuntime,
		agentRepo,
		cacheRepo,
		taskClient,
		taskClassClient,
		scheduleClient,
		memoryClient,
		memoryCfg,
		memoryObserver,
		memoryMetrics,
	)

	activeTaskAdapter, err := activeadapters.NewTaskRPCAdapter(activeadapters.TaskRPCConfig{
		Endpoints: viper.GetStringSlice("task.rpc.endpoints"),
		Target:    viper.GetString("task.rpc.target"),
		Timeout:   viper.GetDuration("task.rpc.timeout"),
	})
	if err != nil {
		return fail(fmt.Errorf("initialize task rpc adapter for agent rerun failed: %w", err))
	}
	activeScheduleAdapter, err := activeadapters.NewScheduleRPCAdapter(activeadapters.ScheduleRPCConfig{
		Endpoints: viper.GetStringSlice("schedule.rpc.endpoints"),
		Target:    viper.GetString("schedule.rpc.target"),
		Timeout:   viper.GetDuration("schedule.rpc.timeout"),
	})
	if err != nil {
		return fail(fmt.Errorf("initialize schedule rpc adapter for agent rerun failed: %w", err))
	}
	activeScheduleDryRun, err := activesvc.NewDryRunService(activeadapters.ReadersWithScheduleRPC(activeTaskAdapter, activeScheduleAdapter))
	if err != nil {
		return fail(err)
	}
	activeSchedulePreviewConfirm, err := buildActiveSchedulePreviewConfirmService(manager.ActiveSchedule, activeScheduleDryRun, activeScheduleAdapter)
	if err != nil {
		return fail(err)
	}
	activeScheduleLLMClient := llmService.ProClient()
	activeScheduleSelector := activesel.NewService(activeScheduleLLMClient)
	activeScheduleFeedbackLocator := activefeedbacklocate.NewService(activeScheduleAdapter, activeScheduleLLMClient)
	activeScheduleGraphRunner, err := activegraph.NewRunner(activeScheduleDryRun.AsGraphDryRunFunc(), activeScheduleSelector)
	if err != nil {
		return fail(err)
	}
	agentService.SetActiveScheduleSessionRerunFunc(buildActiveScheduleSessionRerunFunc(
		manager.ActiveSchedule,
		activeScheduleGraphRunner,
		activeSchedulePreviewConfirm,
		activeScheduleFeedbackLocator,
	))

	return &agentRuntime{
		redisClient:    redisClient,
		eventBus:       eventBus,
		outboxRepo:     outboxRepo,
		repoManager:    manager,
		agentRepo:      agentRepo,
		cacheRepo:      cacheRepo,
		userAuthClient: userAuthClient,
		service:        agentService,
	}, nil
}

func (r *agentRuntime) startWorkers(ctx context.Context) error {
	if r == nil || r.workersStarted {
		return nil
	}
	if r.eventBus == nil {
		log.Println("Agent outbox consumer is disabled")
		return nil
	}
	if r.userAuthClient == nil {
		return fmt.Errorf("agent outbox consumer requires userauth zrpc client")
	}

	// 1. 先登记 agent 自己消费的 handler，同时补齐 memory.extract.requested 的服务路由。
	// 2. 这里明确只接 agent 边界；memory 消费仍归 cmd/memory，task 事件仍是 publish-only 写入 task outbox。
	// 3. 注册完成后再启动总线，避免服务一起来就抢先消费到尚未挂 handler 的消息。
	if err := eventsvc.RegisterCoreOutboxHandlers(
		r.eventBus,
		r.outboxRepo,
		r.repoManager,
		r.agentRepo,
		r.cacheRepo,
		nil,
		r.userAuthClient,
	); err != nil {
		return fmt.Errorf("register agent outbox handlers failed: %w", err)
	}

	r.eventBus.Start(ctx)
	r.workersStarted = true
	log.Println("Agent outbox consumer started")
	return nil
}

func (r *agentRuntime) close() {
	if r == nil {
		return
	}
	if r.eventBus != nil {
		r.eventBus.Close()
	}
	if r.redisClient != nil {
		_ = r.redisClient.Close()
	}
}

func openAgentDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
	if err != nil {
		return nil, err
	}
	if err = autoMigrateAgentOwnedTables(db); err != nil {
		return nil, err
	}
	if err = autoMigrateAgentOutboxTable(db); err != nil {
		return nil, err
	}
	if err = ensureAgentRuntimeDependencyTables(db); err != nil {
		return nil, err
	}
	return db, nil
}

func autoMigrateAgentOwnedTables(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("agent database is not initialized")
	}

	// 1. 独立 agent 进程启动时只负责补齐自有表结构，不在历史库上强制补外键约束。
	// 2. 线上/本地历史数据可能存在旧 chat_history 记录找不到 agent_chat 的情况，硬补 FK 会阻断服务启动。
	// 3. 迁移期保留应用层按 chat_id 关联的读写语义；真正清理孤儿历史和补 FK 应走单独数据治理脚本。
	originalDisableFK := db.Config.DisableForeignKeyConstraintWhenMigrating
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	defer func() {
		db.Config.DisableForeignKeyConstraintWhenMigrating = originalDisableFK
	}()

	if err := db.AutoMigrate(
		&model.AgentChat{},
		&model.ChatHistory{},
		&model.AgentTimelineEvent{},
		&model.AgentScheduleState{},
		&model.ActiveScheduleSession{},
		&model.AgentStateSnapshotRecord{},
	); err != nil {
		return fmt.Errorf("auto migrate agent owned tables failed: %w", err)
	}
	return nil
}

func autoMigrateAgentOutboxTable(db *gorm.DB) error {
	cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceAgent)
	if !ok {
		return fmt.Errorf("resolve agent outbox config failed")
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&model.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate agent outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}

func ensureAgentRuntimeDependencyTables(db *gorm.DB) error {
	// 1. agent 独立进程当前仍复用 task/schedule/active-scheduler 的部分读写表，不在这里越权迁移这些表。
	// 2. 这里只做存在性检查，缺表时直接 fail fast，避免聊天请求进入半初始化状态。
	// 3. 等阶段 6 后续把这些直连改成 RPC/read-model 后，应同步缩减这份依赖清单。
	for _, dependency := range []struct {
		name  string
		model any
	}{
		{name: "tasks", model: &model.Task{}},
		{name: "task_classes", model: &model.TaskClass{}},
		{name: "task_items", model: &model.TaskClassItem{}},
		{name: "schedules", model: &model.Schedule{}},
		{name: "schedule_events", model: &model.ScheduleEvent{}},
		{name: "active_schedule_triggers", model: &model.ActiveScheduleTrigger{}},
		{name: "active_schedule_previews", model: &model.ActiveSchedulePreview{}},
	} {
		if !db.Migrator().HasTable(dependency.model) {
			return fmt.Errorf("agent runtime dependency table missing: %s", dependency.name)
		}
	}
	return nil
}

func buildAgentLLMService() (*llmservice.Service, error) {
	aiHub, err := einoinfra.InitEino()
	if err != nil {
		return nil, err
	}
	return llmservice.New(llmservice.Options{
		AIHub:             aiHub,
		APIKey:            os.Getenv("ARK_API_KEY"),
		BaseURL:           viper.GetString("agent.baseURL"),
		CourseVisionModel: viper.GetString("courseImport.visionModel"),
	}), nil
}

func buildAgentRAGService(ctx context.Context) (*ragservice.Service, error) {
	ragCfg := ragconfig.LoadFromViper()
	if !ragCfg.Enabled {
		log.Println("RAG service is disabled for agent")
		return ragservice.New(ragservice.Options{}), nil
	}

	ragLogger := log.Default()
	ragService, err := ragservice.NewFromConfig(ctx, ragCfg, ragservice.FactoryDeps{
		Logger:   ragLogger,
		Observer: ragservice.NewLoggerObserver(ragLogger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize agent RAG service: %w", err)
	}
	log.Printf("Agent RAG runtime initialized: store=%s embed=%s reranker=%s", ragCfg.Store, ragCfg.EmbedProvider, ragCfg.RerankerProvider)
	return ragService, nil
}

func buildAgentEventBus(outboxRepo *outboxinfra.Repository) (eventsvc.OutboxBus, error) {
	kafkaCfg := kafkabus.LoadConfig()
	bus, err := eventsvc.NewServiceOutboxBus(outboxRepo, kafkaCfg, outboxinfra.ServiceAgent)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize outbox event bus for service %s: %w", outboxinfra.ServiceAgent, err)
	}
	serviceBuses := make(map[string]eventsvc.OutboxBus, 1)
	if bus != nil {
		serviceBuses[outboxinfra.ServiceAgent] = bus
	}

	eventBus := eventsvc.NewRoutedOutboxBus(serviceBuses)
	if eventBus == nil {
		log.Println("Agent outbox event bus is disabled")
	}
	return eventBus, nil
}

func buildAgentOutboxPublisher(outboxRepo *outboxinfra.Repository) outboxinfra.EventPublisher {
	kafkaCfg := kafkabus.LoadConfig()
	if !kafkaCfg.Enabled || outboxRepo == nil {
		return nil
	}
	return &repositoryOutboxPublisher{
		repo:     outboxRepo,
		maxRetry: kafkaCfg.MaxRetry,
	}
}

func buildTaskOutboxPublisher(outboxRepo *outboxinfra.Repository) outboxinfra.EventPublisher {
	kafkaCfg := kafkabus.LoadConfig()
	if !kafkaCfg.Enabled || outboxRepo == nil {
		return nil
	}
	return &repositoryOutboxPublisher{
		repo:     outboxRepo,
		maxRetry: kafkaCfg.MaxRetry,
	}
}

type repositoryOutboxPublisher struct {
	repo     *outboxinfra.Repository
	maxRetry int
}

func (p *repositoryOutboxPublisher) Publish(ctx context.Context, req outboxinfra.PublishRequest) error {
	if p == nil || p.repo == nil {
		return fmt.Errorf("outbox publisher is not initialized")
	}

	eventType := strings.TrimSpace(req.EventType)
	if eventType == "" {
		return fmt.Errorf("eventType is empty")
	}
	eventVersion := strings.TrimSpace(req.EventVersion)
	if eventVersion == "" {
		eventVersion = outboxinfra.DefaultEventVersion
	}
	messageKey := strings.TrimSpace(req.MessageKey)
	aggregateID := strings.TrimSpace(req.AggregateID)
	if aggregateID == "" {
		aggregateID = messageKey
	}

	payloadJSON, err := json.Marshal(req.Payload)
	if err != nil {
		return err
	}

	_, err = p.repo.CreateMessage(ctx, eventType, messageKey, outboxinfra.OutboxEventPayload{
		EventID:      strings.TrimSpace(req.EventID),
		EventType:    eventType,
		EventVersion: eventVersion,
		AggregateID:  aggregateID,
		Payload:      payloadJSON,
	}, p.maxRetry)
	return err
}

func configureAgentService(
	agentService *agentsv.AgentService,
	ragRuntime ragservice.Runtime,
	agentRepo *rootdao.AgentDAO,
	cacheRepo *rootdao.CacheDAO,
	taskClient agentsv.TaskRPCClient,
	taskClassClient agentsv.TaskClassAgentRPCClient,
	scheduleClient agentsv.ScheduleAgentRPCClient,
	memoryReaderClient ports.MemoryReaderClient,
	memoryCfg memorymodel.Config,
	memoryObserver memoryobserve.Observer,
	memoryMetrics memoryobserve.MetricsRecorder,
) {
	if agentService == nil {
		return
	}

	agentService.SetAgentStateStore(rootdao.NewAgentStateStoreAdapter(cacheRepo))

	var webSearchProvider web.SearchProvider
	webProvider := viper.GetString("websearch.provider")
	switch webProvider {
	case "bocha":
		bochaKey := viper.GetString("websearch.apiKey")
		if bochaKey == "" {
			log.Println("WebSearch: 博查 API Key 为空，降级为 mock")
			webSearchProvider = &web.MockProvider{}
		} else {
			webSearchProvider = web.NewBochaProvider(bochaKey, "")
			log.Println("WebSearch provider: bocha")
		}
	case "mock", "":
		webSearchProvider = &web.MockProvider{}
		log.Println("WebSearch provider: mock（模拟模式）")
	default:
		log.Printf("WebSearch provider %q 未识别，降级为 mock", webProvider)
		webSearchProvider = &web.MockProvider{}
	}

	agentService.SetToolRegistry(agenttools.NewDefaultRegistryWithDeps(agenttools.DefaultRegistryDeps{
		RAGRuntime:        ragRuntime,
		WebSearchProvider: webSearchProvider,
		TaskClassWriteDeps: agenttools.TaskClassWriteDeps{
			UpsertTaskClass: agentsv.NewTaskClassRPCUpsertFunc(taskClassClient),
		},
	}))
	agentService.SetScheduleProvider(agentsv.NewScheduleRPCProvider(scheduleClient, taskClassClient))
	agentService.SetCompactionStore(agentRepo)
	agentService.SetQuickTaskDeps(agentsv.NewTaskRPCQuickTaskDeps(taskClient))
	agentService.SetMemoryReader(agentsv.NewMemoryRPCReader(memoryReaderClient, memoryObserver, memoryMetrics), memoryCfg)
}
