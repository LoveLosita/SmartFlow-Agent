package cmd

import (
	"fmt"
	"log"

	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/inits"
	"github.com/LoveLosita/smartflow/backend/middleware"
	"github.com/LoveLosita/smartflow/backend/pkg"
	"github.com/LoveLosita/smartflow/backend/routers"
	"github.com/LoveLosita/smartflow/backend/service"
	"github.com/spf13/viper"
)

// loadConfig 加载配置
// 从配置文件中读取配置信息
func loadConfig() error {
	// 设置配置文件路径
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	// 读取配置文件
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}
	log.Println("Config loaded successfully")
	return nil
}

// Start 启动函数
func Start() {
	// 加载配置
	if err := loadConfig(); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	// 初始化数据库
	db, err := inits.ConnectDB()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	rdb := inits.InitRedis()
	//工具包
	limiter := pkg.NewRateLimiter(rdb)
	//初始化eino
	aiHub, err := inits.InitEino()
	if err != nil {
		log.Fatalf("Failed to initialize Eino: %v", err)
	}
	//中间件

	//dao 层
	cacheRepo := dao.NewCacheDAO(rdb)
	agentCacheRepo := dao.NewAgentCache(rdb)
	_ = db.Use(middleware.NewGormCachePlugin(cacheRepo)) // 注册 GORM 插件
	userRepo := dao.NewUserDAO(db)
	taskRepo := dao.NewTaskDAO(db)
	courseRepo := dao.NewCourseDAO(db)
	taskClassRepo := dao.NewTaskClassDAO(db)
	scheduleRepo := dao.NewScheduleDAO(db)
	manager := dao.NewManager(db)
	agentRepo := dao.NewAgentDAO(db)
	//service 层
	userService := service.NewUserService(userRepo, cacheRepo)
	taskSv := service.NewTaskService(taskRepo, cacheRepo)
	courseService := service.NewCourseService(courseRepo, scheduleRepo)
	taskClassService := service.NewTaskClassService(taskClassRepo, cacheRepo, scheduleRepo, manager)
	scheduleService := service.NewScheduleService(scheduleRepo, userRepo, taskClassRepo, manager, cacheRepo)
	agentService := service.NewAgentService(aiHub, agentRepo, agentCacheRepo)
	//api 层
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
