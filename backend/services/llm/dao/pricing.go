package dao

import (
	"context"

	"gorm.io/gorm"
)

const creditPriceRuleStatusActive = "active"

type CreditPriceRule struct {
	ID                   uint64 `gorm:"column:id"`
	Scene                string `gorm:"column:scene"`
	ProviderName         string `gorm:"column:provider_name"`
	ModelName            string `gorm:"column:model_name"`
	InputPriceMicros     int64  `gorm:"column:input_price_micros"`
	OutputPriceMicros    int64  `gorm:"column:output_price_micros"`
	CachedPriceMicros    int64  `gorm:"column:cached_price_micros"`
	ReasoningPriceMicros int64  `gorm:"column:reasoning_price_micros"`
	CreditPerYuan        int64  `gorm:"column:credit_per_yuan"`
	ProfitRateBps        int64  `gorm:"column:profit_rate_bps"`
	Status               string `gorm:"column:status"`
	Priority             int    `gorm:"column:priority"`
	Description          string `gorm:"column:description"`
}

func (CreditPriceRule) TableName() string {
	return "credit_price_rules"
}

type PriceRuleDAO struct {
	db *gorm.DB
}

func NewPriceRuleDAO(db *gorm.DB) *PriceRuleDAO {
	return &PriceRuleDAO{db: db}
}

func (d *PriceRuleDAO) ListActiveRules(ctx context.Context) ([]CreditPriceRule, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}

	var rules []CreditPriceRule
	err := d.db.WithContext(ctx).
		Model(&CreditPriceRule{}).
		Where("status = ?", creditPriceRuleStatusActive).
		Order("priority DESC, id ASC").
		Find(&rules).Error
	if err != nil {
		return nil, err
	}
	return rules, nil
}
