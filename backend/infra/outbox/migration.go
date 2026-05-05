package outbox

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/model"
	"gorm.io/gorm"
)

// AutoMigrateServiceTable 按服务目录迁移单个服务拥有的 outbox 表。
//
// 职责边界：
// 1. 只负责创建或补齐服务级 outbox 物理表，不迁移任何业务表；
// 2. table 名统一从 service catalog 解析，避免独立服务和 core 进程各写一份默认值；
// 3. 失败时返回带 service/table 的错误，方便启动期直接定位配置漂移。
func AutoMigrateServiceTable(db *gorm.DB, serviceName string) error {
	if db == nil {
		return fmt.Errorf("auto migrate outbox table failed for %s: db is nil", serviceName)
	}
	cfg, ok := ResolveServiceConfig(serviceName)
	if !ok {
		return fmt.Errorf("resolve outbox config failed for service %s", serviceName)
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&model.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}
