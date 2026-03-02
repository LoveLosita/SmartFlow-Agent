// Package routers 路由配置
// 定义所有HTTP路由和路由组
package routers

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/api"
	"github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/middleware"
	"github.com/LoveLosita/smartflow/backend/pkg"
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

func RegisterRouters(handlers *api.ApiHandlers, cache *dao.CacheDAO, limiter *pkg.RateLimiter) *gin.Engine {
	// 初始化Gin引擎
	r := gin.Default()
	// 在这里注册所有的路由和路由组
	apiGroup := r.Group("/api/v1")
	{
		// 健康检查路由
		apiGroup.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "ok",
				"version": "0.4.0.dev",
			})
		})

		userGroup := apiGroup.Group("/user")
		{
			userGroup.POST("/register", handlers.UserHandler.UserRegister)
			userGroup.POST("/login", handlers.UserHandler.UserLogin)
			userGroup.POST("/refresh-token", handlers.UserHandler.RefreshTokenHandler)
			userGroup.POST("/logout", middleware.JWTTokenAuth(cache), middleware.RateLimitMiddleware(limiter, 20, 1), handlers.UserHandler.UserLogout)
		}
		taskGroup := apiGroup.Group("/task")
		{
			taskGroup.Use(middleware.JWTTokenAuth(cache), middleware.RateLimitMiddleware(limiter, 20, 1))
			taskGroup.POST("/create", middleware.IdempotencyMiddleware(cache), handlers.TaskHandler.AddTask)
			taskGroup.GET("/get", handlers.TaskHandler.GetUserTasks)
		}
		courseGroup := apiGroup.Group("/course")
		{
			courseGroup.Use(middleware.JWTTokenAuth(cache), middleware.RateLimitMiddleware(limiter, 20, 1))
			courseGroup.POST("/validate", handlers.CourseHandler.CheckUserCourse)
			courseGroup.POST("/import", middleware.IdempotencyMiddleware(cache), handlers.CourseHandler.AddUserCourses)
		}
		taskClassGroup := apiGroup.Group("/task-class")
		{
			taskClassGroup.Use(middleware.JWTTokenAuth(cache), middleware.RateLimitMiddleware(limiter, 20, 1))
			taskClassGroup.POST("/add", middleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserAddTaskClass)
			taskClassGroup.GET("/list", handlers.TaskClassHandler.UserGetTaskClassInfos)
			taskClassGroup.GET("/get", handlers.TaskClassHandler.UserGetCompleteTaskClass)
			taskClassGroup.PUT("/update", middleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserUpdateTaskClass)
			taskClassGroup.POST("/insert-into-schedule", middleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserAddTaskClassItemIntoSchedule)
			taskClassGroup.DELETE("/delete-item", middleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.DeleteTaskClassItem)
			taskClassGroup.DELETE("/delete-class", middleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.DeleteTaskClass)
			taskClassGroup.PUT("/apply-batch-into-schedule", middleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserInsertBatchTaskClassItemsIntoSchedule)
		}
		scheduleGroup := apiGroup.Group("/schedule")
		{
			scheduleGroup.Use(middleware.JWTTokenAuth(cache), middleware.RateLimitMiddleware(limiter, 20, 1))
			scheduleGroup.GET("/today", handlers.ScheduleHandler.GetUserTodaySchedule)
			scheduleGroup.GET("/week", handlers.ScheduleHandler.GetUserWeeklySchedule)
			scheduleGroup.DELETE("/delete", middleware.IdempotencyMiddleware(cache), handlers.ScheduleHandler.DeleteScheduleEvent)
			scheduleGroup.GET("/recent-completed", handlers.ScheduleHandler.GetUserRecentCompletedSchedules)
			scheduleGroup.GET("/current", handlers.ScheduleHandler.GetUserOngoingSchedule)
			scheduleGroup.DELETE("/undo-task-item", middleware.IdempotencyMiddleware(cache), handlers.ScheduleHandler.UserRevocateTaskItemFromSchedule)
			scheduleGroup.GET("/smart-planning", handlers.ScheduleHandler.SmartPlanning)
		}
		agentGroup := apiGroup.Group("/agent")
		{
			agentGroup.Use(middleware.JWTTokenAuth(cache), middleware.RateLimitMiddleware(limiter, 20, 1))
			agentGroup.POST("/chat", handlers.AgentHandler.ChatAgent)
		}
	}
	// 初始化Gin引擎
	log.Println("Routes setup completed")
	return r
}
