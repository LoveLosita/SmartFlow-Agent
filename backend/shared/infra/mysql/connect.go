package mysqlinfra

import (
	"fmt"

	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// OpenDBFromConfig 只按统一配置创建 MySQL 连接。
//
// 职责边界：
// 1. 只负责把 viper 中的 database 配置转换成 *gorm.DB。
// 2. 不执行任何 AutoMigrate、回填或服务私有依赖检查。
// 3. 调用方负责决定连接属于哪个服务以及后续初始化动作。
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
	return db, nil
}
