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

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/gateway/api"
	gatewayactivescheduler "github.com/LoveLosita/smartflow/backend/gateway/client/activescheduler"
	gatewaycourse "github.com/LoveLosita/smartflow/backend/gateway/client/course"
	gatewaymemory "github.com/LoveLosita/smartflow/backend/gateway/client/memory"
	gatewaynotification "github.com/LoveLosita/smartflow/backend/gateway/client/notification"
	gatewayschedule "github.com/LoveLosita/smartflow/backend/gateway/client/schedule"
	gatewaytask "github.com/LoveLosita/smartflow/backend/gateway/client/task"
	gatewaytaskclass "github.com/LoveLosita/smartflow/backend/gateway/client/taskclass"
	gatewayuserauth "github.com/LoveLosita/smartflow/backend/gateway/client/userauth"
	gatewayrouter "github.com/LoveLosita/smartflow/backend/gateway/router"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/memory"
	memorymodel "github.com/LoveLosita/smartflow/backend/memory/model"
	memoryobserve "github.com/LoveLosita/smartflow/backend/memory/observe"
	"github.com/LoveLosita/smartflow/backend/middleware"
	"github.com/LoveLosita/smartflow/backend/model"
	newagentconv "github.com/LoveLosita/smartflow/backend/newAgent/conv"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagentstream "github.com/LoveLosita/smartflow/backend/newAgent/stream"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/web"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/service"
	agentsvcsvc "github.com/LoveLosita/smartflow/backend/service/agentsvc"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
	activeadapters "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/adapters"
	activeapplyadapter "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/applyadapter"
	activefeedbacklocate "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/feedbacklocate"
	activegraph "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/graph"
	activepreview "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/preview"
	activesel "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/selection"
	activesvc "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/service"
	activeTrigger "github.com/LoveLosita/smartflow/backend/services/active_scheduler/core/trigger"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	ragservice "github.com/LoveLosita/smartflow/backend/services/rag"
	ragconfig "github.com/LoveLosita/smartflow/backend/services/rag/config"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/gorm"
)

// appRuntime 承载一次进程启动所需的依赖图。
//
// 职责边界：
// 1. 只负责保存启动期已经装配好的基础设施、仓储、服务和 HTTP handler；
// 2. 不承载业务逻辑，业务仍然由 service / newAgent / memory 等领域模块负责；
// 3. 不决定进程角色，api / worker / all 由 StartAPI、StartWorker、StartAll 选择启动哪些生命周期。
type appRuntime struct {
	db             *gorm.DB
	redisClient    *redis.Client
	cacheRepo      *dao.CacheDAO
	agentRepo      *dao.AgentDAO
	agentCache     *dao.AgentCache
	manager        *dao.RepoManager
	outboxRepo     *outboxinfra.Repository
	eventBus       eventsvc.OutboxBus
	memoryModule   *memory.Module
	limiter        *pkg.RateLimiter
	handlers       *api.ApiHandlers
	userAuthClient *gatewayuserauth.Client
}

// loadConfig 锻炼?
func loadConfig() error {
	return bootstrap.LoadConfig()
}

// Start 保留历史兼容入口，当前默认等价于 StartAll。
// 1. 兼容 backend/main.go 和旧部署命令。
// 2. 不新增业务语义，只转发给 StartAll。
// 3. 后续若全面切到独立 api/worker 启动，本入口只保留过渡兼容。
func Start() {
	StartAll()
}

// StartAll 启动当前仓库的完整运行态：HTTP API + 后台 worker。
// 这仍然是迁移期的兼容装配，不是终态的“一个服务一个 main.go”模型。
func StartAll() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runtime := mustBuildRuntime(ctx)
	defer runtime.close()

	runtime.startWorkers(ctx)
	runtime.startHTTP(ctx)
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

// StartWorker 只启动后台异步能力，不注册 Gin 路由。
// 当前只包含单体残留域 agent outbox relay / Kafka consumer；memory worker 已迁到 cmd/memory。
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
// 1. 先初始化配置、数据库、Redis、模型、RAG、memory 等基础设施；
// 2. 再构造 DAO / Service / newAgent 依赖；
// 3. 最后构造 HTTP handlers，供 api/all 模式按需启动；
// 4. worker 模式暂时也复用完整依赖图，避免同轮迁移拆出两套装配逻辑。
func buildRuntime(ctx context.Context) (*appRuntime, error) {
	if err := loadConfig(); err != nil {
		return nil, err
	}

	db, err := inits.ConnectCoreDB()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	rdb, err := inits.InitCoreRedis()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}
	limiter := pkg.NewRateLimiter(rdb)

	aiHub, err := inits.InitEino()
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
	memoryModule := memory.NewModuleWithObserve(
		db,
		llmService.ProClient(),
		ragRuntime,
		memoryCfg,
		memory.ObserveDeps{
			Observer: memoryObserver,
			Metrics:  memoryMetrics,
		},
	)

	// DAO 层初始化。
	cacheRepo := dao.NewCacheDAO(rdb)
	agentCacheRepo := dao.NewAgentCache(rdb)
	_ = db.Use(middleware.NewGormCachePlugin(cacheRepo))
	taskRepo := dao.NewTaskDAO(db)
	taskClassRepo := dao.NewTaskClassDAO(db)
	scheduleRepo := dao.NewScheduleDAO(db)
	manager := dao.NewManager(db)
	agentRepo := dao.NewAgentDAO(db)
	outboxRepo := outboxinfra.NewRepository(db)

	eventBus, err := buildAgentEventBus(outboxRepo)
	if err != nil {
		return nil, err
	}
	eventPublisher := buildCoreOutboxPublisher(outboxRepo)

	// Service 层初始化。
	userAuthClient, err := gatewayuserauth.NewClient(gatewayuserauth.ClientConfig{
		Endpoints: viper.GetStringSlice("userauth.rpc.endpoints"),
		Target:    viper.GetString("userauth.rpc.target"),
		Timeout:   viper.GetDuration("userauth.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize userauth zrpc client: %w", err)
	}
	notificationClient, err := gatewaynotification.NewClient(gatewaynotification.ClientConfig{
		Endpoints: viper.GetStringSlice("notification.rpc.endpoints"),
		Target:    viper.GetString("notification.rpc.target"),
		Timeout:   viper.GetDuration("notification.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize notification zrpc client: %w", err)
	}
	scheduleClient, err := gatewayschedule.NewClient(gatewayschedule.ClientConfig{
		Endpoints: viper.GetStringSlice("schedule.rpc.endpoints"),
		Target:    viper.GetString("schedule.rpc.target"),
		Timeout:   viper.GetDuration("schedule.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize schedule zrpc client: %w", err)
	}
	taskClient, err := gatewaytask.NewClient(gatewaytask.ClientConfig{
		Endpoints: viper.GetStringSlice("task.rpc.endpoints"),
		Target:    viper.GetString("task.rpc.target"),
		Timeout:   viper.GetDuration("task.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize task zrpc client: %w", err)
	}
	taskClassClient, err := gatewaytaskclass.NewClient(gatewaytaskclass.ClientConfig{
		Endpoints: viper.GetStringSlice("taskClass.rpc.endpoints"),
		Target:    viper.GetString("taskClass.rpc.target"),
		Timeout:   viper.GetDuration("taskClass.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize task-class zrpc client: %w", err)
	}
	courseClient, err := gatewaycourse.NewClient(gatewaycourse.ClientConfig{
		Endpoints:     viper.GetStringSlice("course.rpc.endpoints"),
		Target:        viper.GetString("course.rpc.target"),
		Timeout:       viper.GetDuration("course.rpc.timeout"),
		MaxImageBytes: viper.GetInt64("courseImport.maxImageBytes"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize course zrpc client: %w", err)
	}
	memoryClient, err := gatewaymemory.NewClient(gatewaymemory.ClientConfig{
		Endpoints: viper.GetStringSlice("memory.rpc.endpoints"),
		Target:    viper.GetString("memory.rpc.target"),
		Timeout:   viper.GetDuration("memory.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize memory zrpc client: %w", err)
	}
	activeSchedulerClient, err := gatewayactivescheduler.NewClient(gatewayactivescheduler.ClientConfig{
		Endpoints: viper.GetStringSlice("activeScheduler.rpc.endpoints"),
		Target:    viper.GetString("activeScheduler.rpc.target"),
		Timeout:   viper.GetDuration("activeScheduler.rpc.timeout"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize active-scheduler zrpc client: %w", err)
	}
	if err := eventsvc.RegisterTaskUrgencyPromoteRoute(); err != nil {
		return nil, fmt.Errorf("failed to register task outbox route: %w", err)
	}
	taskOutboxPublisher := buildTaskOutboxPublisher(outboxRepo)
	taskSv := service.NewTaskService(taskRepo, cacheRepo, taskOutboxPublisher)
	taskSv.SetActiveScheduleDAO(manager.ActiveSchedule)
	scheduleService := service.NewScheduleService(scheduleRepo, taskClassRepo, manager, cacheRepo)
	agentService := service.NewAgentServiceWithSchedule(
		llmService,
		agentRepo,
		taskRepo,
		cacheRepo,
		agentCacheRepo,
		manager.ActiveSchedule,
		manager.ActiveScheduleSession,
		eventPublisher,
		scheduleService,
		taskSv,
	)

	configureAgentService(
		agentService,
		ragRuntime,
		agentRepo,
		cacheRepo,
		taskRepo,
		taskClassRepo,
		scheduleRepo,
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
	handlers := buildAPIHandlers(taskClient, taskClassClient, courseClient, scheduleClient, agentService, memoryClient, activeSchedulerClient, notificationClient)

	runtime := &appRuntime{
		db:          db,
		redisClient: rdb,
		cacheRepo:   cacheRepo,

		agentRepo:      agentRepo,
		agentCache:     agentCacheRepo,
		manager:        manager,
		outboxRepo:     outboxRepo,
		eventBus:       eventBus,
		memoryModule:   memoryModule,
		limiter:        limiter,
		handlers:       handlers,
		userAuthClient: userAuthClient,
	}
	if runtime.eventBus != nil {
		if err := runtime.registerEventHandlers(); err != nil {
			return nil, err
		}
	}
	return runtime, nil
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

func buildAgentEventBus(outboxRepo *outboxinfra.Repository) (eventsvc.OutboxBus, error) {
	// agent outbox 消费边界装配：
	// 1. 单体残留在 CP1 后只消费 agent 自己的 outbox；
	// 2. memory.extract.requested 仍可被发布到 memory_outbox_messages，但消费与 worker 已迁往 cmd/memory；
	// 3. kafka.enabled=false 时返回 nil，业务按既有同步降级策略执行。
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
		log.Println("Outbox event bus is disabled")
	}
	return eventBus, nil
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

func buildCourseService(llmService *llmservice.Service, courseRepo *dao.CourseDAO, scheduleRepo *dao.ScheduleDAO) *service.CourseService {
	courseImageResponsesClient := llmService.CourseImageResponsesClient()
	return service.NewCourseService(
		courseRepo,
		scheduleRepo,
		courseImageResponsesClient,
		service.NewCourseImageParseConfig(
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
) agentsvcsvc.ActiveScheduleSessionRerunFunc {
	return func(
		ctx context.Context,
		session *model.ActiveScheduleSessionSnapshot,
		userMessage string,
		traceID string,
		requestStart time.Time,
	) (*agentsvcsvc.ActiveScheduleSessionRerunResult, error) {
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
				return &agentsvcsvc.ActiveScheduleSessionRerunResult{
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
				return &agentsvcsvc.ActiveScheduleSessionRerunResult{
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

			return &agentsvcsvc.ActiveScheduleSessionRerunResult{
				AssistantText: firstNonEmptyString(selectionResult.ExplanationText, selectionResult.NotificationSummary, previewResp.Detail.Explanation, previewResp.Detail.Notification, "主动调度建议已更新。"),
				BusinessCard: &newagentstream.StreamBusinessCardExtra{
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
			return &agentsvcsvc.ActiveScheduleSessionRerunResult{
				AssistantText: question,
				SessionState:  state,
				SessionStatus: model.ActiveScheduleSessionStatusWaitingUserReply,
			}, nil

		default:
			assistantText := firstNonEmptyString(selectionResult.ExplanationText, selectionResult.NotificationSummary, "当前主动调度暂时没有需要继续处理的内容。")
			state.PendingQuestion = ""
			state.MissingInfo = nil
			state.ExpiresAt = nil
			return &agentsvcsvc.ActiveScheduleSessionRerunResult{
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
	agentService *service.AgentService,
	ragRuntime ragservice.Runtime,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	taskRepo *dao.TaskDAO,
	taskClassRepo *dao.TaskClassDAO,
	scheduleRepo *dao.ScheduleDAO,
	memoryReaderClient ports.MemoryReaderClient,
	memoryCfg memorymodel.Config,
	memoryObserver memoryobserve.Observer,
	memoryMetrics memoryobserve.MetricsRecorder,
) {
	if agentService == nil {
		return
	}

	// newAgent 依赖接线。
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

	agentService.SetToolRegistry(newagenttools.NewDefaultRegistryWithDeps(newagenttools.DefaultRegistryDeps{
		RAGRuntime:        ragRuntime,
		WebSearchProvider: webSearchProvider,
		TaskClassWriteDeps: newagenttools.TaskClassWriteDeps{
			UpsertTaskClass: buildTaskClassUpsertFunc(taskClassRepo),
		},
	}))
	agentService.SetScheduleProvider(newagentconv.NewScheduleProvider(scheduleRepo, taskClassRepo))
	agentService.SetCompactionStore(agentRepo)
	agentService.SetQuickTaskDeps(newagentmodel.QuickTaskDeps{
		CreateTask: buildQuickTaskCreateFunc(taskRepo),
		QueryTasks: buildQuickTaskQueryFunc(agentService),
	})
	// 1. agent 主链路读取记忆统一走 memory zrpc，避免 CP3 后继续直连本进程 memory.Module；
	// 2. observer / metrics 继续复用启动期装配，保证注入侧观测在 RPC 切流后不丢；
	// 3. 旧 memoryModule 仍保留在启动图中，作为迁移期依赖和后续回退面；
	// 4. memory 服务暂不可用时，预取链路只记录警告并软降级，不阻断聊天主流程。
	agentService.SetMemoryReader(agentsvcsvc.NewMemoryRPCReader(memoryReaderClient, memoryObserver, memoryMetrics), memoryCfg)
}

func buildTaskClassUpsertFunc(taskClassRepo *dao.TaskClassDAO) func(userID int, input newagenttools.TaskClassUpsertInput) (newagenttools.TaskClassUpsertPersistResult, error) {
	return func(userID int, input newagenttools.TaskClassUpsertInput) (newagenttools.TaskClassUpsertPersistResult, error) {
		req := input.Request
		taskClassID := 0
		created := input.ID == 0

		err := taskClassRepo.Transaction(func(txDAO *dao.TaskClassDAO) error {
			// 1. 先构造任务类主体，保持与现有 AddOrUpdateTaskClass 口径一致。
			taskClass := &model.TaskClass{
				ID:                 input.ID,
				Name:               &req.Name,
				Mode:               &req.Mode,
				SubjectType:        stringPtrOrNil(req.SubjectType),
				DifficultyLevel:    stringPtrOrNil(req.DifficultyLevel),
				CognitiveIntensity: stringPtrOrNil(req.CognitiveIntensity),
				TotalSlots:         &req.Config.TotalSlots,
				Strategy:           &req.Config.Strategy,
				ExcludedSlots:      req.Config.ExcludedSlots,
				ExcludedDaysOfWeek: req.Config.ExcludedDaysOfWeek,
			}
			taskClass.AllowFillerCourse = &req.Config.AllowFillerCourse

			// 2. 自动模式下写入日期范围；手动模式允许为空。
			if req.StartDate != "" {
				startDate, parseErr := time.ParseInLocation("2006-01-02", req.StartDate, time.Local)
				if parseErr != nil {
					return parseErr
				}
				taskClass.StartDate = &startDate
			}
			if req.EndDate != "" {
				endDate, parseErr := time.ParseInLocation("2006-01-02", req.EndDate, time.Local)
				if parseErr != nil {
					return parseErr
				}
				taskClass.EndDate = &endDate
			}

			// 3. upsert 主体后拿到稳定 task_class_id，供 items 绑定 category_id。
			updatedID, upsertErr := txDAO.AddOrUpdateTaskClass(userID, taskClass)
			if upsertErr != nil {
				return upsertErr
			}
			taskClassID = updatedID

			// 4. 构造任务块并批量 upsert。
			items := make([]model.TaskClassItem, 0, len(req.Items))
			for _, itemReq := range req.Items {
				categoryID := taskClassID
				order := itemReq.Order
				content := itemReq.Content
				status := model.TaskItemStatusUnscheduled
				items = append(items, model.TaskClassItem{
					ID:           itemReq.ID,
					CategoryID:   &categoryID,
					Order:        &order,
					Content:      &content,
					EmbeddedTime: itemReq.EmbeddedTime,
					Status:       &status,
				})
			}
			return txDAO.AddOrUpdateTaskClassItems(userID, items)
		})
		if err != nil {
			return newagenttools.TaskClassUpsertPersistResult{}, err
		}
		return newagenttools.TaskClassUpsertPersistResult{
			TaskClassID: taskClassID,
			Created:     created,
		}, nil
	}
}

func buildQuickTaskCreateFunc(taskRepo *dao.TaskDAO) func(userID int, title string, priorityGroup int, estimatedSections int, deadlineAt *time.Time, urgencyThresholdAt *time.Time) (int, error) {
	return func(userID int, title string, priorityGroup int, estimatedSections int, deadlineAt *time.Time, urgencyThresholdAt *time.Time) (int, error) {
		created, err := taskRepo.AddTask(&model.Task{
			UserID:             userID,
			Title:              title,
			Priority:           priorityGroup,
			EstimatedSections:  model.NormalizeEstimatedSections(&estimatedSections),
			IsCompleted:        false,
			DeadlineAt:         deadlineAt,
			UrgencyThresholdAt: urgencyThresholdAt,
		})
		if err != nil {
			return 0, err
		}
		return created.ID, nil
	}
}

func buildQuickTaskQueryFunc(agentService *service.AgentService) func(ctx context.Context, userID int, params newagentmodel.TaskQueryParams) ([]newagentmodel.TaskQueryResult, error) {
	return func(ctx context.Context, userID int, params newagentmodel.TaskQueryParams) ([]newagentmodel.TaskQueryResult, error) {
		req := newagentmodel.TaskQueryRequest{
			UserID:           userID,
			Quadrant:         params.Quadrant,
			SortBy:           params.SortBy,
			Order:            params.Order,
			Limit:            params.Limit,
			IncludeCompleted: params.IncludeCompleted,
			Keyword:          params.Keyword,
			DeadlineBefore:   params.DeadlineBefore,
			DeadlineAfter:    params.DeadlineAfter,
		}
		records, err := agentService.QueryTasksForTool(ctx, req)
		if err != nil {
			return nil, err
		}
		results := make([]newagentmodel.TaskQueryResult, 0, len(records))
		for _, r := range records {
			deadlineStr := ""
			if r.DeadlineAt != nil {
				deadlineStr = r.DeadlineAt.In(time.Local).Format("2006-01-02 15:04")
			}
			results = append(results, newagentmodel.TaskQueryResult{
				ID:                r.ID,
				Title:             r.Title,
				PriorityGroup:     r.PriorityGroup,
				EstimatedSections: model.NormalizeEstimatedSections(&r.EstimatedSections),
				IsCompleted:       r.IsCompleted,
				DeadlineAt:        deadlineStr,
			})
		}
		return results, nil
	}
}

func buildAPIHandlers(
	taskClient ports.TaskCommandClient,
	taskClassClient ports.TaskClassCommandClient,
	courseClient ports.CourseCommandClient,
	scheduleClient ports.ScheduleCommandClient,
	agentService *service.AgentService,
	memoryClient ports.MemoryCommandClient,
	activeSchedulerClient ports.ActiveSchedulerCommandClient,
	notificationClient ports.NotificationCommandClient,
) *api.ApiHandlers {
	return &api.ApiHandlers{
		TaskHandler:      api.NewTaskHandler(taskClient),
		TaskClassHandler: api.NewTaskClassHandler(taskClassClient),
		CourseHandler:    api.NewCourseHandler(courseClient),
		ScheduleHandler:  api.NewScheduleAPI(scheduleClient),
		AgentHandler:     api.NewAgentHandler(agentService),
		MemoryHandler:    api.NewMemoryHandler(memoryClient),
		ActiveSchedule:   api.NewActiveScheduleAPI(activeSchedulerClient),
		Notification:     api.NewNotificationAPI(notificationClient),
	}
}

func (r *appRuntime) startWorkers(ctx context.Context) {
	if r == nil {
		return
	}

	if r.eventBus != nil {
		r.eventBus.Start(ctx)
		log.Println("Outbox event bus started")
	} else {
		log.Println("Outbox event bus is disabled")
	}
	log.Println("Memory worker is managed by cmd/memory in phase 6 CP1")
}

func (r *appRuntime) registerEventHandlers() error {
	// 调用目的：只注册仍留在单体残留域内的 outbox handler；active-scheduler / notification 已由各自独立进程管理消费边界。
	if err := eventsvc.RegisterCoreOutboxHandlers(
		r.eventBus,
		r.outboxRepo,
		r.manager,
		r.agentRepo,
		r.cacheRepo,
		r.memoryModule,
		r.userAuthClient,
	); err != nil {
		return err
	}
	return nil
}

func (r *appRuntime) startHTTP(ctx context.Context) {
	router := gatewayrouter.RegisterRouters(r.handlers, r.userAuthClient, r.cacheRepo, r.limiter)
	gatewayrouter.StartEngine(ctx, router)
}

func (r *appRuntime) close() {
	if r == nil {
		return
	}
	if r.eventBus != nil {
		r.eventBus.Close()
	}
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
