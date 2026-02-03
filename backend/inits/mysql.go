package inits

import (
	"fmt"
	"log"

	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// ConnectDB 连接数据库
// 从config.yaml中读取数据库配置
// 返回错误信息
func ConnectDB() (*gorm.DB, error) {
	// 从配置中读取数据库信息
	host := viper.GetString("database.host")
	port := viper.GetString("database.port")
	user := viper.GetString("database.user")
	password := viper.GetString("database.password")
	dbname := viper.GetString("database.dbname")

	// 构建DSN连接字符串
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		user, password, host, port, dbname)

	// 连接数据库
	var err error
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	log.Println("Database connected successfully")
	return db, nil
}
