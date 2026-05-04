package inits

import (
	"context"
	"log"

	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
)

// OpenRedisFromConfig 只创建 Redis client 并做连通性校验。
//
// 职责边界：
// 1. 负责当前调用方声明需要 Redis 时的连接初始化；
// 2. 不承载 user/auth 黑名单、token 额度等业务语义，那些语义已经收进 userauth 服务；
// 3. 返回 error 给服务入口统一处理，避免基础设施包直接 log.Fatal 终止进程。
func OpenRedisFromConfig() (*redis.Client, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     viper.GetString("redis.host") + ":" + viper.GetString("redis.port"),
		Password: viper.GetString("redis.password"),
		DB:       0,
	})
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		return nil, err
	}
	return rdb, nil
}

// InitCoreRedis 初始化当前单体残留域使用的 Redis 连接。
func InitCoreRedis() (*redis.Client, error) {
	return OpenRedisFromConfig()
}

// InitRedis 保留历史兼容入口，新的装配代码应优先使用 InitCoreRedis。
func InitRedis() *redis.Client {
	rdb, err := InitCoreRedis()
	if err != nil {
		log.Fatalf("Redis 连接失败: %v", err)
	}
	return rdb
}
