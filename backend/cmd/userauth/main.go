package main

import (
	"log"

	userauthdao "github.com/LoveLosita/smartflow/backend/services/userauth/dao"
	userauthrpc "github.com/LoveLosita/smartflow/backend/services/userauth/rpc"
	userauthsv "github.com/LoveLosita/smartflow/backend/services/userauth/sv"
	"github.com/LoveLosita/smartflow/backend/shared/infra/bootstrap"
	"github.com/spf13/viper"
)

func main() {
	if err := bootstrap.LoadConfig(); err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	db, err := userauthdao.OpenDBFromConfig()
	if err != nil {
		log.Fatalf("failed to connect userauth database: %v", err)
	}
	rdb, err := userauthdao.OpenRedisFromConfig()
	if err != nil {
		log.Fatalf("failed to connect userauth redis: %v", err)
	}

	svc := userauthsv.New(userauthdao.NewUserDAO(db), userauthdao.NewCacheDAO(rdb))
	userauthrpc.Start(userauthrpc.ServerOptions{
		ListenOn: viper.GetString("userauth.rpc.listenOn"),
		Timeout:  viper.GetDuration("userauth.rpc.timeout"),
		Service:  svc,
	})
}
