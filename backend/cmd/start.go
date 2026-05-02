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

	activeadapters "github.com/LoveLosita/smartflow/backend/active_scheduler/adapters"
	"github.com/LoveLosita/smartflow/backend/active_scheduler/applyadapter"
	activefeedbacklocate "github.com/LoveLosita/smartflow/backend/active_scheduler/feedbacklocate"
	activegraph "github.com/LoveLosita/smartflow/backend/active_scheduler/graph"
	activejob "github.com/LoveLosita/smartflow/backend/active_scheduler/job"
	activepreview "github.com/LoveLosita/smartflow/backend/active_scheduler/preview"
	activesel "github.com/LoveLosita/smartflow/backend/active_scheduler/selection"
	activesvc "github.com/LoveLosita/smartflow/backend/active_scheduler/service"
	activeTrigger "github.com/LoveLosita/smartflow/backend/active_scheduler/trigger"
	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	ragconfig "github.com/LoveLosita/smartflow/backend/infra/rag/config"
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
	"github.com/LoveLosita/smartflow/backend/notification"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/routers"
	"github.com/LoveLosita/smartflow/backend/service"
	agentsvcsvc "github.com/LoveLosita/smartflow/backend/service/agentsvc"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
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
	db                    *gorm.DB
	redisClient           *redis.Client
	cacheRepo             *dao.CacheDAO
	userRepo              *dao.UserDAO
	agentRepo             *dao.AgentDAO
	agentCache            *dao.AgentCache
	manager               *dao.RepoManager
	outboxRepo            *outboxinfra.Repository
	eventBus              *outboxinfra.EventBus
	memoryModule          *memory.Module
	activeJobScanner      *activejob.Scanner
	activeTriggerWorkflow *activesvc.TriggerWorkflowService
	notificationService   *notification.NotificationService
	limiter               *pkg.RateLimiter
	handlers              *api.ApiHandlers
}

// loadConfig 加载应用配置。
func loadConfig() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	// 1. 兼容从仓库根目录执行 `go run ./backend/cmd/api` 的场景；
	// 2. 从 backend 目录执行时仍优先命中当前目录，不改变现有默认行为。
	viper.AddConfigPath("backend")
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	log.Println("Config loaded successfully")
	return nil
}

// Start 保留历史入口，默认仍按 all 模式启动。
//
// 职责边界：
// 1. 兼容 backend/main.go 以及旧部署命令；
// 2. 不新增业务语义，只委托给 StartAll；
// 3. 后续若部署全面切到独立 api/worker，可逐步废弃该兼容入口。
func Start() {
	StartAll()
}

// StartAll 启动迁移期兼容模式：HTTP API 与后台 worker 在同一进程内运行。
func StartAll() {
	ctx := context.Background()
	runtime := mustBuildRuntime(ctx)
	defer runtime.close()

	runtime.startWorkers(ctx)
	runtime.startHTTP()
}

// StartAPI 只启动 Gin API 及其同步 service/dao 依赖，不启动后台 worker。
//
// 说明：
// 1. 该模式仍是“带 service/dao 的 API 单体”，不是最终 API Gateway；
// 2. API 可以继续写入 outbox，但不负责消费 outbox，也不启动 memory worker；
// 3. worker 停止时，API 仍可提供同步接口，只是异步能力会延迟处理。
func StartAPI() {
	ctx := context.Background()
	runtime := mustBuildRuntime(ctx)
	defer runtime.close()

	runtime.startHTTP()
}

// StartWorker 只启动后台异步能力，不注册 Gin 路由。
//
// 运行内容：
// 1. outbox relay：扫描 pending 消息并投递 Kafka；
// 2. Kafka consumer：消费事件并分发到业务 handler；
// 3. memory worker：处理 memory_jobs 后台任务。
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

	db, err := inits.ConnectDB()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	rdb := inits.InitRedis()
	limiter := pkg.NewRateLimiter(rdb)

	aiHub, err := inits.InitEino()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Eino: %w", err)
	}

	ragRuntime, err := buildRAGRuntime(ctx)
	if err != nil {
		return nil, err
	}

	memoryCfg := memory.LoadConfigFromViper()
	memoryObserver := memoryobserve.NewLoggerObserver(log.Default())
	memoryMetrics := memoryobserve.NewMetricsRegistry()
	memoryModule := memory.NewModuleWithObserve(
		db,
		infrallm.WrapArkClient(aiHub.Pro),
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
	userRepo := dao.NewUserDAO(db)
	taskRepo := dao.NewTaskDAO(db)
	courseRepo := dao.NewCourseDAO(db)
	taskClassRepo := dao.NewTaskClassDAO(db)
	scheduleRepo := dao.NewScheduleDAO(db)
	manager := dao.NewManager(db)
	agentRepo := dao.NewAgentDAO(db)
	outboxRepo := outboxinfra.NewRepository(db)

	eventBus, err := buildEventBus(outboxRepo)
	if err != nil {
		return nil, err
	}

	// Service 层初始化。
	userService := service.NewUserService(userRepo, cacheRepo)
	taskSv := service.NewTaskService(taskRepo, cacheRepo, eventBus)
	taskSv.SetActiveScheduleDAO(manager.ActiveSchedule)
	courseService := buildCourseService(courseRepo, scheduleRepo)
	taskClassService := service.NewTaskClassService(taskClassRepo, cacheRepo, scheduleRepo, manager)
	scheduleService := service.NewScheduleService(scheduleRepo, userRepo, taskClassRepo, manager, cacheRepo)
	agentService := service.NewAgentServiceWithSchedule(
		aiHub,
		agentRepo,
		taskRepo,
		cacheRepo,
		agentCacheRepo,
		manager.ActiveSchedule,
		manager.ActiveScheduleSession,
		eventBus,
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
		memoryModule,
		memoryCfg,
	)

	activeReaders := activeadapters.NewGormReaders(db)
	activeScheduleDryRun, err := activesvc.NewDryRunService(activeadapters.ReadersFromGorm(activeReaders))
	if err != nil {
		return nil, err
	}
	activeScheduleTrigger, err := activesvc.NewTriggerService(manager.ActiveSchedule, eventBus)
	if err != nil {
		return nil, err
	}
	activeSchedulePreviewConfirm, err := buildActiveSchedulePreviewConfirmService(db, manager.ActiveSchedule, activeScheduleDryRun)
	if err != nil {
		return nil, err
	}
	// 1. 主动调度选择器单独复用 Pro 模型，LLM 失败时由 selection 层显式回退到确定性候选；
	// 2. dry-run 与 selection 通过 graph runner 串起来，避免 trigger_pipeline 再拼第二套候选逻辑。
	activeScheduleLLMClient := infrallm.WrapArkClient(aiHub.Pro)
	activeScheduleSelector := activesel.NewService(activeScheduleLLMClient)
	activeScheduleFeedbackLocator := activefeedbacklocate.NewService(activeReaders, activeScheduleLLMClient)
	activeScheduleGraphRunner, err := activegraph.NewRunner(activeScheduleDryRun.AsGraphDryRunFunc(), activeScheduleSelector)
	if err != nil {
		return nil, err
	}
	agentService.SetActiveScheduleSessionRerunFunc(buildActiveScheduleSessionRerunFunc(manager.ActiveSchedule, activeScheduleGraphRunner, activeSchedulePreviewConfirm, activeScheduleFeedbackLocator))
	// 1. 生产投递先切到用户级飞书 Webhook provider，mock provider 文件继续保留给后续单测和本地隔离验证。
	// 2. provider 与配置测试接口共用同一个实例，保证“测试成功”和“正式投递”走同一套 URL 校验、JSON 拼装和 HTTP 结果分类。
	feishuProvider, err := notification.NewWebhookFeishuProvider(manager.Notification, notification.WebhookFeishuProviderOptions{
		FrontendBaseURL: viper.GetString("notification.frontendBaseURL"),
	})
	if err != nil {
		return nil, err
	}
	notificationService, err := notification.NewNotificationService(manager.ActiveSchedule, feishuProvider, notification.ServiceOptions{})
	if err != nil {
		return nil, err
	}
	notificationChannelService, err := notification.NewChannelService(manager.Notification, feishuProvider, notification.ChannelServiceOptions{})
	if err != nil {
		return nil, err
	}
	var activeTriggerWorkflow *activesvc.TriggerWorkflowService
	var activeJobScanner *activejob.Scanner
	if eventBus != nil {
		activeTriggerWorkflow, err = activesvc.NewTriggerWorkflowServiceWithOptions(
			manager.ActiveSchedule,
			activeScheduleGraphRunner,
			outboxRepo,
			kafkabus.LoadConfig(),
			activesvc.WithActiveScheduleSessionBridge(manager.Agent, manager.ActiveScheduleSession),
		)
		if err != nil {
			return nil, err
		}
		activeJobScanner, err = activejob.NewScanner(manager.ActiveSchedule, activeadapters.ReadersFromGorm(activeReaders), activeScheduleTrigger, activejob.ScannerOptions{
			ScanEvery: viper.GetDuration("activeScheduler.jobScanEvery"),
			Limit:     viper.GetInt("activeScheduler.jobScanLimit"),
		})
		if err != nil {
			return nil, err
		}
	}
	handlers := buildAPIHandlers(userService, taskSv, taskClassService, courseService, scheduleService, agentService, memoryModule, activeScheduleDryRun, activeSchedulePreviewConfirm, activeScheduleTrigger, notificationChannelService)

	return &appRuntime{
		db:                    db,
		redisClient:           rdb,
		cacheRepo:             cacheRepo,
		userRepo:              userRepo,
		agentRepo:             agentRepo,
		agentCache:            agentCacheRepo,
		manager:               manager,
		outboxRepo:            outboxRepo,
		eventBus:              eventBus,
		memoryModule:          memoryModule,
		activeJobScanner:      activeJobScanner,
		activeTriggerWorkflow: activeTriggerWorkflow,
		notificationService:   notificationService,
		limiter:               limiter,
		handlers:              handlers,
	}, nil
}

func buildRAGRuntime(ctx context.Context) (infrarag.Runtime, error) {
	ragCfg := ragconfig.LoadFromViper()
	if !ragCfg.Enabled {
		log.Println("RAG runtime is disabled")
		return nil, nil
	}

	// 1. 当前项目尚未完成全局观测平台建设，这里先注入一层轻量 Observer；
	// 2. RAG 内部只依赖 Observer 接口，后续若全项目统一日志/指标系统，只需替换这里；
	// 3. 这样可以避免 RAG 单独自建一套割裂的日志基础设施。
	ragLogger := log.Default()
	ragRuntime, err := infrarag.NewRuntimeFromConfig(ctx, ragCfg, infrarag.FactoryDeps{
		Logger:   ragLogger,
		Observer: infrarag.NewLoggerObserver(ragLogger),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize RAG runtime: %w", err)
	}
	log.Printf("RAG runtime initialized: store=%s embed=%s reranker=%s", ragCfg.Store, ragCfg.EmbedProvider, ragCfg.RerankerProvider)
	return ragRuntime, nil
}

func buildEventBus(outboxRepo *outboxinfra.Repository) (*outboxinfra.EventBus, error) {
	// outbox 通用事件总线接线：
	// 1. API 模式只使用 Publish 写入 outbox，不启动后台循环；
	// 2. worker/all 模式再显式注册 handler 并启动后台循环；
	// 3. kafka.enabled=false 时返回 nil，业务按既有降级策略执行。
	kafkaCfg := kafkabus.LoadConfig()
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkaCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize outbox event bus: %w", err)
	}
	if eventBus == nil {
		log.Println("Outbox event bus is disabled")
	}
	return eventBus, nil
}

func buildCourseService(courseRepo *dao.CourseDAO, scheduleRepo *dao.ScheduleDAO) *service.CourseService {
	courseImageResponsesClient := infrallm.NewArkResponsesClient(
		os.Getenv("ARK_API_KEY"),
		viper.GetString("agent.baseURL"),
		viper.GetString("courseImport.visionModel"),
	)
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

func buildActiveScheduleDryRunService(db *gorm.DB) (*activesvc.DryRunService, error) {
	readers := activeadapters.NewGormReaders(db)
	return activesvc.NewDryRunService(activeadapters.ReadersFromGorm(readers))
}

func buildActiveSchedulePreviewConfirmService(db *gorm.DB, activeDAO *dao.ActiveScheduleDAO, dryRun *activesvc.DryRunService) (*activesvc.PreviewConfirmService, error) {
	previewService, err := activepreview.NewService(activeDAO)
	if err != nil {
		return nil, err
	}
	return activesvc.NewPreviewConfirmService(dryRun, previewService, activeDAO, applyadapter.NewGormApplyAdapter(db))
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
	ragRuntime infrarag.Runtime,
	agentRepo *dao.AgentDAO,
	cacheRepo *dao.CacheDAO,
	taskRepo *dao.TaskDAO,
	taskClassRepo *dao.TaskClassDAO,
	scheduleRepo *dao.ScheduleDAO,
	memoryModule *memory.Module,
	memoryCfg memorymodel.Config,
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
	agentService.SetMemoryReader(memoryModule, memoryCfg)
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
	userService *service.UserService,
	taskService *service.TaskService,
	taskClassService *service.TaskClassService,
	courseService *service.CourseService,
	scheduleService *service.ScheduleService,
	agentService *service.AgentService,
	memoryModule *memory.Module,
	activeScheduleDryRun *activesvc.DryRunService,
	activeSchedulePreviewConfirm *activesvc.PreviewConfirmService,
	activeScheduleTrigger *activesvc.TriggerService,
	notificationChannelService *notification.ChannelService,
) *api.ApiHandlers {
	return &api.ApiHandlers{
		UserHandler:      api.NewUserHandler(userService),
		TaskHandler:      api.NewTaskHandler(taskService),
		TaskClassHandler: api.NewTaskClassHandler(taskClassService),
		CourseHandler:    api.NewCourseHandler(courseService),
		ScheduleHandler:  api.NewScheduleAPI(scheduleService),
		AgentHandler:     api.NewAgentHandler(agentService),
		MemoryHandler:    api.NewMemoryHandler(memoryModule),
		ActiveSchedule:   api.NewActiveScheduleAPI(activeScheduleDryRun, activeSchedulePreviewConfirm, activeScheduleTrigger),
		Notification:     api.NewNotificationAPI(notificationChannelService),
	}
}

func (r *appRuntime) startWorkers(ctx context.Context) {
	if r == nil {
		return
	}

	if r.eventBus != nil {
		if err := r.registerEventHandlers(); err != nil {
			log.Fatalf("Failed to register outbox event handlers: %v", err)
		}
		r.eventBus.Start(ctx)
		log.Println("Outbox event bus started")
	} else {
		log.Println("Outbox event bus is disabled")
	}

	if r.memoryModule != nil {
		r.memoryModule.StartWorker(ctx)
	}
	if r.activeJobScanner != nil {
		r.activeJobScanner.Start(ctx)
		log.Println("Active schedule due job scanner started")
	}
	if r.notificationService != nil {
		r.notificationService.StartRetryLoop(ctx, viper.GetDuration("notification.retryScanEvery"), viper.GetInt("notification.retryBatchSize"))
		log.Println("Notification retry scanner started")
	}
}

func (r *appRuntime) registerEventHandlers() error {
	// 调用目的：worker/all 启动时复用同一套核心事件注册顺序，避免未来新增入口后复制多份 handler 接线。
	if err := eventsvc.RegisterCoreOutboxHandlers(r.eventBus, r.outboxRepo, r.manager, r.agentRepo, r.cacheRepo, r.memoryModule); err != nil {
		return err
	}
	if err := eventsvc.RegisterActiveScheduleTriggeredHandler(r.eventBus, r.outboxRepo, r.activeTriggerWorkflow); err != nil {
		return fmt.Errorf("注册主动调度触发 handler 失败: %w", err)
	}
	// 调用目的：飞书通知事件消费与 notification retry loop 复用同一个服务实例，
	// 保证后续接入真实 provider 时只需要替换启动期注入配置。
	if err := eventsvc.RegisterFeishuNotificationHandler(r.eventBus, r.outboxRepo, r.notificationService); err != nil {
		return fmt.Errorf("注册飞书通知 handler 失败: %w", err)
	}
	return nil
}

func (r *appRuntime) startHTTP() {
	router := routers.RegisterRouters(r.handlers, r.cacheRepo, r.userRepo, r.limiter)
	routers.StartEngine(router)
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
