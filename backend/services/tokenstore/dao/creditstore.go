package dao

import (
	"context"
	"errors"
	"strings"
	"time"

	creditmodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CreditStoreDAO 承载 Credit 权威账本相关表的持久化访问。
//
// 职责边界：
// 1. 只访问 credit_accounts、credit_ledger、credit_products、credit_orders、credit_price_rules、credit_reward_rules；
// 2. 只提供查询、事务、行锁与原子状态更新，不承载 RPC/前端展示拼装；
// 3. 幂等语义、扣费校验和缓存同步策略由服务层负责。
type CreditStoreDAO struct {
	db *gorm.DB
}

func NewCreditStoreDAO(db *gorm.DB) *CreditStoreDAO {
	return &CreditStoreDAO{db: db}
}

func (dao *CreditStoreDAO) WithTx(tx *gorm.DB) *CreditStoreDAO {
	return &CreditStoreDAO{db: tx}
}

// Transaction 在一个数据库事务内执行 Credit 账本写操作。
func (dao *CreditStoreDAO) Transaction(ctx context.Context, fn func(txDAO *CreditStoreDAO) error) error {
	return dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(dao.WithTx(tx))
	})
}

type ListCreditOrdersQuery struct {
	UserID   uint64
	Page     int
	PageSize int
	Status   string
}

type ListCreditTransactionsQuery struct {
	UserID    uint64
	Page      int
	PageSize  int
	Source    string
	Direction string
}

type GetCreditConsumptionDashboardQuery struct {
	UserID      uint64
	CreatedFrom *time.Time
}

type CreditConsumptionDashboardAggregate struct {
	CreditConsumed int64
	TokenConsumed  int64
}

type ListCreditPriceRulesQuery struct {
	Scene        string
	ProviderName string
	ModelName    string
	Status       string
}

type ListCreditRewardRulesQuery struct {
	Source string
	Status string
}

func (dao *CreditStoreDAO) ListActiveProducts(ctx context.Context) ([]creditmodel.CreditProduct, error) {
	var products []creditmodel.CreditProduct
	err := dao.db.WithContext(ctx).
		Where("status = ?", creditmodel.CreditProductStatusActive).
		Order("sort_order ASC, id ASC").
		Find(&products).Error
	return products, err
}

func (dao *CreditStoreDAO) FindActiveProductByID(ctx context.Context, productID uint64) (*creditmodel.CreditProduct, error) {
	var product creditmodel.CreditProduct
	err := dao.db.WithContext(ctx).
		Where("id = ? AND status = ?", productID, creditmodel.CreditProductStatusActive).
		First(&product).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func (dao *CreditStoreDAO) FindOrderByUserIdempotencyKey(ctx context.Context, userID uint64, key string) (*creditmodel.CreditOrder, error) {
	var order creditmodel.CreditOrder
	err := dao.db.WithContext(ctx).
		Where("user_id = ? AND idempotency_key = ?", userID, key).
		First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (dao *CreditStoreDAO) CreateOrder(ctx context.Context, order *creditmodel.CreditOrder) error {
	return dao.db.WithContext(ctx).Create(order).Error
}

func (dao *CreditStoreDAO) CountOrders(ctx context.Context, query ListCreditOrdersQuery) (int64, error) {
	db := dao.db.WithContext(ctx).
		Model(&creditmodel.CreditOrder{}).
		Where("user_id = ?", query.UserID)
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var total int64
	err := db.Count(&total).Error
	return total, err
}

func (dao *CreditStoreDAO) ListOrders(ctx context.Context, query ListCreditOrdersQuery) ([]creditmodel.CreditOrder, error) {
	db := dao.db.WithContext(ctx).
		Where("user_id = ?", query.UserID)
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var orders []creditmodel.CreditOrder
	err := db.Order("created_at DESC, id DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&orders).Error
	return orders, err
}

func (dao *CreditStoreDAO) FindOrderByID(ctx context.Context, orderID uint64) (*creditmodel.CreditOrder, error) {
	var order creditmodel.CreditOrder
	err := dao.db.WithContext(ctx).Where("id = ?", orderID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (dao *CreditStoreDAO) LockOrderByID(ctx context.Context, orderID uint64) (*creditmodel.CreditOrder, error) {
	var order creditmodel.CreditOrder
	err := dao.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", orderID).
		First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateOrderState 只负责把 Credit 订单持久化到最新状态。
func (dao *CreditStoreDAO) UpdateOrderState(ctx context.Context, orderID uint64, status string, paidAt *time.Time, creditedAt *time.Time, paymentMode string) error {
	updates := map[string]any{
		"status":       status,
		"paid_at":      paidAt,
		"credited_at":  creditedAt,
		"payment_mode": paymentMode,
		"updated_at":   time.Now(),
	}
	return dao.db.WithContext(ctx).
		Model(&creditmodel.CreditOrder{}).
		Where("id = ?", orderID).
		Updates(updates).Error
}

func (dao *CreditStoreDAO) FindLedgerByEventID(ctx context.Context, eventID string) (*creditmodel.CreditLedger, error) {
	var ledger creditmodel.CreditLedger
	err := dao.db.WithContext(ctx).
		Where("event_id = ?", eventID).
		First(&ledger).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ledger, nil
}

func (dao *CreditStoreDAO) FindLatestLedgerByOrderID(ctx context.Context, orderID uint64) (*creditmodel.CreditLedger, error) {
	var ledger creditmodel.CreditLedger
	err := dao.db.WithContext(ctx).
		Where("order_id = ?", orderID).
		Order("created_at DESC, id DESC").
		First(&ledger).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &ledger, nil
}

func (dao *CreditStoreDAO) ListLedgerByOrderIDs(ctx context.Context, orderIDs []uint64) ([]creditmodel.CreditLedger, error) {
	if len(orderIDs) == 0 {
		return []creditmodel.CreditLedger{}, nil
	}

	var ledgers []creditmodel.CreditLedger
	err := dao.db.WithContext(ctx).
		Where("order_id IN ?", orderIDs).
		Order("created_at DESC, id DESC").
		Find(&ledgers).Error
	return ledgers, err
}

func (dao *CreditStoreDAO) CreateLedger(ctx context.Context, ledger *creditmodel.CreditLedger) error {
	return dao.db.WithContext(ctx).Create(ledger).Error
}

func (dao *CreditStoreDAO) CountTransactions(ctx context.Context, query ListCreditTransactionsQuery) (int64, error) {
	db := dao.db.WithContext(ctx).
		Model(&creditmodel.CreditLedger{}).
		Where("user_id = ?", query.UserID)
	if source := strings.TrimSpace(query.Source); source != "" {
		db = db.Where("source = ?", source)
	}
	if direction := strings.TrimSpace(query.Direction); direction != "" {
		db = db.Where("direction = ?", direction)
	}

	var total int64
	err := db.Count(&total).Error
	return total, err
}

func (dao *CreditStoreDAO) ListTransactions(ctx context.Context, query ListCreditTransactionsQuery) ([]creditmodel.CreditLedger, error) {
	db := dao.db.WithContext(ctx).
		Where("user_id = ?", query.UserID)
	if source := strings.TrimSpace(query.Source); source != "" {
		db = db.Where("source = ?", source)
	}
	if direction := strings.TrimSpace(query.Direction); direction != "" {
		db = db.Where("direction = ?", direction)
	}

	var items []creditmodel.CreditLedger
	err := db.Order("created_at DESC, id DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&items).Error
	return items, err
}

// GetCreditConsumptionDashboard 只聚合当前用户 AI 扣费流水对应的消耗看板数据。
//
// 职责边界：
// 1. 只统计 source=charge 且 direction=expense 的流水，保证商店页口径和真实扣费一致。
// 2. 默认排除 failed 流水；skipped 会保留，这样可展示“有 Token 消耗但 Credit 未扣减”的真实情况。
// 3. 这里只做聚合查询，不负责周期归一化、权限校验和前端文案拼装。
func (dao *CreditStoreDAO) GetCreditConsumptionDashboard(ctx context.Context, query GetCreditConsumptionDashboardQuery) (CreditConsumptionDashboardAggregate, error) {
	type aggregateRow struct {
		CreditConsumed int64 `gorm:"column:credit_consumed"`
		TokenConsumed  int64 `gorm:"column:token_consumed"`
	}

	db := dao.db.WithContext(ctx).
		Model(&creditmodel.CreditLedger{}).
		Select(`
			COALESCE(SUM(CASE WHEN amount < 0 THEN -amount ELSE 0 END), 0) AS credit_consumed,
			COALESCE(SUM(
				CASE
					WHEN COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.total_tokens')) AS SIGNED), 0) > 0
						THEN CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.total_tokens')) AS SIGNED)
					ELSE GREATEST(
						COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.input_tokens')) AS SIGNED), 0) +
						COALESCE(CAST(JSON_UNQUOTE(JSON_EXTRACT(metadata_json, '$.output_tokens')) AS SIGNED), 0),
						0
					)
				END
			), 0) AS token_consumed
		`).
		Where("user_id = ?", query.UserID).
		Where("source = ?", creditmodel.CreditLedgerSourceCharge).
		Where("direction = ?", creditmodel.CreditLedgerDirectionExpense).
		Where("status <> ?", creditmodel.CreditLedgerStatusFailed)

	if query.CreatedFrom != nil {
		db = db.Where("created_at >= ?", *query.CreatedFrom)
	}

	var row aggregateRow
	if err := db.Scan(&row).Error; err != nil {
		return CreditConsumptionDashboardAggregate{}, err
	}
	return CreditConsumptionDashboardAggregate{
		CreditConsumed: row.CreditConsumed,
		TokenConsumed:  row.TokenConsumed,
	}, nil
}

func (dao *CreditStoreDAO) FindAccountByUserID(ctx context.Context, userID uint64) (*creditmodel.CreditAccount, error) {
	var account creditmodel.CreditAccount
	err := dao.db.WithContext(ctx).
		Where("user_id = ?", userID).
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (dao *CreditStoreDAO) LockAccountByUserID(ctx context.Context, userID uint64) (*creditmodel.CreditAccount, error) {
	var account creditmodel.CreditAccount
	err := dao.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("user_id = ?", userID).
		First(&account).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &account, nil
}

func (dao *CreditStoreDAO) CreateAccount(ctx context.Context, account *creditmodel.CreditAccount) error {
	return dao.db.WithContext(ctx).Create(account).Error
}

func (dao *CreditStoreDAO) SaveAccount(ctx context.Context, account *creditmodel.CreditAccount) error {
	return dao.db.WithContext(ctx).Save(account).Error
}

func (dao *CreditStoreDAO) ListPriceRules(ctx context.Context, query ListCreditPriceRulesQuery) ([]creditmodel.CreditPriceRule, error) {
	db := dao.db.WithContext(ctx).Model(&creditmodel.CreditPriceRule{})
	if scene := strings.TrimSpace(query.Scene); scene != "" {
		db = db.Where("scene = ?", scene)
	}
	if providerName := strings.TrimSpace(query.ProviderName); providerName != "" {
		db = db.Where("provider_name = ?", providerName)
	}
	if modelName := strings.TrimSpace(query.ModelName); modelName != "" {
		db = db.Where("model_name = ?", modelName)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var rules []creditmodel.CreditPriceRule
	err := db.Order("priority DESC, id ASC").Find(&rules).Error
	return rules, err
}

func (dao *CreditStoreDAO) ListRewardRules(ctx context.Context, query ListCreditRewardRulesQuery) ([]creditmodel.CreditRewardRule, error) {
	db := dao.db.WithContext(ctx).Model(&creditmodel.CreditRewardRule{})
	if source := strings.TrimSpace(query.Source); source != "" {
		db = db.Where("source = ?", source)
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var rules []creditmodel.CreditRewardRule
	err := db.Order("id ASC").Find(&rules).Error
	return rules, err
}
