package main

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
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

	// 1. 当前阶段只启动计划广场自身 RPC 壳。
	// 2. TaskClass legacy adapter 会在第三步业务主链路接入，避免现在抢改 task 模块。
	// 3. 未实现的业务方法会明确返回 Unimplemented，而不是伪装成可用能力。
	svc := forumsv.New(forumsv.Options{DB: db})
	forumrpc.Start(forumrpc.ServerOptions{
		ListenOn: viper.GetString("taskclassforum.rpc.listenOn"),
		Timeout:  viper.GetDuration("taskclassforum.rpc.timeout"),
		Service:  svc,
	})
}
