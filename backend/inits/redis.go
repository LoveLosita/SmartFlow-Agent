package inits

import (
	"context"
	"log"

	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
)

func InitRedis() *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis.host") + ":" + viper.GetString("redis.port"),
		Password: viper.GetString("redis.password"),
		DB:       0,
	})
	// 检查连接是否通畅
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		log.Fatalf("Redis 连接失败: %v", err)
	}
	return rdb
}
