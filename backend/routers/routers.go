// Package routers 路由配置
// 定义所有HTTP路由和路由组
package routers

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/middleware"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// StartEngine 注册路由
func StartEngine(r *gin.Engine) {
	// 从配置中获取端口
	port := viper.GetString("server.port")
	if port == "" {
		port = "8080" // 默认端口
	}

	// 启动服务器
	log.Printf("Server starting on port %s...", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func RegisterRouters(handlers *api.ApiHandlers, cache *dao.CacheDAO) *gin.Engine {
	// 初始化Gin引擎
	r := gin.Default()
	// 在这里注册所有的路由和路由组
	apiGroup := r.Group("/api/v1")
	{
		// 健康检查路由
		apiGroup.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "ok",
				"version": "1.0.0",
			})
		})

		userGroup := apiGroup.Group("/user")
		{
			userGroup.POST("/register", handlers.UserHandler.UserRegister)
			userGroup.POST("/login", handlers.UserHandler.UserLogin)
			userGroup.POST("/refresh-token", handlers.UserHandler.RefreshTokenHandler)
			userGroup.POST("/logout", middleware.JWTTokenAuth(cache), handlers.UserHandler.UserLogout)
		}
		taskGroup := apiGroup.Group("/task")
		{
			taskGroup.Use(middleware.JWTTokenAuth(cache))
			taskGroup.POST("/create", handlers.TaskHandler.AddTask)
			taskGroup.GET("/get", handlers.TaskHandler.GetUserTasks)
		}
	}
	// 初始化Gin引擎
	log.Println("Routes setup completed")
	return r
}
