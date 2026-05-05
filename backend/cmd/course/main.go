package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	coursedao "github.com/LoveLosita/smartflow/backend/services/course/dao"
	courserpc "github.com/LoveLosita/smartflow/backend/services/course/rpc"
	coursesv "github.com/LoveLosita/smartflow/backend/services/course/sv"
	llmservice "github.com/LoveLosita/smartflow/backend/services/llm"
	rootdao "github.com/LoveLosita/smartflow/backend/services/runtime/dao"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := coursedao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect course database: %v", err)
	}

	// 1. course 自有 DAO 只承载课程导入对 schedule 表的迁移期写入。
	// 2. scheduleRepo 用于复用既有冲突检查，后续若切 schedule RPC bridge 再替换这里。
	courseRepo := coursedao.NewCourseDAO(db)
	scheduleRepo := rootdao.NewScheduleDAO(db)
	courseImageClient := llmservice.NewArkResponsesClient(
		os.Getenv("ARK_API_KEY"),
		viper.GetString("agent.baseURL"),
		viper.GetString("courseImport.visionModel"),
	)
	svc := coursesv.NewCourseService(
		courseRepo,
		scheduleRepo,
		courseImageClient,
		coursesv.NewCourseImageParseConfig(
			viper.GetInt64("courseImport.maxImageBytes"),
			viper.GetInt("courseImport.maxTokens"),
		),
		viper.GetString("courseImport.visionModel"),
	)

	server, listenOn, err := courserpc.NewServer(courserpc.ServerOptions{
		ListenOn:      viper.GetString("course.rpc.listenOn"),
		Timeout:       viper.GetDuration("course.rpc.timeout"),
		MaxImageBytes: viper.GetInt64("courseImport.maxImageBytes"),
		Service:       svc,
	})
	if err != nil {
		log.Fatalf("failed to build course zrpc server: %v", err)
	}
	defer server.Stop()

	go func() {
		log.Printf("course zrpc service starting on %s", listenOn)
		server.Start()
	}()

	<-ctx.Done()
	log.Println("course service stopping")
}
