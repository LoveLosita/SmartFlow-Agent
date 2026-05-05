package dao

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	redisinfra "github.com/LoveLosita/smartflow/backend/shared/infra/redis"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 task 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 tasks 表和 task 服务自己的 outbox 表；
// 2. 不迁移 active-scheduler、schedule、course 或 task-class 表；
// 3. 迁移期仍检查 active_schedule_jobs 是否存在，因为 task 写入后还会 best-effort 同步 due job。
func OpenDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
	if err != nil {
		return nil, err
	}
	if err = db.AutoMigrate(&model.Task{}); err != nil {
		return nil, fmt.Errorf("auto migrate task tables failed: %w", err)
	}
	if err = autoMigrateTaskOutboxTable(db); err != nil {
		return nil, err
	}
	if err = ensureRuntimeDependencyTables(db); err != nil {
		return nil, err
	}
	return db, nil
}

// OpenRedisFromConfig 创建 task 服务自己的 Redis 句柄。
//
// 职责边界：
// 1. 只负责初始化 task 缓存和紧急性平移去重锁所需 Redis client；
// 2. 不清理任何业务 key；
// 3. Ping 失败直接返回错误，避免缓存链路静默降级。
func OpenRedisFromConfig() (*redis.Client, error) {
	return redisinfra.OpenRedisFromConfig()
}

// autoMigrateTaskOutboxTable 只迁移 task 服务自己的 outbox 物理表。
func autoMigrateTaskOutboxTable(db *gorm.DB) error {
	cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceTask)
	if !ok {
		return fmt.Errorf("resolve task outbox config failed")
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&model.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate task outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}

// ensureRuntimeDependencyTables 显式检查 task 迁移期仍写入的跨域表。
//
// 说明：
// 1. active_schedule_jobs 属于 active-scheduler，自有迁移仍由 active-scheduler 管理；
// 2. 本轮为保持任务写入后 due job 同步语义，task 服务只检查存在性；
// 3. 下一轮把 due job 同步改为 active-scheduler RPC 或事件后，应从这里移除。
func ensureRuntimeDependencyTables(db *gorm.DB) error {
	for _, table := range []string{"active_schedule_jobs"} {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("task runtime dependency table missing: %s", table)
		}
	}
	return nil
}
