package cmd

import (
	"fmt"
	"log"

	"github.com/smartflow/backend/api"
	"github.com/smartflow/backend/dao"
	"github.com/smartflow/backend/inits"
	"github.com/smartflow/backend/routers"
	"github.com/smartflow/backend/service"
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
	userRepo := dao.NewUserDAO(db)
	userService := service.NewUserService(userRepo)
	userApi := api.NewUserHandler(userService)
	handlers := &api.ApiHandlers{
		UserHandler: userApi,
	}
	r := routers.RegisterRouters(handlers)
	routers.StartEngine(r)
}
