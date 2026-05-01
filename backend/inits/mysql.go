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
		&model.AgentTimelineEvent{},
		&model.Task{},
		&model.TaskClass{},
		&model.TaskClassItem{},
		&model.ScheduleEvent{},
		&model.Schedule{},
		&model.ActiveScheduleJob{},
		&model.ActiveScheduleTrigger{},
		&model.ActiveSchedulePreview{},
		&model.NotificationRecord{},
		&model.UserNotificationChannel{},
		&model.AgentOutboxMessage{},
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
	if err := backfillAutoMigrateData(db); err != nil {
		return err
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
