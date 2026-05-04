package dao

import (
	"context"
	"fmt"

	userauthmodel "github.com/LoveLosita/smartflow/backend/services/userauth/model"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 user/auth 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 users 以及 user/auth 自己拥有的辅助表，避免独立 userauth 进程顺手迁移其它服务表；
// 2. 不负责读取业务配置之外的外部依赖，配置来源仍由 bootstrap.LoadConfig 统一注入；
// 3. 返回 *gorm.DB 供服务内 DAO 复用，调用方负责进程生命周期。
func OpenDBFromConfig() (*gorm.DB, error) {
	host := viper.GetString("database.host")
	port := viper.GetString("database.port")
	user := viper.GetString("database.user")
	password := viper.GetString("database.password")
	dbname := viper.GetString("database.dbname")

	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port, dbname,
	)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	if err = db.AutoMigrate(&userauthmodel.User{}, &userauthmodel.TokenUsageAdjustment{}); err != nil {
		return nil, fmt.Errorf("auto migrate userauth tables failed: %w", err)
	}
	return db, nil
}

// OpenRedisFromConfig 创建 user/auth 服务自己的 Redis 句柄。
//
// 失败时返回 error，让独立进程入口 fail-fast，避免黑名单和额度门禁静默失效。
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
