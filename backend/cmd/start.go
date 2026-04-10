package cmd

import (
	"context"
	"fmt"
	"log"

	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	kafkabus "github.com/LoveLosita/smartflow/backend/infra/kafka"
	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/middleware"
	newagentconv "github.com/LoveLosita/smartflow/backend/newAgent/conv"
	newagenttools "github.com/LoveLosita/smartflow/backend/newAgent/tools"
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
	// 3. 显式注册"聊天持久化"事件处理器；
	// 4. 启动总线后台 dispatch/consume 循环。
	kafkaCfg := kafkabus.LoadConfig()
	eventBus, err := outboxinfra.NewEventBus(outboxRepo, kafkaCfg)
	if err != nil {
		log.Fatalf("Failed to initialize outbox event bus: %v", err)
	}
	if eventBus != nil {
		// 3. 在启动前完成"业务事件处理器"注册。
		// 3.1 这里显式调用 service/events，保证 infra 层不承载业务语义。
		// 3.2 若注册失败直接中止启动，避免"消息已入队但无人消费"的隐性故障。
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
		if err = eventsvc.RegisterMemoryExtractRequestedHandler(eventBus, outboxRepo); err != nil {
			log.Fatalf("Failed to register memory extract event handler: %v", err)
		}
		eventBus.Start(context.Background())
		defer eventBus.Close()
		log.Println("Outbox event bus started")
	} else {
		log.Println("Outbox event bus is disabled")
	}

	// Service 层初始化。
	userService := service.NewUserService(userRepo, cacheRepo)
	taskSv := service.NewTaskService(taskRepo, cacheRepo, eventBus)
	courseService := service.NewCourseService(courseRepo, scheduleRepo)
	taskClassService := service.NewTaskClassService(taskClassRepo, cacheRepo, scheduleRepo, manager)
	scheduleService := service.NewScheduleService(scheduleRepo, userRepo, taskClassRepo, manager, cacheRepo)
	agentService := service.NewAgentServiceWithSchedule(aiHub, agentRepo, taskRepo, cacheRepo, agentCacheRepo, eventBus, scheduleService)

	// newAgent 依赖接线。
	agentService.SetAgentStateStore(dao.NewAgentStateStoreAdapter(cacheRepo))
	agentService.SetToolRegistry(newagenttools.NewDefaultRegistry())
	agentService.SetScheduleProvider(newagentconv.NewScheduleProvider(scheduleRepo, taskClassRepo))
	agentService.SetSchedulePersistor(newagentconv.NewSchedulePersistorAdapter(manager))

	// API 层初始化。
	userApi := api.NewUserHandler(userService)
	taskApi := api.NewTaskHandler(taskSv)
	courseApi := api.NewCourseHandler(courseService)
	taskClassApi := api.NewTaskClassHandler(taskClassService)
	scheduleApi := api.NewScheduleAPI(scheduleService)
	agentApi := api.NewAgentHandler(agentService)
	handlers := &api.ApiHandlers{
		UserHandler:      userApi,
		TaskHandler:      taskApi,
		TaskClassHandler: taskClassApi,
		CourseHandler:    courseApi,
		ScheduleHandler:  scheduleApi,
		AgentHandler:     agentApi,
	}

	r := routers.RegisterRouters(handlers, cacheRepo, userRepo, limiter)
	routers.StartEngine(r)
}
