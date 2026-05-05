package dao

import (
	"context"
	"fmt"

	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	coremodel "github.com/LoveLosita/smartflow/backend/model"
	"github.com/go-redis/redis/v8"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 memory 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 memory_items / memory_jobs / memory_audit_logs / memory_user_settings 以及 memory 服务自己的 outbox 表；
// 2. 不迁移 agent、task、schedule、active-scheduler、notification 等跨域表，避免独立进程越权管理别的领域；
// 3. 返回的 *gorm.DB 供 memory 服务内部 repo、worker 和 outbox consumer 复用。
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

	// 1. 先按统一配置建立 MySQL 连接；若连接失败，独立 memory 进程直接 fail fast。
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// 2. 只迁移 memory 自有表，明确与 agent/task/schedule 等跨域模型隔离。
	if err = db.AutoMigrate(
		&coremodel.MemoryItem{},
		&coremodel.MemoryJob{},
		&coremodel.MemoryAuditLog{},
		&coremodel.MemoryUserSetting{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate memory tables failed: %w", err)
	}

	// 3. 再迁移 memory 服务自己的 outbox 物理表，让独立服务可以单独发布与消费 memory 事件。
	if err = autoMigrateMemoryOutboxTable(db); err != nil {
		return nil, err
	}
	return db, nil
}

// OpenRedisFromConfig 创建 memory 服务自己的 Redis 句柄。
//
// 职责边界：
// 1. 只负责初始化 memory 独立进程所需的 Redis client；
// 2. 不创建、不预热、不清理任何 memory 业务 key；
// 3. Ping 失败直接返回 error，让入口在缓存、锁或幂等依赖异常时尽早暴露问题。
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

// autoMigrateMemoryOutboxTable 只迁移 memory 服务自己的 outbox 物理表。
//
// 职责边界：
// 1. 只负责 service catalog 中 memory 对应的 outbox 表，不硬编码别的服务表名；
// 2. 共享 AgentOutboxMessage 结构作为表结构模板，但物理表仍归 memory 服务所有；
// 3. 若后续 outbox 表名调整，只改 service catalog，不在这里散落配置。
func autoMigrateMemoryOutboxTable(db *gorm.DB) error {
	cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceMemory)
	if !ok {
		return fmt.Errorf("resolve memory outbox config failed")
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&coremodel.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate memory outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}
