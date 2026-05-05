package redisinfra

import (
	"context"

	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
)

// OpenRedisFromConfig 只按统一配置创建 Redis client 并做连通性校验。
//
// 职责边界：
// 1. 只负责初始化通用 Redis 连接，不承载任何业务 key 语义。
// 2. 只做 Ping 校验，失败时返回 error，由调用方决定是否 fail fast。
// 3. 不创建、不预热、不清理任何缓存或分布式锁数据。
func OpenRedisFromConfig() (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis.host") + ":" + viper.GetString("redis.port"),
		Password: viper.GetString("redis.password"),
		DB:       0,
	})
	if _, err := client.Ping(context.Background()).Result(); err != nil {
		return nil, err
	}
	return client, nil
}
