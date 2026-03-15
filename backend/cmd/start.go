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
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/routers"
	"github.com/LoveLosita/smartflow/backend/service"
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

	// outbox 异步链路接线：
	// - 读取 Kafka 配置
	// - 创建基础设施级 outbox 异步引擎
	// - 引擎内部负责 dispatch/consume 两个后台循环
	kafkaCfg := kafkabus.LoadConfig()
	asyncPipeline, err := outboxinfra.NewChatHistoryAsync(outboxRepo, kafkaCfg)
	if err != nil {
		log.Fatalf("Failed to initialize Kafka async pipeline: %v", err)
	}
	if asyncPipeline != nil {
		asyncPipeline.Start(context.Background())
		defer asyncPipeline.Close()
		log.Println("Kafka async pipeline started")
	} else {
		log.Println("Kafka async pipeline is disabled")
	}

	// Service 层初始化。
	userService := service.NewUserService(userRepo, cacheRepo)
	taskSv := service.NewTaskService(taskRepo, cacheRepo)
	courseService := service.NewCourseService(courseRepo, scheduleRepo)
	taskClassService := service.NewTaskClassService(taskClassRepo, cacheRepo, scheduleRepo, manager)
	scheduleService := service.NewScheduleService(scheduleRepo, userRepo, taskClassRepo, manager, cacheRepo)
	agentService := service.NewAgentService(aiHub, agentRepo, taskRepo, agentCacheRepo, asyncPipeline)

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

	r := routers.RegisterRouters(handlers, cacheRepo, limiter)
	routers.StartEngine(r)
}
