package main

import (
	"log"

	"github.com/LoveLosita/smartflow/backend/bootstrap"
	tokenstoredao "github.com/LoveLosita/smartflow/backend/services/tokenstore/dao"
	tokenstorerpc "github.com/LoveLosita/smartflow/backend/services/tokenstore/rpc"
	tokenstoresv "github.com/LoveLosita/smartflow/backend/services/tokenstore/sv"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := tokenstoredao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect tokenstore database: %v", err)
	}

	// 1. 当前阶段只启动 token-store 自身 RPC 壳和本服务私有表迁移。
	// 2. user/auth 授额出口后续通过 GrantOutlet adapter 切入，避免现在制造冲突。
	// 3. 未实现的业务方法会明确返回 Unimplemented，而不是伪装成可用能力。
	svc := tokenstoresv.New(tokenstoresv.Options{DB: db})
	tokenstorerpc.Start(tokenstorerpc.ServerOptions{
		ListenOn: viper.GetString("tokenstore.rpc.listenOn"),
		Timeout:  viper.GetDuration("tokenstore.rpc.timeout"),
		Service:  svc,
	})
}
