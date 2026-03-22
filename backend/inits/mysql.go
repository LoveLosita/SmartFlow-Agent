package inits

import (
	"fmt"
	"log"

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
		&model.Task{},
		&model.TaskClass{},
		&model.TaskClassItem{},
		&model.ScheduleEvent{},
		&model.Schedule{},
		&model.AgentOutboxMessage{},
		&model.AgentScheduleState{},
	}

	for _, m := range models {
		if err := db.AutoMigrate(m); err != nil {
			return fmt.Errorf("auto migrate failed for %T: %w", m, err)
		}
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
