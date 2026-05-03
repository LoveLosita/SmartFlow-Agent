package inits

import (
	"fmt"
	"log"

	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func autoMigrateModels(db *gorm.DB) error {
	models := []any{
		&model.User{},
		&model.AgentChat{},
		&model.ChatHistory{},
		&model.AgentTimelineEvent{},
		&model.Task{},
		&model.TaskClass{},
		&model.TaskClassItem{},
		&model.ScheduleEvent{},
		&model.Schedule{},
		&model.ActiveScheduleJob{},
		&model.ActiveScheduleTrigger{},
		&model.ActiveSchedulePreview{},
		&model.NotificationRecord{},
		&model.UserNotificationChannel{},
		&model.AgentScheduleState{},
		&model.ActiveScheduleSession{},
		&model.AgentStateSnapshotRecord{},
		&model.MemoryItem{},
		&model.MemoryJob{},
		&model.MemoryAuditLog{},
		&model.MemoryUserSetting{},
	}

	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			return fmt.Errorf("auto migrate failed for %T: %w", m, err)
		}
	}
	if err := autoMigrateOutboxTables(db); err != nil {
		return err
	}
	if err := backfillAutoMigrateData(db); err != nil {
		return err
	}
	return nil
}

// autoMigrateOutboxTables 按服务目录一次性创建各服务的 outbox 物理表。
//
// 职责边界：
// 1. 只创建 outbox 目录，不改写业务表；
// 2. 每张表都复用同一套模型结构，保证字段和索引一致；
// 3. 这里显式列出服务目录，避免把共享单表误当成终态。
func autoMigrateOutboxTables(db *gorm.DB) error {
	// 1. 这里必须按服务目录读取最终生效的 table 名，而不能只看默认内置映射。
	// 2. 这样即使后续通过配置覆盖 outbox.services.*.table，启动建表也会和运行时写入保持一致。
	for _, serviceName := range outboxinfra.ServiceNames() {
		cfg, ok := outboxinfra.ResolveServiceConfig(serviceName)
		if !ok {
			return fmt.Errorf("resolve outbox config failed for service %s", serviceName)
		}
		if err := db.Table(cfg.TableName).AutoMigrate(&model.AgentOutboxMessage{}); err != nil {
			return fmt.Errorf("auto migrate outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
		}
	}
	return nil
}

// backfillAutoMigrateData 补齐 AutoMigrate 无法表达的条件回填。
//
// 职责边界：
// 1. 只处理新增列上线后的兼容数据修复，不替代业务迁移系统；
// 2. 当前仅回填历史动态任务日程来源，确保旧的 type=task 记录按 task_item 解释；
// 3. 失败时直接返回错误，避免服务在 schema 半迁移状态下继续启动。
func backfillAutoMigrateData(db *gorm.DB) error {
	// 1. AutoMigrate 只能新增列和默认值，不能表达"仅 type=task 时回填"。
	// 2. 这里把历史任务日程显式标记为 task_item，避免后续主动调度读取 rel_id 时误判来源。
	// 3. 新增 task_pool 正式落库仍必须由 apply 链路显式写 task_source_type=task_pool。
	result := db.Exec(
		"UPDATE schedule_events SET task_source_type = ? WHERE type = ? AND (task_source_type IS NULL OR task_source_type = '')",
		"task_item",
		"task",
	)
	if result.Error != nil {
		return fmt.Errorf("backfill schedule_events.task_source_type failed: %w", result.Error)
	}
	return nil
}

func ConnectDB() (*gorm.DB, error) {
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

	if err = autoMigrateModels(db); err != nil {
		return nil, err
	}

	log.Println("Database connected successfully")
	log.Println("Database auto migration completed")
	return db, nil
}
