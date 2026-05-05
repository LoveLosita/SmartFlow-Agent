package dao

import (
	"fmt"

	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDBFromConfig 创建 course 服务自己的数据库句柄。
//
// 职责边界：
// 1. course 当前没有独立课程写模型表，导入链路迁移期仍写 schedule_events / schedules；
// 2. 本函数不 AutoMigrate schedule 表，避免 course 进程越权管理 schedule schema；
// 3. 启动期只检查运行时依赖表是否存在，缺表时尽早失败。
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
	if err = ensureRuntimeDependencyTables(db); err != nil {
		return nil, err
	}
	return db, nil
}

// ensureRuntimeDependencyTables 显式检查 course 导入链路迁移期仍直写的 schedule 表。
//
// 说明：
// 1. schedule_events / schedules 属于 schedule 服务正式日程域；
// 2. 本轮保留 course 直写权限，用来维持课程导入两个表同事务写入；
// 3. 后续若改为 schedule RPC bridge，应先补课程导入幂等与冲突返回契约，再移除这里的依赖检查。
func ensureRuntimeDependencyTables(db *gorm.DB) error {
	for _, table := range []string{"schedule_events", "schedules"} {
		if !db.Migrator().HasTable(table) {
			return fmt.Errorf("course runtime dependency table missing: %s", table)
		}
	}
	return nil
}
