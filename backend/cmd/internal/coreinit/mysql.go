package coreinit

import (
	"fmt"
	"log"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"gorm.io/gorm"
)

// autoMigrateCoreModels 只迁移仍留在当前单体进程内的业务表。
//
// 职责边界：
// 1. 负责 agent / task / schedule / memory 等尚未独立拆出的表；
// 2. 不负责 users、notification_records、JWT、黑名单、token 额度等已拆服务表；
// 3. user/auth 与 notification 表由各自独立进程在自己的 DAO 初始化阶段迁移，避免 all 启动时跨服务碰核心表。
func autoMigrateCoreModels(db *gorm.DB) error {
	models := []any{
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

// OpenDBFromConfig 只按配置创建 MySQL 连接，不执行任何自动迁移。
//
// 职责边界：
// 1. 负责把 viper 中的 database 配置转换成 *gorm.DB；
// 2. 不负责选择要迁移哪些模型，迁移入口必须由具体服务显式调用；
// 3. 调用方负责决定这是单体残留域、user/auth 还是后续新服务的连接。
func OpenDBFromConfig() (*gorm.DB, error) {
	return mysqlinfra.OpenDBFromConfig()
}

// AutoMigrateCoreStorage 执行当前单体残留域拥有的 schema 初始化。
//
// 职责边界：
// 1. 只迁移当前 all/api/worker 仍直接拥有的表和这些域的 outbox 表；
// 2. 不迁移 userauth.User，避免 gateway/all 在阶段 2 之后继续直接管理用户核心表；
// 3. 回填逻辑仍保留在当前域内，因为 schedule_events 仍属于单体残留域。
func AutoMigrateCoreStorage(db *gorm.DB) error {
	if err := autoMigrateCoreModels(db); err != nil {
		return err
	}
	return nil
}

// ConnectCoreDB 创建当前单体残留域的 MySQL 连接，并执行该域自己的迁移。
//
// 迁移期约束：
// 1. all/api/worker 仍需要这条入口来承载尚未拆出的业务域；
// 2. 已拆出的 user/auth 不再通过这里迁移；
// 3. 后续每拆出一个服务，就从 autoMigrateCoreModels 中移走对应模型。
func ConnectCoreDB() (*gorm.DB, error) {
	db, err := OpenDBFromConfig()
	if err != nil {
		return nil, err
	}

	if err = AutoMigrateCoreStorage(db); err != nil {
		return nil, err
	}

	log.Println("Database connected successfully")
	log.Println("Database auto migration completed")
	return db, nil
}

// ConnectDB 保留历史兼容入口，新的装配代码应优先调用 ConnectCoreDB。
func ConnectDB() (*gorm.DB, error) {
	return ConnectCoreDB()
}
