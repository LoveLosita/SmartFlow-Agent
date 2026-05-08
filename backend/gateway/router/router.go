package router

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	taskclassforumclient "github.com/LoveLosita/smartflow/backend/client/taskclassforum"
	tokenstoreclient "github.com/LoveLosita/smartflow/backend/client/tokenstore"
	"github.com/LoveLosita/smartflow/backend/gateway/api"
	forumapi "github.com/LoveLosita/smartflow/backend/gateway/api/forumapi"
	tokenstoreapi "github.com/LoveLosita/smartflow/backend/gateway/api/tokenstoreapi"
	userauthapi "github.com/LoveLosita/smartflow/backend/gateway/api/userauth"
	gatewaymiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	rootmiddleware "github.com/LoveLosita/smartflow/backend/gateway/middleware"
	"github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	ratelimit "github.com/LoveLosita/smartflow/backend/shared/infra/ratelimit"
	"github.com/LoveLosita/smartflow/backend/shared/ports"
	"github.com/gin-gonic/gin"
	"github.com/spf13/viper"
)

// StartEngine 启动 HTTP 服务，并在上下文取消时尽量优雅退出。
func StartEngine(ctx context.Context, r *gin.Engine) {
	// 1. 先解析端口，保持和历史行为一致。
	// 2. 再用 http.Server 托管 gin engine，便于收到取消信号时执行 Shutdown。
	port := viper.GetString("server.port")
	if port == "" {
		port = "8080"
	}

	srv := &http.Server{
		Addr:    ":" + port,
		Handler: r,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Server starting on port %s...", port)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("Failed to shutdown server gracefully: %v", err)
		}
		if err := <-errCh; err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Failed to start server: %v", err)
		}
	}
}

func RegisterRouters(
	handlers *api.ApiHandlers,
	authClient ports.UserAuthClient,
	forumClient *taskclassforumclient.Client,
	tokenStoreClient *tokenstoreclient.Client,
	cache *dao.CacheDAO,
	limiter *ratelimit.RateLimiter,
) *gin.Engine {
	r := gin.Default()
	r.Use(gatewaymiddleware.CORSMiddleware(gatewaymiddleware.CORSOptions{
		AllowedOrigins:   readConfigList("cors.allowedOrigins"),
		AllowedMethods:   readConfigList("cors.allowedMethods"),
		AllowedHeaders:   readConfigList("cors.allowedHeaders"),
		ExposedHeaders:   readConfigList("cors.exposedHeaders"),
		AllowCredentials: viper.GetBool("cors.allowCredentials"),
	}))
	apiGroup := r.Group("/api/v1")
	{
		apiGroup.GET("/health", func(c *gin.Context) {
			c.JSON(200, gin.H{
				"status":  "ok",
				"version": "0.4.0.dev",
			})
		})

		userauthapi.RegisterRoutes(apiGroup, userauthapi.NewUserHandler(
			authClient,
			userauthapi.NewGeeTestServiceFromConfig(),
			readUserAuthAllowRegister(),
		), authClient, limiter)
		forumapi.RegisterRoutes(apiGroup, forumapi.NewHandler(forumClient), authClient, cache, limiter)
		tokenstoreapi.RegisterRoutes(apiGroup, tokenstoreapi.NewHandler(tokenStoreClient), authClient, cache, limiter)

		taskGroup := apiGroup.Group("/task")
		{
			taskGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			taskGroup.POST("/create", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskHandler.AddTask)
			taskGroup.PUT("/complete", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskHandler.CompleteTask)
			taskGroup.PUT("/undo-complete", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskHandler.UndoCompleteTask)
			taskGroup.PUT("/update", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskHandler.UpdateTask)
			taskGroup.DELETE("/delete", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskHandler.DeleteTask)
			taskGroup.GET("/get", handlers.TaskHandler.GetUserTasks)
			taskGroup.POST("/batch-status", handlers.TaskHandler.BatchTaskStatus)
		}

		courseGroup := apiGroup.Group("/course")
		{
			courseGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			courseGroup.POST("/validate", handlers.CourseHandler.CheckUserCourse)
			courseGroup.POST("/parse-image", handlers.CourseHandler.ParseCourseTableImage)
			courseGroup.POST("/import", rootmiddleware.IdempotencyMiddleware(cache), handlers.CourseHandler.AddUserCourses)
		}

		taskClassGroup := apiGroup.Group("/task-class")
		{
			taskClassGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			taskClassGroup.POST("/add", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserAddTaskClass)
			taskClassGroup.GET("/list", handlers.TaskClassHandler.UserGetTaskClassInfos)
			taskClassGroup.GET("/get", handlers.TaskClassHandler.UserGetCompleteTaskClass)
			taskClassGroup.PUT("/update", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserUpdateTaskClass)
			taskClassGroup.POST("/insert-into-schedule", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserAddTaskClassItemIntoSchedule)
			taskClassGroup.DELETE("/delete-item", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.DeleteTaskClassItem)
			taskClassGroup.DELETE("/delete-class", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.DeleteTaskClass)
			taskClassGroup.PUT("/apply-batch-into-schedule", rootmiddleware.IdempotencyMiddleware(cache), handlers.TaskClassHandler.UserInsertBatchTaskClassItemsIntoSchedule)
		}

		scheduleGroup := apiGroup.Group("/schedule")
		{
			scheduleGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			scheduleGroup.GET("/today", handlers.ScheduleHandler.GetUserTodaySchedule)
			scheduleGroup.GET("/week", handlers.ScheduleHandler.GetUserWeeklySchedule)
			scheduleGroup.DELETE("/delete", rootmiddleware.IdempotencyMiddleware(cache), handlers.ScheduleHandler.DeleteScheduleEvent)
			scheduleGroup.GET("/recent-completed", handlers.ScheduleHandler.GetUserRecentCompletedSchedules)
			scheduleGroup.GET("/current", handlers.ScheduleHandler.GetUserOngoingSchedule)
			scheduleGroup.DELETE("/undo-task-item", rootmiddleware.IdempotencyMiddleware(cache), handlers.ScheduleHandler.UserRevocateTaskItemFromSchedule)
			scheduleGroup.GET("/smart-planning", handlers.ScheduleHandler.SmartPlanning)
			scheduleGroup.POST("/smart-planning-multi", handlers.ScheduleHandler.SmartPlanningMulti)
		}

		agentGroup := apiGroup.Group("/agent")
		{
			agentGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			agentGroup.POST("/chat", handlers.AgentHandler.ChatAgent)
			agentGroup.GET("/conversation-meta", handlers.AgentHandler.GetConversationMeta)
			agentGroup.GET("/conversation-list", handlers.AgentHandler.GetConversationList)
			agentGroup.GET("/conversation-timeline", handlers.AgentHandler.GetConversationTimeline)
			agentGroup.GET("/schedule-preview", handlers.AgentHandler.GetSchedulePlanPreview)
			agentGroup.GET("/context-stats", handlers.AgentHandler.GetContextStats)
			agentGroup.POST("/schedule-state", handlers.AgentHandler.SaveScheduleState)
		}

		memoryGroup := apiGroup.Group("/memory")
		{
			memoryGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			memoryGroup.GET("/items", handlers.MemoryHandler.ListItems)
			memoryGroup.GET("/items/:id", handlers.MemoryHandler.GetItem)
			memoryGroup.POST("/items", rootmiddleware.IdempotencyMiddleware(cache), handlers.MemoryHandler.CreateItem)
			memoryGroup.PATCH("/items/:id", rootmiddleware.IdempotencyMiddleware(cache), handlers.MemoryHandler.UpdateItem)
			memoryGroup.DELETE("/items/:id", rootmiddleware.IdempotencyMiddleware(cache), handlers.MemoryHandler.DeleteItem)
			memoryGroup.POST("/items/:id/restore", rootmiddleware.IdempotencyMiddleware(cache), handlers.MemoryHandler.RestoreItem)
		}

		activeScheduleGroup := apiGroup.Group("/active-schedule")
		{
			activeScheduleGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			activeScheduleGroup.POST("/dry-run", handlers.ActiveSchedule.DryRun)
			activeScheduleGroup.POST("/trigger", handlers.ActiveSchedule.Trigger)
			activeScheduleGroup.POST("/preview", handlers.ActiveSchedule.CreatePreview)
			activeScheduleGroup.GET("/preview/:preview_id", handlers.ActiveSchedule.GetPreview)
			activeScheduleGroup.POST("/preview/:preview_id/confirm", handlers.ActiveSchedule.ConfirmPreview)
		}

		notificationGroup := apiGroup.Group("/notification")
		{
			notificationGroup.Use(gatewaymiddleware.JWTTokenAuth(authClient), rootmiddleware.RateLimitMiddleware(limiter, 20, 1))
			notificationGroup.GET("/channels/feishu", handlers.Notification.GetFeishuWebhook)
			notificationGroup.PUT("/channels/feishu", handlers.Notification.SaveFeishuWebhook)
			notificationGroup.DELETE("/channels/feishu", handlers.Notification.DeleteFeishuWebhook)
			notificationGroup.POST("/channels/feishu/test", handlers.Notification.TestFeishuWebhook)
		}
	}

	log.Println("Routes setup completed")
	return r
}

func readConfigList(key string) []string {
	values := viper.GetStringSlice(key)
	if len(values) > 0 {
		return compactConfigList(expandConfigList(values))
	}

	raw := strings.TrimSpace(viper.GetString(key))
	if raw == "" {
		return nil
	}

	splitted := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ';'
	})
	return compactConfigList(splitted)
}

func expandConfigList(values []string) []string {
	expanded := make([]string, 0, len(values))
	for _, value := range values {
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == ';'
		})
		if len(parts) == 0 {
			expanded = append(expanded, value)
			continue
		}
		expanded = append(expanded, parts...)
	}
	return expanded
}

func compactConfigList(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func readUserAuthAllowRegister() bool {
	// 1. 缺省保持“允许注册”，避免历史未补配置的环境升级后被静默改行为。
	// 2. 只有显式把 userauth.allowRegister 设成 false，才真正关闭注册入口。
	if !viper.IsSet("userauth.allowRegister") {
		return true
	}
	return viper.GetBool("userauth.allowRegister")
}
