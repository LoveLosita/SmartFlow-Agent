package dao

import (
	"fmt"

	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	forummodel "github.com/LoveLosita/smartflow/backend/services/taskclassforum/model"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建计划广场服务自己的数据库句柄，并迁移本服务私有表。
//
// 职责边界：
// 1. 只迁移 forum_* 表和本服务 outbox 表，不迁移 task_classes / task_items，避免抢占 task-class 拆分线；
// 2. 不负责装配 legacy TaskClass adapter，adapter 在服务实现阶段单独注入；
// 3. 返回 *gorm.DB 供本服务 DAO 复用，调用方负责进程生命周期。
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
	if err = AutoMigrate(db); err != nil {
		return nil, err
	}
	return db, nil
}

// AutoMigrate 只迁移计划广场服务拥有的表。
//
// 步骤说明：
// 1. 先创建帖子、模板、条目、点赞、评论、导入记录表；
// 2. 再按 service catalog 创建 taskclass-forum outbox 表，为后续论坛自身异步事件预留稳定目录；
// 3. 迁移期论坛奖励事件直接写 token-store outbox 表，发布端也兜底创建目标表，避免独立启动顺序导致奖励漏表；
// 4. 唯一约束交给 GORM tag 生成，保证点赞和导入幂等有数据库兜底；
// 5. 失败时直接返回错误，避免服务在 schema 不完整时继续启动。
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("taskclassforum auto migrate failed: db is nil")
	}
	if err := db.AutoMigrate(
		&forummodel.ForumPost{},
		&forummodel.ForumPostTemplate{},
		&forummodel.ForumPostTemplateItem{},
		&forummodel.ForumLike{},
		&forummodel.ForumComment{},
		&forummodel.ForumImport{},
	); err != nil {
		return fmt.Errorf("auto migrate taskclassforum tables failed: %w", err)
	}
	if err := outboxinfra.AutoMigrateServiceTable(db, outboxinfra.ServiceTaskClassForum); err != nil {
		return err
	}
	if err := outboxinfra.AutoMigrateServiceTable(db, outboxinfra.ServiceTokenStore); err != nil {
		return err
	}
	return nil
}
