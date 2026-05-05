package dao

import (
	"fmt"

	notificationmodel "github.com/LoveLosita/smartflow/backend/services/notification/model"
	coremodel "github.com/LoveLosita/smartflow/backend/services/runtime/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 notification 服务自己的数据库句柄。
//
// 职责边界：
// 1. 只迁移 notification_records 与 user_notification_channels；
// 2. 不迁移主动调度、agent、userauth 或其它服务表；
// 3. 返回的 *gorm.DB 供 notification 服务内 DAO 和 outbox consumer 复用。
func OpenDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
	if err != nil {
		return nil, err
	}
	if err = db.AutoMigrate(&notificationmodel.NotificationRecord{}, &notificationmodel.UserNotificationChannel{}); err != nil {
		return nil, fmt.Errorf("auto migrate notification tables failed: %w", err)
	}
	if err = autoMigrateNotificationOutboxTable(db); err != nil {
		return nil, err
	}
	return db, nil
}

// autoMigrateNotificationOutboxTable 只迁移 notification 服务自己的 outbox 物理表。
//
// 职责边界：
// 1. 只负责 notification.outbox 对应表，不碰单体残留的其他业务表；
// 2. 让独立 notification 服务可以单独启动和消费 outbox，不依赖 backend/inits 的全量迁移；
// 3. 若后续调整 outbox 表名，只改 service catalog，不在这里硬编码。
func autoMigrateNotificationOutboxTable(db *gorm.DB) error {
	cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceNotification)
	if !ok {
		return fmt.Errorf("resolve notification outbox config failed")
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&coremodel.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate notification outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}
