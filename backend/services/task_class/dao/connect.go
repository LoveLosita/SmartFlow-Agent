package dao

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	redisinfra "github.com/LoveLosita/smartflow/backend/shared/infra/redis"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 task-class 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 task_classes / task_items 这两个 task-class 自有表；
// 2. 不迁移 schedule_events / schedules，迁移期只检查它们是否存在；
// 3. 迁移期允许 task-class 继续直写 schedule 表，以保留原本本地事务语义。
func OpenDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
	if err != nil {
		return nil, err
	}
	if err = db.AutoMigrate(&model.TaskClass{}, &model.TaskClassItem{}); err != nil {
		return nil, fmt.Errorf("auto migrate task-class tables failed: %w", err)
	}
	if err = ensureRuntimeDependencyTables(db); err != nil {
		return nil, err
	}
	return db, nil
}

// OpenRedisFromConfig 创建 task-class 服务自己的 Redis 句柄。
//
// 职责边界：
// 1. 只负责初始化 task-class 列表缓存和幂等链路所需的 Redis client；
// 2. 不清理任何业务 key；
// 3. Ping 失败直接返回错误，避免服务启动后才暴露缓存不可用。
func OpenRedisFromConfig() (*redis.Client, error) {
	return redisinfra.OpenRedisFromConfig()
}

// ensureRuntimeDependencyTables 显式检查 task-class 迁移期仍直写的外部表。
//
// 说明：
// 1. schedule_events / schedules 属于 schedule 服务正式日程域；
// 2. 本轮按主人拍板保留 task-class 直写权限，换取 insert/apply 与 item 状态更新的本地事务语义；
// 3. 后续若改为 schedule RPC bridge，应先补幂等与补偿，再从这里移除依赖检查。
func ensureRuntimeDependencyTables(db *gorm.DB) error {
	for _, table := range []string{"schedule_events", "schedules"} {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("task-class runtime dependency table missing: %s", table)
		}
	}
	return nil
}
