package dao

import (
	"context"
	"errors"
	"strings"
	"time"

	tokenmodel "github.com/LoveLosita/smartflow/backend/services/tokenstore/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TokenStoreDAO 承载 token-store 私有表的持久化访问。
//
// 职责边界：
// 1. 只访问 token_products、token_orders、token_grants、token_reward_rules。
// 2. 只提供查询、事务和原子状态更新，不组装 RPC/HTTP 视图。
// 3. 业务状态机、幂等回退和提示文案由 sv 层负责。
type TokenStoreDAO struct {
	db *gorm.DB
}

func NewTokenStoreDAO(db *gorm.DB) *TokenStoreDAO {
	return &TokenStoreDAO{db: db}
}

func (dao *TokenStoreDAO) WithTx(tx *gorm.DB) *TokenStoreDAO {
	return &TokenStoreDAO{db: tx}
}

// Transaction 在一个数据库事务内执行 token-store 写操作。
func (dao *TokenStoreDAO) Transaction(ctx context.Context, fn func(txDAO *TokenStoreDAO) error) error {
	return dao.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(dao.WithTx(tx))
	})
}

type ListTokenOrdersQuery struct {
	UserID   uint64
	Page     int
	PageSize int
	Status   string
}

type ListTokenGrantsQuery struct {
	UserID   uint64
	Page     int
	PageSize int
	Source   string
}

type TokenGrantSummary struct {
	RecordedTokenTotal int64
	AppliedTokenTotal  int64
}

func (dao *TokenStoreDAO) ListActiveProducts(ctx context.Context) ([]tokenmodel.TokenProduct, error) {
	var products []tokenmodel.TokenProduct
	err := dao.db.WithContext(ctx).
		Where("status = ?", tokenmodel.TokenProductStatusActive).
		Order("sort_order ASC, id ASC").
		Find(&products).Error
	return products, err
}

func (dao *TokenStoreDAO) FindActiveProductByID(ctx context.Context, productID uint64) (*tokenmodel.TokenProduct, error) {
	var product tokenmodel.TokenProduct
	err := dao.db.WithContext(ctx).
		Where("id = ? AND status = ?", productID, tokenmodel.TokenProductStatusActive).
		First(&product).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func (dao *TokenStoreDAO) FindOrderByUserIdempotencyKey(ctx context.Context, userID uint64, key string) (*tokenmodel.TokenOrder, error) {
	var order tokenmodel.TokenOrder
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

func (dao *TokenStoreDAO) CreateOrder(ctx context.Context, order *tokenmodel.TokenOrder) error {
	return dao.db.WithContext(ctx).Create(order).Error
}

func (dao *TokenStoreDAO) CountOrders(ctx context.Context, query ListTokenOrdersQuery) (int64, error) {
	db := dao.db.WithContext(ctx).
		Model(&tokenmodel.TokenOrder{}).
		Where("user_id = ?", query.UserID)
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var total int64
	err := db.Count(&total).Error
	return total, err
}

func (dao *TokenStoreDAO) ListOrders(ctx context.Context, query ListTokenOrdersQuery) ([]tokenmodel.TokenOrder, error) {
	db := dao.db.WithContext(ctx).
		Where("user_id = ?", query.UserID)
	if status := strings.TrimSpace(query.Status); status != "" {
		db = db.Where("status = ?", status)
	}

	var orders []tokenmodel.TokenOrder
	err := db.Order("created_at DESC, id DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&orders).Error
	return orders, err
}

func (dao *TokenStoreDAO) FindOrderByID(ctx context.Context, orderID uint64) (*tokenmodel.TokenOrder, error) {
	var order tokenmodel.TokenOrder
	err := dao.db.WithContext(ctx).Where("id = ?", orderID).First(&order).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &order, nil
}

func (dao *TokenStoreDAO) LockOrderByID(ctx context.Context, orderID uint64) (*tokenmodel.TokenOrder, error) {
	var order tokenmodel.TokenOrder
	err := dao.db.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", orderID).
		First(&order).Error
	if err != nil {
		return nil, err
	}
	return &order, nil
}

// UpdateOrderState 只负责把订单持久化到最新状态。
//
// 职责边界：
// 1. 调用方必须先完成状态机判断，并决定最终 status/paid_at/granted_at。
// 2. 这里不做“是否允许从 A -> B”校验，避免 DAO 层承载业务规则。
// 3. payment_mode 允许调用方显式回填，保证 mock paid 后订单快照完整。
func (dao *TokenStoreDAO) UpdateOrderState(ctx context.Context, orderID uint64, status string, paidAt *time.Time, grantedAt *time.Time, paymentMode string) error {
	updates := map[string]any{
		"status":       status,
		"paid_at":      paidAt,
		"granted_at":   grantedAt,
		"payment_mode": paymentMode,
		"updated_at":   time.Now(),
	}
	return dao.db.WithContext(ctx).
		Model(&tokenmodel.TokenOrder{}).
		Where("id = ?", orderID).
		Updates(updates).Error
}

func (dao *TokenStoreDAO) FindGrantByEventID(ctx context.Context, eventID string) (*tokenmodel.TokenGrant, error) {
	var grant tokenmodel.TokenGrant
	err := dao.db.WithContext(ctx).
		Where("event_id = ?", eventID).
		First(&grant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &grant, nil
}

func (dao *TokenStoreDAO) FindGrantByOrderID(ctx context.Context, orderID uint64) (*tokenmodel.TokenGrant, error) {
	var grant tokenmodel.TokenGrant
	err := dao.db.WithContext(ctx).
		Where("order_id = ?", orderID).
		Order("created_at DESC, id DESC").
		First(&grant).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &grant, nil
}

func (dao *TokenStoreDAO) ListGrantsByOrderIDs(ctx context.Context, orderIDs []uint64) ([]tokenmodel.TokenGrant, error) {
	if len(orderIDs) == 0 {
		return []tokenmodel.TokenGrant{}, nil
	}

	var grants []tokenmodel.TokenGrant
	err := dao.db.WithContext(ctx).
		Where("order_id IN ?", orderIDs).
		Order("created_at DESC, id DESC").
		Find(&grants).Error
	return grants, err
}

func (dao *TokenStoreDAO) CreateGrant(ctx context.Context, grant *tokenmodel.TokenGrant) error {
	return dao.db.WithContext(ctx).Create(grant).Error
}

func (dao *TokenStoreDAO) CountGrants(ctx context.Context, query ListTokenGrantsQuery) (int64, error) {
	db := dao.db.WithContext(ctx).
		Model(&tokenmodel.TokenGrant{}).
		Where("user_id = ?", query.UserID)
	if source := strings.TrimSpace(query.Source); source != "" {
		db = db.Where("source = ?", source)
	}

	var total int64
	err := db.Count(&total).Error
	return total, err
}

func (dao *TokenStoreDAO) ListGrants(ctx context.Context, query ListTokenGrantsQuery) ([]tokenmodel.TokenGrant, error) {
	db := dao.db.WithContext(ctx).
		Where("user_id = ?", query.UserID)
	if source := strings.TrimSpace(query.Source); source != "" {
		db = db.Where("source = ?", source)
	}

	var grants []tokenmodel.TokenGrant
	err := db.Order("created_at DESC, id DESC").
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		Find(&grants).Error
	return grants, err
}

func (dao *TokenStoreDAO) SummarizePositiveGrants(ctx context.Context, userID uint64) (TokenGrantSummary, error) {
	var summary TokenGrantSummary
	err := dao.db.WithContext(ctx).
		Model(&tokenmodel.TokenGrant{}).
		Select(
			`COALESCE(SUM(CASE WHEN amount > 0 AND status IN (?, ?) THEN amount ELSE 0 END), 0) AS recorded_token_total,
			 COALESCE(SUM(CASE WHEN amount > 0 AND (quota_applied = ? OR status = ?) THEN amount ELSE 0 END), 0) AS applied_token_total`,
			tokenmodel.TokenGrantStatusRecorded,
			tokenmodel.TokenGrantStatusApplied,
			true,
			tokenmodel.TokenGrantStatusApplied,
		).
		Where("user_id = ?", userID).
		Scan(&summary).Error
	return summary, err
}
