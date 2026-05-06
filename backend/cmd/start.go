package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	activeschedulerclient "github.com/LoveLosita/smartflow/backend/client/activescheduler"
	agentclient "github.com/LoveLosita/smartflow/backend/client/agent"
	courseclient "github.com/LoveLosita/smartflow/backend/client/course"
	memoryclient "github.com/LoveLosita/smartflow/backend/client/memory"
	notificationclient "github.com/LoveLosita/smartflow/backend/client/notification"
	scheduleclient "github.com/LoveLosita/smartflow/backend/client/schedule"
	taskclient "github.com/LoveLosita/smartflow/backend/client/task"
	taskclassclient "github.com/LoveLosita/smartflow/backend/client/taskclass"
	taskclassforumclient "github.com/LoveLosita/smartflow/backend/client/taskclassforum"
	tokenstoreclient "github.com/LoveLosita/smartflow/backend/client/tokenstore"
	userauthclient "github.com/LoveLosita/smartflow/backend/client/userauth"
	coreinit "github.com/LoveLosita/smartflow/backend/cmd/internal/coreinit"
	"github.com/LoveLosita/smartflow/backend/gateway/api"
	gatewayrouter "github.com/LoveLosita/smartflow/backend/gateway/router"
	activeadapters "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/adapters"
	activeapplyadapter "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/applyadapter"
	activefeedbacklocate "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/feedbacklocate"
	activegraph "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/graph"
	activepreview "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/preview"
	activesel "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	activesvc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/service"
	activeTrigger "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	agentstream "github.com/LoveLosita/smartflow/backend/services/agent/stream"
	agentsv "github.com/LoveLosita/smartflow/backend/services/agent/sv"
	agenttools "github.com/LoveLosita/smartflow/backend/services/agent/tools"
	"github.com/LoveLosita/smartflow/backend/services/agent/tools/web"
	coursedao "github.com/LoveLosita/smartflow/backend/services/course/dao"
	coursesv "github.com/LoveLosita/smartflow/backend/services/course/sv"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	"github.com/LoveLosita/smartflow/backend/services/memory"
	memorymodel "github.com/LoveLosita/smartflow/backend/services/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/services/memory/observe"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
	ragconfig "github.com/LoveLosita/smartflow/backend/services/rag/config"
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	eventsvc "github.com/LoveLosita/smartflow/backend/services/runtime/eventsvc"
	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	scheduledao "github.com/LoveLosita/smartflow/backend/services/schedule/dao"
	schedulesv "github.com/LoveLosita/smartflow/backend/services/schedule/sv"
	taskdao "github.com/LoveLosita/smartflow/backend/services/task/dao"
	tasksv "github.com/LoveLosita/smartflow/backend/services/task/sv"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	einoinfra "github.com/LoveLosita/smartflow/backend/shared/infra/eino"
	gormcache "github.com/LoveLosita/smartflow/backend/shared/infra/gormcache"
	kafkabus "github.com/LoveLosita/smartflow/backend/shared/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	ratelimit "github.com/LoveLosita/smartflow/backend/shared/infra/ratelimit"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

const (
	gatewayAgentRPCChatEnabledKey = "agent.rpc.chat.enabled"
	gatewayAgentRPCAPIEnabledKey  = "agent.rpc.api.enabled"
)

// appRuntime 承载一次进程启动所需的依赖图。
//
// 职责边界：
// 1. 只负责保存启动期已经装配好的基础设施、仓储、服务和 HTTP handler；
// 2. 不承载业务逻辑，业务仍然由 service / agent / memory 等领域模块负责；
// 3. 不决定进程角色，api / worker 由 StartAPI、StartWorker 选择启动哪些生命周期；StartAll 仅保留兼容别名。
type appRuntime struct {
	db             *gorm.DB
	redisClient    *redis.Client
	cacheRepo      *dao.CacheDAO
	agentRepo      *dao.AgentDAO
	agentCache     *dao.AgentCache
	manager        *dao.RepoManager
	outboxRepo     *outboxinfra.Repository
	limiter        *ratelimit.RateLimiter
	handlers       *api.ApiHandlers
	userAuthClient *userauthclient.Client
	forumClient    *taskclassforumclient.Client
	tokenClient    *tokenstoreclient.Client
}

// loadConfig 锻炼?
func loadConfig() error {
	return bootstrap.LoadConfig()
}

// Start 保留历史兼容入口，当前默认等价于 StartAPI。
// 1. 兼容 backend/main.go 和旧部署命令。
// 2. 不新增业务语义，只转发给 StartAPI。
// 3. 后续若全面切到独立 api/worker 启动，本入口只保留过渡兼容。
func Start() {
	StartAPI()
}

// StartAll 保留给历史入口与旧命令的兼容别名，当前语义与 StartAPI 完全一致。
// 1. cmd/all 已移除，不再作为后端本地启动标准入口。
// 2. 之所以暂时保留该函数，是为了避免仓库根兼容入口和旧脚本立刻失效。
// 3. 后续若仓库根入口一并收口，可直接删除该兼容别名。
func StartAll() {
	StartAPI()
}

// StartAPI 只启动 Gin API 和其同步依赖，不启动后台 worker。
// 这仍是迁移期的单体 API 模式，不是终态的独立网关。
func StartAPI() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime := mustBuildRuntime(ctx)
	defer runtime.close()

	runtime.startHTTP(ctx)
}

// StartWorker 保留历史 worker 入口，但阶段 6 后不再拥有 agent / memory 消费边界。
// 当前语义：
// 1. agent outbox relay / consumer 已迁到 cmd/agent；
// 2. memory worker 已迁到 cmd/memory；
// 3. 该入口仅用于兼容旧启动命令，后续可在 gateway 收口阶段删除。
func StartWorker() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runtime := mustBuildRuntime(ctx)
	defer runtime.close()

	runtime.startWorkers(ctx)
	log.Println("Worker process started")
	<-ctx.Done()
	log.Println("Worker process stopping")
}

func mustBuildRuntime(ctx context.Context) *appRuntime {
	runtime, err := buildRuntime(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize application runtime: %v", err)
	}
	return runtime
}

// buildRuntime 装配应用依赖图，但不启动 HTTP 或后台循环。
//
// 步骤说明：
// 1. 先初始化配置、数据库、Redis 等 gateway 必需基础设施；
// 2. 再构造各服务 zrpc client，并按开关决定是否装配 agent 本地 fallback；
// 3. 最后构造 HTTP handlers，供 api/all 模式按需启动；
// 4. worker 模式暂时也复用 gateway 依赖图，但不再启动 agent / memory worker。
func buildRuntime(ctx context.Context) (*appRuntime, error) {
	if err := loadConfig(); err != nil {
		return nil, err
	}

	db, err := coreinit.ConnectCoreDB()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	rdb, err := coreinit.InitCoreRedis()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}
	limiter := ratelimit.NewRateLimiter(rdb)

	// DAO 层初始化。
	cacheRepo := dao.NewCacheDAO(rdb)
	_ = db.Use(gormcache.NewGormCachePlugin(cacheRepo))

	// Service 层初始化。
	userAuthClient, err := userauthclient.NewClient(userauthclient.ClientConfig{
		Endpoints: viper.GetStringSlice("userauth.rpc.endpoints"),
		Target:    viper.GetString("userauth.rpc.target"),
		Timeout:   viper.GetDuration("userauth.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize userauth zrpc client: %w", err)
	}
	notificationClient, err := notificationclient.NewClient(notificationclient.ClientConfig{
		Endpoints: viper.GetStringSlice("notification.rpc.endpoints"),
		Target:    viper.GetString("notification.rpc.target"),
		Timeout:   viper.GetDuration("notification.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize notification zrpc client: %w", err)
	}
	forumClient, err := taskclassforumclient.NewClient(taskclassforumclient.ClientConfig{
		Endpoints: viper.GetStringSlice("taskclassforum.rpc.endpoints"),
		Target:    viper.GetString("taskclassforum.rpc.target"),
		Timeout:   viper.GetDuration("taskclassforum.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize taskclassforum zrpc client: %w", err)
	}
	tokenClient, err := tokenstoreclient.NewClient(tokenstoreclient.ClientConfig{
		Endpoints: viper.GetStringSlice("tokenstore.rpc.endpoints"),
		Target:    viper.GetString("tokenstore.rpc.target"),
		Timeout:   viper.GetDuration("tokenstore.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize tokenstore zrpc client: %w", err)
	}
	scheduleClient, err := scheduleclient.NewClient(scheduleclient.ClientConfig{
		Endpoints: viper.GetStringSlice("schedule.rpc.endpoints"),
		Target:    viper.GetString("schedule.rpc.target"),
		Timeout:   viper.GetDuration("schedule.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schedule zrpc client: %w", err)
	}
	taskClient, err := taskclient.NewClient(taskclient.ClientConfig{
		Endpoints: viper.GetStringSlice("task.rpc.endpoints"),
		Target:    viper.GetString("task.rpc.target"),
		Timeout:   viper.GetDuration("task.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize task zrpc client: %w", err)
	}
	taskClassClient, err := taskclassclient.NewClient(taskclassclient.ClientConfig{
		Endpoints: viper.GetStringSlice("taskClass.rpc.endpoints"),
		Target:    viper.GetString("taskClass.rpc.target"),
		Timeout:   viper.GetDuration("taskClass.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize task-class zrpc client: %w", err)
	}
	courseClient, err := courseclient.NewClient(courseclient.ClientConfig{
		Endpoints:     viper.GetStringSlice("course.rpc.endpoints"),
		Target:        viper.GetString("course.rpc.target"),
		Timeout:       viper.GetDuration("course.rpc.timeout"),
		MaxImageBytes: viper.GetInt64("courseImport.maxImageBytes"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize course zrpc client: %w", err)
	}
	memoryClient, err := memoryclient.NewClient(memoryclient.ClientConfig{
		Endpoints: viper.GetStringSlice("memory.rpc.endpoints"),
		Target:    viper.GetString("memory.rpc.target"),
		Timeout:   viper.GetDuration("memory.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize memory zrpc client: %w", err)
	}
	agentRPCClient, err := agentclient.NewClient(agentclient.ClientConfig{
		Endpoints: viper.GetStringSlice("agent.rpc.endpoints"),
		Target:    viper.GetString("agent.rpc.target"),
		Timeout:   viper.GetDuration("agent.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize agent zrpc client: %w", err)
	}
	activeSchedulerClient, err := activeschedulerclient.NewClient(activeschedulerclient.ClientConfig{
		Endpoints: viper.GetStringSlice("activeScheduler.rpc.endpoints"),
		Target:    viper.GetString("activeScheduler.rpc.target"),
		Timeout:   viper.GetDuration("activeScheduler.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize active-scheduler zrpc client: %w", err)
	}
	var agentRepo *dao.AgentDAO
	var agentCacheRepo *dao.AgentCache
	var manager *dao.RepoManager
	var outboxRepo *outboxinfra.Repository
	var agentService *agentsv.AgentService
	if shouldBuildGatewayAgentFallback() {
		log.Println("Gateway agent RPC fallback is enabled; building local AgentService compatibility path")

		aiHub, err := einoinfra.InitEino()
		if err != nil {
			return nil, fmt.Errorf("failed to initialize Eino: %w", err)
		}
		llmService := llmservice.New(llmservice.Options{
			AIHub:             aiHub,
			APIKey:            os.Getenv("ARK_API_KEY"),
			BaseURL:           viper.GetString("agent.baseURL"),
			CourseVisionModel: viper.GetString("courseImport.visionModel"),
		})

		ragService, err := buildRAGService(ctx)
		if err != nil {
			return nil, err
		}
		ragRuntime := ragService.Runtime()
		memoryCfg := memory.LoadConfigFromViper()
		memoryObserver := memoryobserve.NewLoggerObserver(log.Default())
		memoryMetrics := memoryobserve.NewMetricsRegistry()

		agentCacheRepo = dao.NewAgentCache(rdb)
		taskRepo := dao.NewTaskDAO(db)
		taskServiceRepo := taskdao.NewTaskDAO(db)
		taskClassRepo := dao.NewTaskClassDAO(db)
		scheduleServiceRepo := scheduledao.NewScheduleDAO(db)
		manager = dao.NewManager(db)
		agentRepo = dao.NewAgentDAO(db)
		outboxRepo = outboxinfra.NewRepository(db)

		// 1. fallback 仅用于 RPC 开关关闭时的迁移期回退，不再启动 agent outbox event bus。
		// 2. fallback 产生的事件仍写入服务级 outbox 表，由 cmd/agent / cmd/task 独立进程负责 relay / consume。
		eventPublisher := buildCoreOutboxPublisher(outboxRepo)
		if err := eventsvc.RegisterTaskUrgencyPromoteRoute(); err != nil {
			return nil, fmt.Errorf("failed to register task outbox route: %w", err)
		}
		taskOutboxPublisher := buildTaskOutboxPublisher(outboxRepo)
		taskSv := tasksv.NewTaskService(taskServiceRepo, cacheRepo, taskOutboxPublisher)
		taskSv.SetActiveScheduleDAO(manager.ActiveSchedule)
		scheduleService := schedulesv.NewScheduleService(scheduleServiceRepo, taskClassRepo, manager, cacheRepo)
		agentService = agentsv.NewAgentService(
			llmService,
			agentRepo,
			taskRepo,
			cacheRepo,
			agentCacheRepo,
			manager.ActiveSchedule,
			manager.ActiveScheduleSession,
			eventPublisher,
		)
		// 1. 仍由启动装配层注入旧 service 的排程能力，避免 agent/sv 反向 import 旧 service 形成循环依赖。
		// 2. 后续 schedule/task 完全走 RPC 后，这两个函数注入点可继续缩掉。
		agentService.SmartPlanningMultiRawFunc = scheduleService.SmartPlanningMultiRaw
		agentService.HybridScheduleWithPlanMultiFunc = scheduleService.HybridScheduleWithPlanMulti
		agentService.ResolvePlanningWindowFunc = scheduleService.ResolvePlanningWindowByTaskClasses
		agentService.GetTasksWithUrgencyPromotionFunc = taskSv.GetTasksWithUrgencyPromotion

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

		// 1. task_pool facts 已统一走 task RPC，避免聊天 rerun 继续直连 tasks 表；
		// 2. schedule facts / feedback / apply 已统一走 schedule RPC，避免聊天 rerun 继续直连 schedule 表。
		activeTaskAdapter, err := activeadapters.NewTaskRPCAdapter(activeadapters.TaskRPCConfig{
			Endpoints: viper.GetStringSlice("task.rpc.endpoints"),
			Target:    viper.GetString("task.rpc.target"),
			Timeout:   viper.GetDuration("task.rpc.timeout"),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize task rpc adapter for active-scheduler rerun: %w", err)
		}
		activeScheduleAdapter, err := activeadapters.NewScheduleRPCAdapter(activeadapters.ScheduleRPCConfig{
			Endpoints: viper.GetStringSlice("schedule.rpc.endpoints"),
			Target:    viper.GetString("schedule.rpc.target"),
			Timeout:   viper.GetDuration("schedule.rpc.timeout"),
		})
		if err != nil {
			return nil, fmt.Errorf("failed to initialize schedule rpc adapter for active-scheduler rerun: %w", err)
		}
		activeScheduleDryRun, err := activesvc.NewDryRunService(activeadapters.ReadersWithScheduleRPC(activeTaskAdapter, activeScheduleAdapter))
		if err != nil {
			return nil, err
		}
		activeSchedulePreviewConfirm, err := buildActiveSchedulePreviewConfirmService(manager.ActiveSchedule, activeScheduleDryRun, activeScheduleAdapter)
		if err != nil {
			return nil, err
		}
		// 1. 主动调度选择器单独复用 Pro 模型，LLM 失败时由 selection 层显式回退到确定性候选；
		// 2. dry-run 与 selection 通过 graph runner 串起来，避免 trigger_pipeline 再拼第二套候选逻辑。
		activeScheduleLLMClient := llmService.ProClient()
		activeScheduleSelector := activesel.NewService(activeScheduleLLMClient)
		activeScheduleFeedbackLocator := activefeedbacklocate.NewService(activeScheduleAdapter, activeScheduleLLMClient)
		activeScheduleGraphRunner, err := activegraph.NewRunner(activeScheduleDryRun.AsGraphDryRunFunc(), activeScheduleSelector)
		if err != nil {
			return nil, err
		}
		agentService.SetActiveScheduleSessionRerunFunc(buildActiveScheduleSessionRerunFunc(manager.ActiveSchedule, activeScheduleGraphRunner, activeSchedulePreviewConfirm, activeScheduleFeedbackLocator))
	} else {
		log.Println("Gateway agent local fallback is disabled; /agent HTTP routes use cmd/agent zrpc")
	}
	handlers := buildAPIHandlers(taskClient, taskClassClient, courseClient, scheduleClient, agentService, agentRPCClient, memoryClient, activeSchedulerClient, notificationClient)

	runtime := &appRuntime{
		db:          db,
		redisClient: rdb,
		cacheRepo:   cacheRepo,

		agentRepo:      agentRepo,
		agentCache:     agentCacheRepo,
		manager:        manager,
		outboxRepo:     outboxRepo,
		limiter:        limiter,
		handlers:       handlers,
		userAuthClient: userAuthClient,
		forumClient:    forumClient,
		tokenClient:    tokenClient,
	}
	return runtime, nil
}

// shouldBuildGatewayAgentFallback 判断 gateway 是否需要保留本地 AgentService 回退面。
//
// 职责边界：
// 1. 只读取启动期配置，不做运行时动态切换；
// 2. chat 或非 chat 任一 RPC 开关关闭时，保守装配 fallback，避免旧环境无法启动；
// 3. 两个开关都开启时跳过本地 agent 编排依赖，让 gateway 只保留 HTTP/SSE 门面。
func shouldBuildGatewayAgentFallback() bool {
	return !viper.GetBool(gatewayAgentRPCChatEnabledKey) || !viper.GetBool(gatewayAgentRPCAPIEnabledKey)
}

func buildRAGService(ctx context.Context) (*ragservice.Service, error) {
	ragCfg := ragconfig.LoadFromViper()
	if !ragCfg.Enabled {
		log.Println("RAG service is disabled")
		return ragservice.New(ragservice.Options{}), nil
	}

	// 1. 当前项目尚未完成全局观测平台建设，这里先注入一层轻量 Observer；
	// 2. RAG 内部只依赖 Observer 接口，后续若全项目统一日志/指标系统，只需替换这里；
	// 3. 这样可以避免 RAG 单独自建一套割裂的日志基础设施。
	ragLogger := log.Default()
	ragService, err := ragservice.NewFromConfig(ctx, ragCfg, ragservice.FactoryDeps{
		Logger:   ragLogger,
		Observer: ragservice.NewLoggerObserver(ragLogger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize RAG service: %w", err)
	}
	log.Printf("RAG service initialized: store=%s embed=%s reranker=%s", ragCfg.Store, ragCfg.EmbedProvider, ragCfg.RerankerProvider)
	return ragService, nil
}

// buildCoreOutboxPublisher 构造单体残留发布器。
//
// 职责边界：
// 1. 只负责把 agent 主链路产生的跨服务事件写入对应服务 outbox 表；
// 2. 不创建 memory consumer / relay，memory 消费边界已迁往 cmd/memory；
// 3. kafka.enabled=false 时返回 nil，让聊天历史继续走同步 DB fallback。
func buildCoreOutboxPublisher(outboxRepo *outboxinfra.Repository) outboxinfra.EventPublisher {
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

// buildTaskOutboxPublisher 构造单体残留 task 查询链路的发布器。
//
// 职责边界：
// 1. 只负责把 Agent 残留 TaskService 产生的 task 事件写入 task_outbox_messages；
// 2. 不创建 task consumer / relay，消费边界仍归 cmd/task；
// 3. kafka.enabled=false 时返回 nil，保持本地降级语义与旧 eventBus 一致。
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

// Publish 以 publish-only 方式写入服务级 outbox。
//
// 说明：
// 1. 这里不复用 outbox EventBus，是因为 EventBus 会创建并可能启动对应 service engine；
// 2. 单体残留在 task / memory 等迁移期只允许发布跨服务事件，不允许抢对应 consumer group；
// 3. payload 仍包装成统一 OutboxEventPayload，确保独立服务 relay / consumer 能按标准协议解析。
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

func buildCourseService(llmService *llmservice.Service, courseRepo *coursedao.CourseDAO, scheduleRepo *dao.ScheduleDAO) *coursesv.CourseService {
	courseImageResponsesClient := llmService.CourseImageResponsesClient()
	return coursesv.NewCourseService(
		courseRepo,
		scheduleRepo,
		courseImageResponsesClient,
		coursesv.NewCourseImageParseConfig(
			viper.GetInt64("courseImport.maxImageBytes"),
			viper.GetInt("courseImport.maxTokens"),
		),
		viper.GetString("courseImport.visionModel"),
	)
}

func buildActiveSchedulePreviewConfirmService(activeDAO *dao.ActiveScheduleDAO, dryRun *activesvc.DryRunService, scheduleApplyAdapter interface {
	ApplyActiveScheduleChanges(context.Context, activeapplyadapter.ApplyActiveScheduleRequest) (activeapplyadapter.ApplyActiveScheduleResult, error)
}) (*activesvc.PreviewConfirmService, error) {
	previewService, err := activepreview.NewService(activeDAO)
	if err != nil {
		return nil, err
	}
	return activesvc.NewPreviewConfirmService(dryRun, previewService, activeDAO, scheduleApplyAdapter)
}

// buildActiveScheduleSessionRerunFunc 把主动调度定位器 / graph / preview 能力装成聊天入口可调用的 rerun 闭包。
//
// 说明：
// 1. 这里只做最小接线：复用现有定位器 -> trigger -> graph -> preview 组件，不把 worker/notification 再搬一遍；
// 2. 成功时返回 session 状态、assistant 文本和业务卡片数据；
// 3. 失败时直接把 error 交回聊天入口，由上层统一写失败日志和 SSE 错误。
func buildActiveScheduleSessionRerunFunc(
	activeDAO *dao.ActiveScheduleDAO,
	graphRunner *activegraph.Runner,
	previewConfirm *activesvc.PreviewConfirmService,
	feedbackLocator *activefeedbacklocate.Service,
) agentsv.ActiveScheduleSessionRerunFunc {
	return func(
		ctx context.Context,
		session *model.ActiveScheduleSessionSnapshot,
		userMessage string,
		traceID string,
		requestStart time.Time,
	) (*agentsv.ActiveScheduleSessionRerunResult, error) {
		if activeDAO == nil || graphRunner == nil || previewConfirm == nil {
			return nil, fmt.Errorf("主动调度 rerun 依赖未初始化")
		}
		if session == nil {
			return nil, fmt.Errorf("主动调度 session 不能为空")
		}

		triggerRow, err := activeDAO.GetTriggerByID(ctx, session.TriggerID)
		if err != nil {
			return nil, err
		}
		resolvedTargetType := activeTrigger.TargetType(triggerRow.TargetType)
		resolvedTargetID := triggerRow.TargetID
		needsFeedbackLocate := activeTrigger.TriggerType(triggerRow.TriggerType) == activeTrigger.TriggerTypeUnfinishedFeedback &&
			(resolvedTargetID <= 0 || containsString(session.State.MissingInfo, "feedback_target"))

		// 1. unfinished_feedback 在目标缺失时先走定位器，把用户补充信息转成可校验的 schedule_event。
		// 2. 定位失败时直接 ask_user，不硬猜 target_id，也不继续跑 graph。
		// 3. 定位成功后只改本次 domainTrigger 的 target_type / target_id，不写正式日程。
		if needsFeedbackLocate {
			if feedbackLocator == nil {
				question := firstNonEmptyString(
					activefeedbacklocate.BuildAskUserQuestion(session.State.MissingInfo),
					session.State.PendingQuestion,
				)
				nextState := session.State
				nextState.PendingQuestion = question
				nextState.MissingInfo = appendMissingString(nextState.MissingInfo, "feedback_target")
				nextState.LastCandidateID = ""
				nextState.LastNotificationID = ""
				nextState.FailedReason = ""
				nextState.ExpiresAt = nil
				return &agentsv.ActiveScheduleSessionRerunResult{
					AssistantText: question,
					SessionState:  nextState,
					SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
				}, nil
			}
			locateResult, locateErr := feedbackLocator.Resolve(ctx, activefeedbacklocate.Request{
				UserID:          triggerRow.UserID,
				UserMessage:     userMessage,
				PendingQuestion: session.State.PendingQuestion,
				MissingInfo:     cloneStringSlice(session.State.MissingInfo),
			})
			if locateErr != nil {
				return nil, locateErr
			}
			if locateResult.ShouldAskUser() {
				question := firstNonEmptyString(
					locateResult.AskUserQuestion,
					activefeedbacklocate.BuildAskUserQuestion(session.State.MissingInfo),
					session.State.PendingQuestion,
				)
				nextState := session.State
				nextState.PendingQuestion = question
				nextState.MissingInfo = appendMissingString(nextState.MissingInfo, "feedback_target")
				nextState.LastCandidateID = ""
				nextState.LastNotificationID = ""
				nextState.FailedReason = ""
				nextState.ExpiresAt = nil
				return &agentsv.ActiveScheduleSessionRerunResult{
					AssistantText: question,
					SessionState:  nextState,
					SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
				}, nil
			}
			resolvedTargetType = activeTrigger.TargetType(locateResult.TargetType)
			resolvedTargetID = locateResult.TargetID
		}

		// 1. 定位完成后再构造 domainTrigger，避免 unfinished_feedback 的 target_id 为空时误触校验失败。
		// 2. 这里仍然复用现有 graph -> preview 链路，不写新排程引擎。
		domainTrigger := activeTrigger.ActiveScheduleTrigger{
			TriggerID:      triggerRow.ID,
			UserID:         triggerRow.UserID,
			TriggerType:    activeTrigger.TriggerType(triggerRow.TriggerType),
			Source:         activeTrigger.SourceUserFeedback,
			TargetType:     resolvedTargetType,
			TargetID:       resolvedTargetID,
			FeedbackID:     triggerRow.FeedbackID,
			IdempotencyKey: triggerRow.IdempotencyKey,
			MockNow:        nil,
			IsMockTime:     false,
			RequestedAt:    requestStart,
			TraceID:        traceID,
		}
		if err := domainTrigger.Validate(); err != nil {
			return nil, err
		}

		graphResult, err := graphRunner.Run(ctx, domainTrigger)
		if err != nil {
			return nil, err
		}
		if graphResult == nil || graphResult.DryRunData == nil || graphResult.DryRunData.Context == nil {
			return nil, fmt.Errorf("主动调度 graph 返回空结果")
		}

		selectionResult := graphResult.SelectionResult
		state := session.State
		state.LastCandidateID = strings.TrimSpace(selectionResult.SelectedCandidateID)
		state.LastNotificationID = ""
		state.FailedReason = ""
		state.MissingInfo = cloneStringSlice(graphResult.DryRunData.Context.DerivedFacts.MissingInfo)

		switch selectionResult.Action {
		case activesel.ActionSelectCandidate:
			if !graphResult.DryRunData.Observation.Decision.ShouldWritePreview {
				return nil, fmt.Errorf("主动调度 graph 选择了候选，但未产出可写 preview")
			}
			previewResp, err := previewConfirm.CreatePreviewFromDryRun(ctx, activepreview.CreatePreviewRequest{
				ActiveContext:       graphResult.DryRunData.Context,
				Observation:         graphResult.DryRunData.Observation,
				Candidates:          graphResult.DryRunData.Candidates,
				TriggerID:           triggerRow.ID,
				GeneratedAt:         requestStart,
				SelectedCandidateID: selectionResult.SelectedCandidateID,
				ExplanationText:     selectionResult.ExplanationText,
				NotificationSummary: selectionResult.NotificationSummary,
				FallbackUsed:        selectionResult.FallbackUsed,
			})
			if err != nil {
				return nil, err
			}
			state.PendingQuestion = ""
			state.MissingInfo = nil
			state.FailedReason = ""
			expiresAt := previewResp.Detail.ExpiresAt
			state.ExpiresAt = &expiresAt

			return &agentsv.ActiveScheduleSessionRerunResult{
				AssistantText: firstNonEmptyString(selectionResult.ExplanationText, selectionResult.NotificationSummary, previewResp.Detail.Explanation, previewResp.Detail.Notification, "主动调度建议已更新。"),
				BusinessCard: &agentstream.StreamBusinessCardExtra{
					CardType: "active_schedule_preview",
					Title:    "SmartFlow 日程调整建议",
					Summary:  firstNonEmptyString(selectionResult.NotificationSummary, previewResp.Detail.Notification, previewResp.Detail.Explanation),
					Data:     previewDetailToMap(previewResp.Detail),
				},
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusReadyPreview,
				PreviewID:     previewResp.Detail.PreviewID,
			}, nil

		case activesel.ActionAskUser:
			question := firstNonEmptyString(selectionResult.AskUserQuestion, selectionResult.ExplanationText, "请继续补充主动调度需要的信息。")
			state.PendingQuestion = question
			state.ExpiresAt = nil
			return &agentsv.ActiveScheduleSessionRerunResult{
				AssistantText: question,
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
			}, nil

		default:
			assistantText := firstNonEmptyString(selectionResult.ExplanationText, selectionResult.NotificationSummary, "当前主动调度暂时没有需要继续处理的内容。")
			state.PendingQuestion = ""
			state.MissingInfo = nil
			state.ExpiresAt = nil
			return &agentsv.ActiveScheduleSessionRerunResult{
				AssistantText: assistantText,
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusIgnored,
			}, nil
		}
	}
}

// previewDetailToMap 将 active_schedule preview 详情转成通用 map，供 timeline business_card 直接复用。
func previewDetailToMap(detail activepreview.ActiveSchedulePreviewDetail) map[string]any {
	raw, err := json.Marshal(detail)
	if err != nil {
		return map[string]any{}
	}
	var output map[string]any
	if err := json.Unmarshal(raw, &output); err != nil {
		return map[string]any{}
	}
	return output
}

// firstNonEmptyString 负责在一组候选文本里挑出第一条可展示内容。
func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// cloneStringSlice 负责复制 string 切片，避免直接复用底层数组被后续修改。
func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	copied := make([]string, len(values))
	copy(copied, values)
	return copied
}

// appendMissingString 负责把缺失字段名补回状态数组，避免 ask_user 分支把原始缺失项冲掉。
func appendMissingString(values []string, next string) []string {
	trimmed := strings.TrimSpace(next)
	if trimmed == "" {
		return cloneStringSlice(values)
	}
	for _, value := range values {
		if strings.TrimSpace(value) == trimmed {
			return cloneStringSlice(values)
		}
	}
	result := cloneStringSlice(values)
	return append(result, trimmed)
}

// containsString 负责判断 missing_info 里是否已经标记过某个缺失项。
func containsString(values []string, target string) bool {
	trimmed := strings.TrimSpace(target)
	if trimmed == "" {
		return false
	}
	for _, value := range values {
		if strings.TrimSpace(value) == trimmed {
			return true
		}
	}
	return false
}

func configureAgentService(
	agentService *agentsv.AgentService,
	ragRuntime ragservice.Runtime,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
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

	// agent 依赖接线。
	agentService.SetAgentStateStore(dao.NewAgentStateStoreAdapter(cacheRepo))

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
		// 未识别的 provider 类型降级为 mock 并输出警告。
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
	// 1. quick task 创建 / 查询统一走 task zrpc，避免 agent 工具链继续直连 tasks 表；
	// 2. task-class upsert 与 schedule provider 已在 CP5 统一切到 task-class/schedule zrpc；
	// 3. task 服务不可用时由 quick_task 节点返回轻量失败文案，不影响 agent 其它分支。
	agentService.SetQuickTaskDeps(agentsv.NewTaskRPCQuickTaskDeps(taskClient))
	// 1. agent 主链路读取记忆统一走 memory zrpc，避免 CP3 后继续直连本进程 memory.Module；
	// 2. observer / metrics 继续复用启动期装配，保证注入侧观测在 RPC 切流后不丢；
	// 3. gateway 不再组装 memory.Module，memory worker / 管理能力统一交给 cmd/memory；
	// 4. memory 服务暂不可用时，预取链路只记录警告并软降级，不阻断聊天主流程。
	agentService.SetMemoryReader(agentsv.NewMemoryRPCReader(memoryReaderClient, memoryObserver, memoryMetrics), memoryCfg)
}

func buildAPIHandlers(
	taskClient ports.TaskCommandClient,
	taskClassClient ports.TaskClassCommandClient,
	courseClient ports.CourseCommandClient,
	scheduleClient ports.ScheduleCommandClient,
	agentService *agentsv.AgentService,
	agentRPCClient *agentclient.Client,
	memoryClient ports.MemoryCommandClient,
	activeSchedulerClient ports.ActiveSchedulerCommandClient,
	notificationClient ports.NotificationCommandClient,
) *api.ApiHandlers {
	return &api.ApiHandlers{
		TaskHandler:      api.NewTaskHandler(taskClient),
		TaskClassHandler: api.NewTaskClassHandler(taskClassClient),
		CourseHandler:    api.NewCourseHandler(courseClient),
		ScheduleHandler:  api.NewScheduleAPI(scheduleClient),
		AgentHandler:     api.NewAgentHandlerWithRPC(agentService, agentRPCClient),
		MemoryHandler:    api.NewMemoryHandler(memoryClient),
		ActiveSchedule:   api.NewActiveScheduleAPI(activeSchedulerClient),
		Notification:     api.NewNotificationAPI(notificationClient),
	}
}

func (r *appRuntime) startWorkers(ctx context.Context) {
	if r == nil {
		return
	}

	log.Println("Gateway outbox worker is disabled; agent relay/consumer is managed by cmd/agent")
	log.Println("Memory worker is managed by cmd/memory in phase 6")
}

func (r *appRuntime) startHTTP(ctx context.Context) {
	router := gatewayrouter.RegisterRouters(r.handlers, r.userAuthClient, r.forumClient, r.tokenClient, r.cacheRepo, r.limiter)
	gatewayrouter.StartEngine(ctx, router)
}

func (r *appRuntime) close() {
	if r == nil {
		return
	}
}
