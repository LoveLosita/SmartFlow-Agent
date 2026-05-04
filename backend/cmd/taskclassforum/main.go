package main

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	legacydao "github.com/LoveLosita/smartflow/backend/dao"
	"github.com/LoveLosita/smartflow/backend/services/taskclassforum/adapter"
	forumdao "github.com/LoveLosita/smartflow/backend/services/taskclassforum/dao"
	forumrpc "github.com/LoveLosita/smartflow/backend/services/taskclassforum/rpc"
	forumsv "github.com/LoveLosita/smartflow/backend/services/taskclassforum/sv"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := forumdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect taskclassforum database: %v", err)
	}

	// 1. 复用同一个 DB 句柄装配 legacy TaskClass DAO，避免本轮抢改 task-class 模块。
	// 2. 计划广场只通过快照端口读取和创建 TaskClass，不直接写 schedule。
	// 3. 后续 task-class 独立成服务后，只替换这里的 adapter 注入点。
	taskClassPort := adapter.NewLegacyTaskClassAdapter(legacydao.NewTaskClassDAO(db))
	svc := forumsv.New(forumsv.Options{
		DB:            db,
		TaskClassPort: taskClassPort,
	})
	forumrpc.Start(forumrpc.ServerOptions{
		ListenOn: viper.GetString("taskclassforum.rpc.listenOn"),
		Timeout:  viper.GetDuration("taskclassforum.rpc.timeout"),
		Service:  svc,
	})
}
