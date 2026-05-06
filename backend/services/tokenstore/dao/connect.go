package dao

import (
	"errors"
	"fmt"

	storemodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	mysqlinfra "github.com/LoveLosita/smartflow/backend/shared/infra/mysql"
	outboxinfra "github.com/LoveLosita/smartflow/backend/shared/infra/outbox"
	redisinfra "github.com/LoveLosita/smartflow/backend/shared/infra/redis"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OpenDBFromConfig 创建 token-store 服务自己的数据库句柄，并迁移本服务私有表。
//
// 职责边界：
// 1. 只迁移 token_*、credit_* 以及 token-store outbox 表，不迁移其它服务表；
// 2. 自动迁移后执行默认 seed，保证旧 Token 链路和新 Credit 链路都能并行跑通；
// 3. 返回 *gorm.DB 供 DAO 复用，调用方负责进程生命周期。
func OpenDBFromConfig() (*gorm.DB, error) {
	db, err := mysqlinfra.OpenDBFromConfig()
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

// OpenRedisFromConfig 创建 token-store 服务自己的 Redis 句柄。
//
// 职责边界：
// 1. 这里只负责初始化通用 Redis 连接，不决定是否必须启用；
// 2. 调用方可以把失败视为“可选能力不可用”，而不是必须退出进程；
// 3. Credit 缓存 key 语义统一放在 tokenstore 自己的 cache DAO 内维护。
func OpenRedisFromConfig() (*redis.Client, error) {
	return redisinfra.OpenRedisFromConfig()
}

// AutoMigrate 只迁移 token-store 服务拥有的表。
//
// 步骤说明：
// 1. 只迁移 Credit 权威账本表；
// 2. 最后迁移 token-store 的 outbox 表，保证论坛奖励与 Credit 扣费消费都能稳定落表；
// 3. 任一步失败都直接返回，避免服务在 schema 不完整时继续启动。
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("tokenstore auto migrate failed: db is nil")
	}
	if err := db.AutoMigrate(
		&storemodel.CreditAccount{},
		&storemodel.CreditLedger{},
		&storemodel.CreditProduct{},
		&storemodel.CreditOrder{},
		&storemodel.CreditPriceRule{},
		&storemodel.CreditRewardRule{},
	); err != nil {
		return fmt.Errorf("auto migrate tokenstore tables failed: %w", err)
	}
	if err := outboxinfra.AutoMigrateServiceTable(db, outboxinfra.ServiceTokenStore); err != nil {
		return err
	}
	return nil
}

// SeedDefaults 写入 Token 与 Credit 默认商品/规则。
//
// 步骤说明：
// 1. 只保留 Credit 商品与奖励规则 seed；
// 2. Credit 价格规则本轮只建表不写默认价格，避免误用错误计费参数。
func SeedDefaults(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("tokenstore seed failed: db is nil")
	}
	if err := seedDefaultCreditProducts(db); err != nil {
		return err
	}
	if err := backfillCreditProductOriginalPrice(db); err != nil {
		return err
	}
	if err := seedDefaultCreditPriceRules(db); err != nil {
		return err
	}
	if err := seedDefaultCreditRewardRules(db); err != nil {
		return err
	}
	return nil
}

func seedDefaultCreditProducts(db *gorm.DB) error {
	products := defaultCreditProducts()
	for _, product := range products {
		// 1. 这里只负责“缺失即补齐”的默认商品播种，不再把服务启动当成运营配置同步器。
		// 2. 一旦线上已经存在同 SKU 商品，说明运营侧可能手动改过价格、文案或状态，此时必须保留现状。
		// 3. 真正需要批量改默认套餐时，应该走显式 migration 或脚本，而不是依赖服务重启覆盖。
		if err := db.Clauses(creditProductSeedOnConflict()).Create(&product).Error; err != nil {
			return fmt.Errorf("seed credit product %s failed: %w", product.SKU, err)
		}
	}
	return nil
}

func creditProductSeedOnConflict() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "sku"}},
		DoNothing: true,
	}
}

func backfillCreditProductOriginalPrice(db *gorm.DB) error {
	// 1. 只回填 original_price_cent 为空的旧数据，避免覆盖运营已手工维护的划线价。
	// 2. 回填时直接复用当前售价 price_cent，保证接口上线后这个字段立刻可用。
	// 3. 这里不改商品文案、状态和现价，继续遵守“服务启动不是配置覆盖器”的边界。
	return db.
		Model(&storemodel.CreditProduct{}).
		Where("original_price_cent = 0").
		Update("original_price_cent", gorm.Expr("price_cent")).Error
}

func seedDefaultCreditRewardRules(db *gorm.DB) error {
	rules := defaultCreditRewardRules()
	for _, rule := range rules {
		if err := db.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "source"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"name",
				"amount",
				"status",
				"description",
				"config_json",
				"updated_at",
			}),
		}).Create(&rule).Error; err != nil {
			return fmt.Errorf("seed credit reward rule %s failed: %w", rule.Source, err)
		}
	}
	return nil
}

func seedDefaultCreditPriceRules(db *gorm.DB) error {
	rules := defaultCreditPriceRules()
	for _, rule := range rules {
		var existing storemodel.CreditPriceRule
		err := db.
			Where(
				"scene = ? AND provider_name = ? AND model_name = ? AND status = ?",
				rule.Scene,
				rule.ProviderName,
				rule.ModelName,
				storemodel.CreditPriceRuleStatusActive,
			).
			First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if createErr := db.Create(&rule).Error; createErr != nil {
				return fmt.Errorf("seed credit price rule %s/%s/%s failed: %w", rule.Scene, rule.ProviderName, rule.ModelName, createErr)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("load credit price rule %s/%s/%s failed: %w", rule.Scene, rule.ProviderName, rule.ModelName, err)
		}
	}
	return nil
}

func defaultCreditProducts() []storemodel.CreditProduct {
	return []storemodel.CreditProduct{
		{
			SKU:               "credit_free_100",
			Name:              "Free",
			Description:       "每日免费发放，适合基础功能体验。",
			CreditAmount:      100,
			PriceCent:         0,
			OriginalPriceCent: 0,
			Currency:          "CNY",
			Badge:             "每日",
			Status:            storemodel.CreditProductStatusActive,
			SortOrder:         10,
		},
		{
			SKU:               "credit_starter_1000",
			Name:              "Starter",
			Description:       "入门级额度，有效期 1 个月。续费时时间和额度均可累加。",
			CreditAmount:      1000,
			PriceCent:         990,
			OriginalPriceCent: 990,
			Currency:          "CNY",
			Badge:             "入门",
			Status:            storemodel.CreditProductStatusActive,
			SortOrder:         20,
		},
		{
			SKU:               "credit_lite_3000",
			Name:              "Lite",
			Description:       "经济型套餐，有效期 1 个月。适合日常轻度规划。",
			CreditAmount:      3000,
			PriceCent:         1990,
			OriginalPriceCent: 1990,
			Currency:          "CNY",
			Badge:             "经济",
			Status:            storemodel.CreditProductStatusActive,
			SortOrder:         30,
		},
		{
			SKU:               "credit_pro_10000",
			Name:              "Pro",
			Description:       "专业版套餐，有效期 1 个月。最受深度规划用户欢迎。",
			CreditAmount:      10000,
			PriceCent:         3990,
			OriginalPriceCent: 3990,
			Currency:          "CNY",
			Badge:             "Most Popular",
			Status:            storemodel.CreditProductStatusActive,
			SortOrder:         40,
		},
		{
			SKU:               "credit_max_40000",
			Name:              "Max",
			Description:       "旗舰级套餐，有效期 1 个月。极致体验，额度充沛。",
			CreditAmount:      40000,
			PriceCent:         9990,
			OriginalPriceCent: 9990,
			Currency:          "CNY",
			Badge:             "旗舰",
			Status:            storemodel.CreditProductStatusActive,
			SortOrder:         50,
		},
	}
}

func defaultCreditRewardRules() []storemodel.CreditRewardRule {
	return []storemodel.CreditRewardRule{
		{
			Source:      storemodel.CreditLedgerSourceForumLike,
			Name:        "计划被点赞奖励",
			Amount:      1,
			Status:      storemodel.CreditRewardRuleStatusActive,
			Description: "预留论坛点赞正向激励。",
		},
		{
			Source:      storemodel.CreditLedgerSourceForumImport,
			Name:        "计划被导入奖励",
			Amount:      5,
			Status:      storemodel.CreditRewardRuleStatusActive,
			Description: "预留论坛导入正向激励。",
		},
	}
}

func defaultCreditPriceRules() []storemodel.CreditPriceRule {
	return []storemodel.CreditPriceRule{
		{
			Scene:                "*",
			ProviderName:         "ark",
			ModelName:            "*",
			InputPriceMicros:     3200,
			OutputPriceMicros:    16000,
			CachedPriceMicros:    800,
			ReasoningPriceMicros: 16000,
			CreditPerYuan:        100,
			ProfitRateBps:        0,
			Status:               storemodel.CreditPriceRuleStatusActive,
			Priority:             100,
			Description:          "Default Ark rule, prices are expressed in micros CNY per 1K tokens.",
		},
	}
}
