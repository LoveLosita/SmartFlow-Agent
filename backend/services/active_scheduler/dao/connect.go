package dao

import (
	"fmt"

	coremodel "github.com/LoveLosita/smartflow/backend/services/runtime/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 active-scheduler 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 active-scheduler 拥有的 trigger / preview / job / session 表和本服务 outbox 表；
// 2. 不迁移 task、schedule、agent、notification 或 user/auth 表，避免独立进程越权管理其它服务模型；
// 3. 返回的 *gorm.DB 供服务内主链路、due job scanner 和 outbox consumer 复用。
func OpenDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
	if err != nil {
		return nil, err
	}
	if err = db.AutoMigrate(
		&coremodel.ActiveScheduleJob{},
		&coremodel.ActiveScheduleTrigger{},
		&coremodel.ActiveSchedulePreview{},
		&coremodel.ActiveScheduleSession{},
	); err != nil {
		return nil, fmt.Errorf("auto migrate active-scheduler tables failed: %w", err)
	}
	if err = autoMigrateActiveSchedulerOutboxTable(db); err != nil {
		return nil, err
	}
	if err = ensureRuntimeDependencyTables(db); err != nil {
		return nil, err
	}
	return db, nil
}

// autoMigrateActiveSchedulerOutboxTable 只迁移 active-scheduler 服务自己的 outbox 物理表。
//
// 职责边界：
// 1. 只负责 active-scheduler.outbox 对应表，不碰其它服务 outbox；
// 2. 让独立 active-scheduler 服务可以单独发布 trigger 并消费 active_schedule.triggered；
// 3. 若后续调整 outbox 表名，只改 service catalog，不在这里硬编码。
func autoMigrateActiveSchedulerOutboxTable(db *gorm.DB) error {
	cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceActiveScheduler)
	if !ok {
		return fmt.Errorf("resolve active-scheduler outbox config failed")
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&coremodel.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate active-scheduler outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}

type runtimeDependencyTable struct {
	Name   string
	Reason string
}

// ensureRuntimeDependencyTables 在服务启动期校验迁移期共享主库依赖。
//
// 职责边界：
// 1. 只检查表是否存在，不 AutoMigrate、不补列、不修改任何跨域表；
// 2. 把 active-scheduler 运行时仍然需要的 agent / notification outbox 边界显式化；
// 3. 若部署顺序、库权限或表结构归属不满足，启动阶段直接 fail fast，避免第一次 trigger 才反复重试。
func ensureRuntimeDependencyTables(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("active-scheduler runtime dependency check failed: db is nil")
	}
	for _, table := range activeSchedulerRuntimeDependencyTables() {
		if err := ensureTableExists(db, table); err != nil {
			return err
		}
	}
	return nil
}

// ensureTableExists 只做存在性探测。
//
// 职责边界：
// 1. 不负责判断字段是否兼容，字段级契约由拥有该表的服务迁移脚本保证；
// 2. 不负责自动修复缺失表，避免 active-scheduler 越权创建其它服务的数据模型；
// 3. 返回错误会阻止服务启动，让部署问题尽早显现。
func ensureTableExists(db *gorm.DB, table runtimeDependencyTable) error {
	if table.Name == "" {
		return fmt.Errorf("active-scheduler runtime dependency table name is empty: %s", table.Reason)
	}
	if db.Migrator().HasTable(table.Name) {
		return nil
	}
	return fmt.Errorf("active-scheduler runtime dependency table missing: %s (%s)", table.Name, table.Reason)
}

// activeSchedulerRuntimeDependencyTables 列出迁移期运行仍需共享主库访问的外部表。
//
// 说明：
// 1. active-scheduler 自有表在 OpenDBFromConfig 内迁移，这里只放跨域依赖；
// 2. notification outbox 表名来自 service catalog，避免和 outbox 多表路由配置漂移；
// 3. schedule 与 task 事实读取已切到 RPC；后续切到 agent/notification RPC 或 read model 后，应继续移除对应表依赖。
func activeSchedulerRuntimeDependencyTables() []runtimeDependencyTable {
	notificationOutboxTable := "notification_outbox_messages"
	if cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceNotification); ok && cfg.TableName != "" {
		notificationOutboxTable = cfg.TableName
	}

	return []runtimeDependencyTable{
		{Name: "agent_chats", Reason: "trigger 生成 preview 后预建主动调度会话"},
		{Name: "chat_histories", Reason: "trigger 生成 preview 后写入会话首屏消息"},
		{Name: "agent_timeline_events", Reason: "trigger 生成 preview 后写入主动调度时间线卡片"},
		{Name: notificationOutboxTable, Reason: "ShouldNotify=true 时投递 notification.feishu.requested 事件"},
	}
}
