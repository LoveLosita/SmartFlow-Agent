package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	infrallm "github.com/LoveLosita/smartflow/backend/infra/llm"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	infrarag "github.com/LoveLosita/smartflow/backend/infra/rag"
	ragconfig "github.com/LoveLosita/smartflow/backend/infra/rag/config"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/memory"
	memoryobserve "github.com/LoveLosita/smartflow/backend/memory/observe"
	"github.com/LoveLosita/smartflow/backend/middleware"
	"github.com/LoveLosita/smartflow/backend/model"
	newagentconv "github.com/LoveLosita/smartflow/backend/newAgent/conv"
	newagentmodel "github.com/LoveLosita/smartflow/backend/newAgent/model"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
	"github.com/LoveLosita/smartflow/backend/newAgent/tools/web"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/routers"
	"github.com/LoveLosita/smartflow/backend/service"
	eventsvc "github.com/LoveLosita/smartflow/backend/service/events"
	"github.com/spf13/viper"
)

// loadConfig 加载应用配置。
func loadConfig() error {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	log.Println("Config loaded successfully")
	return nil
}

// Start 是应用启动入口。
func Start() {
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	db, err := inits.ConnectDB()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	rdb := inits.InitRedis()
	limiter := pkg.NewRateLimiter(rdb)

	aiHub, err := inits.InitEino()
	if err != nil {
		log.Fatalf("Failed to initialize Eino: %v", err)
	}

	ragCfg := ragconfig.LoadFromViper()
	var ragRuntime infrarag.Runtime
	if ragCfg.Enabled {
		// 1. 当前项目尚未完成全局观测平台建设，这里先注入一层轻量 Observer；
		// 2. RAG 内部只依赖 Observer 接口，后续若全项目统一日志/指标系统，只需替换这里；
		// 3. 这样可以避免 RAG 单独自建一套割裂的日志基础设施。
		ragLogger := log.Default()
		ragRuntime, err = infrarag.NewRuntimeFromConfig(context.Background(), ragCfg, infrarag.FactoryDeps{
			Logger:   ragLogger,
			Observer: infrarag.NewLoggerObserver(ragLogger),
		})
		if err != nil {
			log.Fatalf("Failed to initialize RAG runtime: %v", err)
		}
		log.Printf("RAG runtime initialized: store=%s embed=%s reranker=%s", ragCfg.Store, ragCfg.EmbedProvider, ragCfg.RerankerProvider)
	} else {
		log.Println("RAG runtime is disabled")
	}

	// 1. memory 模块对启动层只暴露一个门面。
	// 2. 后续若接入统一 DI 容器，也优先注入这个门面，而不是继续暴露内部 repo/service。
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

	// outbox 通用事件总线接线（第二阶段）：
	// 1. 读取 Kafka 配置；
	// 2. 创建 infra 级 EventBus；
	// 3. 显式注册业务事件处理器；
	// 4. 启动总线后台 dispatch/consume 循环。
	kafkaCfg := kafkabus.LoadConfig()
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkaCfg)
	if err != nil {
		log.Fatalf("Failed to initialize outbox event bus: %v", err)
	}
	if eventBus != nil {
		// 1. 在启动前完成业务事件处理器注册。
		// 2. memory 事件处理器也统一通过 memoryModule 接入，避免启动层感知内部细节。
		if err = eventsvc.RegisterChatHistoryPersistHandler(eventBus, outboxRepo, manager); err != nil {
			log.Fatalf("Failed to register chat history event handler: %v", err)
		}
		if err = eventsvc.RegisterTaskUrgencyPromoteHandler(eventBus, outboxRepo, manager); err != nil {
			log.Fatalf("Failed to register task urgency promote event handler: %v", err)
		}
		if err = eventsvc.RegisterChatTokenUsageAdjustHandler(eventBus, outboxRepo, manager); err != nil {
			log.Fatalf("Failed to register chat token usage adjust event handler: %v", err)
		}
		if err = eventsvc.RegisterAgentStateSnapshotHandler(eventBus, outboxRepo, manager); err != nil {
			log.Fatalf("Failed to register agent state snapshot event handler: %v", err)
		}
		if err = eventsvc.RegisterMemoryExtractRequestedHandler(eventBus, outboxRepo, memoryModule); err != nil {
			log.Fatalf("Failed to register memory extract event handler: %v", err)
		}
		eventBus.Start(context.Background())
		defer eventBus.Close()
		log.Println("Outbox event bus started")
	} else {
		log.Println("Outbox event bus is disabled")
	}

	memoryModule.StartWorker(context.Background())

	// Service 层初始化。
	userService := service.NewUserService(userRepo, cacheRepo)
	taskSv := service.NewTaskService(taskRepo, cacheRepo, eventBus)
	courseImageResponsesClient := infrallm.NewArkResponsesClient(
		os.Getenv("ARK_API_KEY"),
		viper.GetString("agent.baseURL"),
		viper.GetString("courseImport.visionModel"),
	)
	courseService := service.NewCourseService(
		courseRepo,
		scheduleRepo,
		courseImageResponsesClient,
		service.NewCourseImageParseConfig(
			viper.GetInt64("courseImport.maxImageBytes"),
			viper.GetInt("courseImport.maxTokens"),
		),
		viper.GetString("courseImport.visionModel"),
	)
	taskClassService := service.NewTaskClassService(taskClassRepo, cacheRepo, scheduleRepo, manager)
	scheduleService := service.NewScheduleService(scheduleRepo, userRepo, taskClassRepo, manager, cacheRepo)
	agentService := service.NewAgentServiceWithSchedule(aiHub, agentRepo, taskRepo, cacheRepo, agentCacheRepo, eventBus, scheduleService, taskSv)

	// newAgent 依赖接线。
	agentService.SetAgentStateStore(dao.NewAgentStateStoreAdapter(cacheRepo))

	// 1. WebSearch provider 初始化：根据配置选择 mock/bocha；
	// 2. provider 为 nil 时，web_search / web_fetch 返回"暂未启用"，不阻断主流程。
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
			UpsertTaskClass: func(userID int, input newagenttools.TaskClassUpsertInput) (newagenttools.TaskClassUpsertPersistResult, error) {
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
			},
		},
	}))
	agentService.SetScheduleProvider(newagentconv.NewScheduleProvider(scheduleRepo, taskClassRepo))
	agentService.SetCompactionStore(agentRepo)
	agentService.SetQuickTaskDeps(newagentmodel.QuickTaskDeps{
		CreateTask: func(userID int, title string, priorityGroup int, deadlineAt *time.Time, urgencyThresholdAt *time.Time) (int, error) {
			created, err := taskRepo.AddTask(&model.Task{
				UserID:             userID,
				Title:              title,
				Priority:           priorityGroup,
				IsCompleted:        false,
				DeadlineAt:         deadlineAt,
				UrgencyThresholdAt: urgencyThresholdAt,
			})
			if err != nil {
				return 0, err
			}
			return created.ID, nil
		},
		QueryTasks: func(ctx context.Context, userID int, params newagentmodel.TaskQueryParams) ([]newagentmodel.TaskQueryResult, error) {
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
					ID:            r.ID,
					Title:         r.Title,
					PriorityGroup: r.PriorityGroup,
					IsCompleted:   r.IsCompleted,
					DeadlineAt:    deadlineStr,
				})
			}
			return results, nil
		},
	})
	agentService.SetMemoryReader(memoryModule, memoryCfg)

	// API 层初始化。
	userApi := api.NewUserHandler(userService)
	taskApi := api.NewTaskHandler(taskSv)
	courseApi := api.NewCourseHandler(courseService)
	taskClassApi := api.NewTaskClassHandler(taskClassService)
	scheduleApi := api.NewScheduleAPI(scheduleService)
	agentApi := api.NewAgentHandler(agentService)
	memoryApi := api.NewMemoryHandler(memoryModule)
	handlers := &api.ApiHandlers{
		UserHandler:      userApi,
		TaskHandler:      taskApi,
		TaskClassHandler: taskClassApi,
		CourseHandler:    courseApi,
		ScheduleHandler:  scheduleApi,
		AgentHandler:     agentApi,
		MemoryHandler:    memoryApi,
	}

	r := routers.RegisterRouters(handlers, cacheRepo, userRepo, limiter)
	routers.StartEngine(r)
}

func stringPtrOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
