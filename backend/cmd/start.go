package cmd

import (
	"fmt"
	"log"

	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/inits"
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

	userRepo := dao.NewUserDAO(db)
	cacheRepo := dao.NewCacheDAO(rdb)
	userService := service.NewUserService(userRepo, cacheRepo)
	userApi := api.NewUserHandler(userService)
	handlers := &api.ApiHandlers{
		UserHandler: userApi,
	}
	r := routers.RegisterRouters(handlers, cacheRepo)
	routers.StartEngine(r)
}
