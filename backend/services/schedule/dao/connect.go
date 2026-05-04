package dao

import (
	"context"
	"fmt"

	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 schedule 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 schedule_events / schedules 这两个正式日程写模型；
// 2. 不迁移 task、task-class、course 或 active-scheduler 的表，避免 schedule 服务越权管理其它领域；
// 3. 迁移期仍检查 task/task-class 依赖表是否存在，方便启动阶段暴露部署顺序问题。
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
	if err = db.AutoMigrate(&model.ScheduleEvent{}, &model.Schedule{}); err != nil {
		return nil, fmt.Errorf("auto migrate schedule tables failed: %w", err)
	}
	if err = ensureRuntimeDependencyTables(db); err != nil {
		return nil, err
	}
	return db, nil
}

// OpenRedisFromConfig 创建 schedule 服务自己的 Redis 句柄。
//
// 职责边界：
// 1. 只负责初始化 schedule 读缓存所需 Redis client；
// 2. 不创建、不清理任何业务 key；
// 3. Ping 失败直接返回错误，避免读缓存链路静默降级成难排查的启动问题。
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

// ensureRuntimeDependencyTables 显式检查迁移期仍需读取或锁定的外部表。
//
// 说明：
// 1. task_classes / task_items 支撑智能粗排、任务块撤销和补做目标校验；
// 2. tasks 支撑 active-scheduler confirm 的 task_pool 锁定；
// 3. 下一轮拆 task / task-class 后，应把这些依赖改为 RPC 或 read model，再从这里移除。
func ensureRuntimeDependencyTables(db *gorm.DB) error {
	for _, table := range []string{"task_classes", "task_items", "tasks"} {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("schedule runtime dependency table missing: %s", table)
		}
	}
	return nil
}
