package dao

import (
	"fmt"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	"gorm.io/gorm"
)

// OpenDBFromConfig 负责打开 LLM 独立服务需要的数据库连接。
//
// 职责边界：
// 1. 只初始化通用 MySQL 连接并补齐 LLM 自己的 outbox 表；
// 2. 不负责启动 Kafka relay，也不负责装配 Redis/模型客户端；
// 3. 当前阶段不额外声明业务私表，避免和主代理后续 Credit 表迁移交叉。
func OpenDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
	if err != nil {
		return nil, err
	}
	if err = autoMigrateLLMOutboxTable(db); err != nil {
		return nil, err
	}
	return db, nil
}

func autoMigrateLLMOutboxTable(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("llm database is not initialized")
	}

	cfg, ok := outboxinfra.ResolveServiceConfig(outboxinfra.ServiceLLM)
	if !ok {
		return fmt.Errorf("resolve llm outbox config failed")
	}
	if err := db.Table(cfg.TableName).AutoMigrate(&model.AgentOutboxMessage{}); err != nil {
		return fmt.Errorf("auto migrate llm outbox table failed for %s (%s): %w", cfg.Name, cfg.TableName, err)
	}
	return nil
}
