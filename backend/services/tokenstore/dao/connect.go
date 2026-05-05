package dao

import (
	"fmt"

	outboxinfra "github.com/LoveLosita/smartflow/backend/infra/outbox"
	tokenmodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	"github.com/spf13/viper"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OpenDBFromConfig 创建 token-store 服务自己的数据库句柄，并迁移本服务私有表。
//
// 职责边界：
// 1. 只迁移 token_* 表和 token-store outbox 表，不迁移 users，避免和 user/auth 服务边界冲突；
// 2. 自动迁移后执行 P0 seed，确保前端商品页有可展示商品；
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
	if err = SeedDefaults(db); err != nil {
		return nil, err
	}
	return db, nil
}

// AutoMigrate 只迁移 token-store 服务拥有的表。
//
// 步骤说明：
// 1. 先创建商品、订单、获取账本和奖励规则表；
// 2. 再按 service catalog 创建 token-store outbox 表，保证论坛奖励事件有稳定落表目录；
// 3. 通过唯一约束保证 order_no、event_id 和幂等键不会重复写入；
// 4. 失败时直接返回错误，避免服务在 schema 不完整时继续启动。
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("tokenstore auto migrate failed: db is nil")
	}
	if err := db.AutoMigrate(
		&tokenmodel.TokenProduct{},
		&tokenmodel.TokenOrder{},
		&tokenmodel.TokenGrant{},
		&tokenmodel.TokenRewardRule{},
	); err != nil {
		return fmt.Errorf("auto migrate tokenstore tables failed: %w", err)
	}
	if err := outboxinfra.AutoMigrateServiceTable(db, outboxinfra.ServiceTokenStore); err != nil {
		return err
	}
	return nil
}

// SeedDefaults 写入 P0 默认商品和奖励规则。
//
// 步骤说明：
// 1. 商品和奖励规则都用稳定业务键做 upsert，允许重复启动服务；
// 2. seed 只提供 P0 默认数据，不代表有管理后台能力；
// 3. 后续若商品或规则由运营后台维护，可替换本函数或仅保留初始化兜底。
func SeedDefaults(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("tokenstore seed failed: db is nil")
	}
	if err := seedDefaultProducts(db); err != nil {
		return err
	}
	if err := seedDefaultRewardRules(db); err != nil {
		return err
	}
	return nil
}

func seedDefaultProducts(db *gorm.DB) error {
	products := defaultTokenProducts()
	for _, product := range products {
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "sku"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"description",
				"token_amount",
				"price_cent",
				"currency",
				"badge",
				"status",
				"sort_order",
				"updated_at",
			}),
		}).Create(&product).Error; err != nil {
			return fmt.Errorf("seed token product %s failed: %w", product.SKU, err)
		}
	}
	return nil
}

func defaultTokenProducts() []tokenmodel.TokenProduct {
	return []tokenmodel.TokenProduct{
		{
			SKU:         "token_basic_100",
			Name:        "基础 Token 包",
			Description: "适合轻量使用 Agent。",
			TokenAmount: 100,
			PriceCent:   990,
			Currency:    "CNY",
			Badge:       "入门",
			Status:      tokenmodel.TokenProductStatusActive,
			SortOrder:   10,
		},
		{
			SKU:         "token_plus_300",
			Name:        "进阶 Token 包",
			Description: "适合高频规划和复盘。",
			TokenAmount: 300,
			PriceCent:   1990,
			Currency:    "CNY",
			Badge:       "推荐",
			Status:      tokenmodel.TokenProductStatusActive,
			SortOrder:   20,
		},
		{
			SKU:         "token_pro_800",
			Name:        "专业 Token 包",
			Description: "适合长周期学习计划和高频 Agent 使用。",
			TokenAmount: 800,
			PriceCent:   3990,
			Currency:    "CNY",
			Badge:       "高频",
			Status:      tokenmodel.TokenProductStatusActive,
			SortOrder:   30,
		},
	}
}

func seedDefaultRewardRules(db *gorm.DB) error {
	rules := defaultTokenRewardRules()
	for _, rule := range rules {
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"amount",
				"status",
				"config_json",
				"updated_at",
			}),
		}).Create(&rule).Error; err != nil {
			return fmt.Errorf("seed token reward rule %s failed: %w", rule.Source, err)
		}
	}
	return nil
}

func defaultTokenRewardRules() []tokenmodel.TokenRewardRule {
	return []tokenmodel.TokenRewardRule{
		{
			Source: tokenmodel.TokenGrantSourceForumLike,
			Name:   "计划被点赞奖励",
			Amount: 1,
			Status: tokenmodel.TokenRewardRuleStatusActive,
		},
		{
			Source: tokenmodel.TokenGrantSourceForumImport,
			Name:   "计划被导入奖励",
			Amount: 5,
			Status: tokenmodel.TokenRewardRuleStatusActive,
		},
	}
}
